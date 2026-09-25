package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
	"github.com/acat-fun/acat-go-admin-system/auth/repo"
	"github.com/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/result"
)

// 工作人员管理文案。
const (
	// MessageWorkerNotExist 工作人员不存在。
	MessageWorkerNotExist = "工作人员不存在"
	// MessageWorkerRootEdit root 不可被非 root 编辑。
	MessageWorkerRootEdit = "不可编辑超级管理员"
	// MessageWorkerRootDisable root 不可被禁用。
	MessageWorkerRootDisable = "不可禁用超级管理员"
	// MessageWorkerRootDelete root 不可被删除。
	MessageWorkerRootDelete = "不可删除超级管理员"
	// MessageOnlyRootAssignRole 非 root 不能分配 root/admin 角色。
	MessageOnlyRootAssignRole = "只有超级管理员才能分配 %s 角色"
)

// ListWorkers 复刻 WorkerAdminServiceImpl.listWorkers：keyword LIKE + ORDER BY id ASC。
func (s *Service) ListWorkers(ctx context.Context, pageIndex, pageSize int, keyword string) (result.PageData[domain.WorkerVO], error) {
	var empty result.PageData[domain.WorkerVO]
	if s.adminWorkerRepo == nil || s.roleRepo == nil {
		return empty, fmt.Errorf("service: 工作人员/角色数据访问未注入")
	}
	pageIndex, pageSize = result.NormalizePage(pageIndex, pageSize)
	workers, total, err := s.adminWorkerRepo.List(ctx, keyword, result.Offset(pageIndex, pageSize), pageSize)
	if err != nil {
		return empty, err
	}
	allRoles, err := s.roleRepo.List(ctx)
	if err != nil {
		return empty, err
	}
	list := make([]domain.WorkerVO, 0, len(workers))
	for i := range workers {
		vo, err := s.toWorkerVO(ctx, &workers[i], allRoles)
		if err != nil {
			return empty, err
		}
		list = append(list, vo)
	}
	return result.NewPageData(list, total, pageIndex, pageSize), nil
}

// CreateWorker 复刻 WorkerAdminServiceImpl.createWorker：
// BCrypt 密码、status=1、重复用户名业务失败、创建后分配角色。
func (s *Service) CreateWorker(ctx context.Context, actor *logic.Actor, dto domain.WorkerCreateDTO) (domain.WorkerVO, error) {
	if s.adminWorkerRepo == nil || s.roleRepo == nil {
		return domain.WorkerVO{}, fmt.Errorf("service: 工作人员/角色数据访问未注入")
	}
	if err := actor.RequirePermission(logic.SystemUsersWorkersAdd); err != nil {
		return domain.WorkerVO{}, err
	}
	if strings.TrimSpace(dto.Username) == "" {
		return domain.WorkerVO{}, apperr.BadRequest("用户名不能为空")
	}
	if dto.Password == "" {
		return domain.WorkerVO{}, apperr.BadRequest("密码不能为空")
	}
	if _, err := s.adminWorkerRepo.FindByUsername(ctx, dto.Username); err == nil {
		return domain.WorkerVO{}, apperr.NewBusiness(MessageUsernameExists)
	} else if !errors.Is(err, repo.ErrNotFound) {
		return domain.WorkerVO{}, err
	}
	if err := s.validateRoleAssignment(ctx, actor, dto.RoleIDs); err != nil {
		return domain.WorkerVO{}, err
	}

	hashed, err := hashWorkerPassword(dto.Password)
	if err != nil {
		return domain.WorkerVO{}, err
	}
	now := domain.Now()
	worker := &domain.AdminWorker{
		ID:        s.nextID(),
		Username:  dto.Username,
		Password:  hashed,
		Email:     dto.Email,
		Status:    1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.adminWorkerRepo.Insert(ctx, worker); err != nil {
		return domain.WorkerVO{}, err
	}
	if len(dto.RoleIDs) > 0 {
		if err := s.assignWorkerRolesUnchecked(ctx, worker.ID, dto.RoleIDs); err != nil {
			return domain.WorkerVO{}, err
		}
	}

	allRoles, err := s.roleRepo.List(ctx)
	if err != nil {
		return domain.WorkerVO{}, err
	}
	return s.toWorkerVO(ctx, worker, allRoles)
}

// UpdateWorker 复刻 WorkerAdminServiceImpl.updateWorker：
// 自操作绕过 + root 保护 + 空值不更新 + password 非空才重新 BCrypt + roleIds 非 nil 才重分配角色。
func (s *Service) UpdateWorker(ctx context.Context, actor *logic.Actor, id string, dto domain.WorkerUpdateDTO) (domain.WorkerVO, error) {
	if s.adminWorkerRepo == nil || s.roleRepo == nil {
		return domain.WorkerVO{}, fmt.Errorf("service: 工作人员/角色数据访问未注入")
	}
	worker, err := s.adminWorkerRepo.FindByID(ctx, id)
	if errors.Is(err, repo.ErrNotFound) {
		return domain.WorkerVO{}, apperr.NewBusiness(MessageWorkerNotExist)
	}
	if err != nil {
		return domain.WorkerVO{}, err
	}
	if err := actor.RequirePermissionOrSelf(id, logic.SystemUsersWorkersEdit); err != nil {
		return domain.WorkerVO{}, err
	}
	if s.workerIsRoot(ctx, worker.ID) && !actor.IsRoot() {
		return domain.WorkerVO{}, apperr.NewBusiness(MessageWorkerRootEdit)
	}

	if dto.Username != nil {
		worker.Username = *dto.Username
	}
	if dto.Email != nil {
		worker.Email = dto.Email
	}
	if dto.Password != nil && *dto.Password != "" {
		hashed, err := hashWorkerPassword(*dto.Password)
		if err != nil {
			return domain.WorkerVO{}, err
		}
		worker.Password = hashed
	}
	if dto.Status != nil {
		worker.Status = *dto.Status
	}
	worker.UpdatedAt = domain.Now()
	affected, err := s.adminWorkerRepo.Update(ctx, worker)
	if err != nil {
		return domain.WorkerVO{}, err
	}
	s.warnIfNotUpdated(writeFact("updateWorker", TableAdminWorker, id, affected))

	if dto.RoleIDs != nil {
		if err := s.validateRoleAssignment(ctx, actor, dto.RoleIDs); err != nil {
			return domain.WorkerVO{}, err
		}
		if err := s.assignWorkerRolesUnchecked(ctx, id, dto.RoleIDs); err != nil {
			return domain.WorkerVO{}, err
		}
	}

	allRoles, err := s.roleRepo.List(ctx)
	if err != nil {
		return domain.WorkerVO{}, err
	}
	return s.toWorkerVO(ctx, worker, allRoles)
}

// UpdateWorkerStatus 复刻 WorkerAdminServiceImpl.updateStatus（权限码用 workers:edit）。
func (s *Service) UpdateWorkerStatus(ctx context.Context, actor *logic.Actor, id string, status int) error {
	if s.adminWorkerRepo == nil {
		return fmt.Errorf("service: 工作人员数据访问未注入")
	}
	worker, err := s.adminWorkerRepo.FindByID(ctx, id)
	if errors.Is(err, repo.ErrNotFound) {
		return apperr.NewBusiness(MessageWorkerNotExist)
	}
	if err != nil {
		return err
	}
	if err := actor.RequirePermissionOrSelf(id, logic.SystemUsersWorkersEdit); err != nil {
		return err
	}
	if s.workerIsRoot(ctx, worker.ID) {
		return apperr.NewBusiness(MessageWorkerRootDisable)
	}
	worker.Status = status
	worker.UpdatedAt = domain.Now()
	affected, err := s.adminWorkerRepo.Update(ctx, worker)
	if err != nil {
		return err
	}
	// 状态迁移（启用/禁用工作人员）：0 行说明并发下已被删除或改动，必须失败。
	if err := requireUpdated(writeFact("updateWorkerStatus", TableAdminWorker, id, affected), ErrVersionConflict); err != nil {
		return apperr.TargetMissing(MessageWorkerNotExist).WithCause(err)
	}
	return nil
}

// DeleteWorker 复刻 WorkerAdminServiceImpl.deleteWorker：软删工作人员 + 软删角色关联。
func (s *Service) DeleteWorker(ctx context.Context, actor *logic.Actor, id string) error {
	if s.adminWorkerRepo == nil {
		return fmt.Errorf("service: 工作人员数据访问未注入")
	}
	worker, err := s.adminWorkerRepo.FindByID(ctx, id)
	if errors.Is(err, repo.ErrNotFound) {
		return apperr.NewBusiness(MessageWorkerNotExist)
	}
	if err != nil {
		return err
	}
	if err := actor.RequirePermissionOrSelf(id, logic.SystemUsersWorkersDelete); err != nil {
		return err
	}
	if s.workerIsRoot(ctx, worker.ID) {
		return apperr.NewBusiness(MessageWorkerRootDelete)
	}
	// 事务边界：工作人员软删 + 角色关联同生共死。
	return s.tx.Within(ctx, func(ctx context.Context) error {
		deleted, err := s.adminWorkerRepo.SoftDelete(ctx, id)
		if err != nil {
			return err
		}
		s.warnIfNotUpdated(writeFact("deleteWorker", TableAdminWorker, id, deleted))
		roles, err := s.adminWorkerRepo.SoftDeleteRoles(ctx, id)
		if err != nil {
			return err
		}
		s.warnIfNotUpdated(writeFact("deleteWorker.roles", TableUserRole, id, roles))
		return nil
	})
}

// AssignWorkerRoles 复刻 WorkerAdminServiceImpl.assignRoles（自操作绕过 + assign-role 权限），
// 并按需求补充非 root 不得分配 root/admin 角色的校验。
func (s *Service) AssignWorkerRoles(ctx context.Context, actor *logic.Actor, workerID string, roleIDs []string) error {
	if s.adminWorkerRepo == nil || s.roleRepo == nil {
		return fmt.Errorf("service: 工作人员/角色数据访问未注入")
	}
	if err := actor.RequirePermissionOrSelf(workerID, logic.SystemUsersWorkersAssignRole); err != nil {
		return err
	}
	if err := s.validateRoleAssignment(ctx, actor, roleIDs); err != nil {
		return err
	}
	return s.assignWorkerRolesUnchecked(ctx, workerID, roleIDs)
}

// assignWorkerRolesUnchecked 执行“先软删再幂等插入”，不重复做权限判定（创建/更新内部调用）。
func (s *Service) assignWorkerRolesUnchecked(ctx context.Context, workerID string, roleIDs []string) error {
	// 事务边界：先软删再插入必须原子。
	return s.tx.Within(ctx, func(ctx context.Context) error {
		deleted, err := s.adminWorkerRepo.SoftDeleteRoles(ctx, workerID)
		if err != nil {
			return err
		}
		// 关联重建属于幂等补偿操作：0 行只记观测。
		s.warnIfNotUpdated(writeFact("assignWorkerRoles.clear", TableUserRole, workerID, deleted))
		valid, err := s.filterExistingRoleIDs(ctx, roleIDs)
		if err != nil {
			return err
		}
		if len(valid) == 0 {
			return nil
		}
		inserted, err := s.adminWorkerRepo.InsertRoles(ctx, workerID, valid)
		if err != nil {
			return err
		}
		s.warnIfNotUpdated(writeFact("assignWorkerRoles.insert", TableUserRole, workerID, inserted))
		return nil
	})
}

// workerIsRoot 判断工作人员是否绑定超级管理员角色（roles 含 root）。
//
// 超管保护统一按角色判定；查询失败视为非 root（fail-closed，不阻塞只影响保护分支）。
func (s *Service) workerIsRoot(ctx context.Context, workerID string) bool {
	codes, err := s.adminWorkerRepo.RoleCodes(ctx, workerID)
	if err != nil {
		return false
	}
	return domain.IsRootRole(codes)
}

// validateRoleAssignment。
// 非 root 分配 root/admin 角色时返回「只有超级管理员才能分配 {角色名} 角色」。
func (s *Service) validateRoleAssignment(ctx context.Context, actor *logic.Actor, roleIDs []string) error {
	if actor.IsRoot() || len(roleIDs) == 0 {
		return nil
	}
	if s.roleRepo == nil {
		return fmt.Errorf("service: 角色数据访问未注入")
	}
	roles, err := s.roleRepo.FindByIDs(ctx, roleIDs)
	if err != nil {
		return err
	}
	for _, role := range roles {
		if role.Code == logic.RoleCodeRoot || role.Code == logic.RoleCodeAdmin {
			return apperr.NewBusiness(fmt.Sprintf(MessageOnlyRootAssignRole, role.Name))
		}
	}
	return nil
}

// toWorkerVO 复刻 WorkerAdminServiceImpl.toVO：
// 角色取“该用户启用角色编码 ∩ 全量未删除角色”，permissionCount 恒为 null。
func (s *Service) toWorkerVO(ctx context.Context, worker *domain.AdminWorker, allRoles []domain.Role) (domain.WorkerVO, error) {
	codes, err := s.adminWorkerRepo.RoleCodes(ctx, worker.ID)
	if err != nil {
		return domain.WorkerVO{}, err
	}
	allowed := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		allowed[code] = struct{}{}
	}
	roles := make([]domain.WorkerRoleVO, 0, len(allRoles))
	for _, role := range allRoles {
		if _, ok := allowed[role.Code]; !ok {
			continue
		}
		roles = append(roles, toWorkerRoleVO(role))
	}
	return domain.WorkerVO{
		ID:        worker.ID,
		Username:  worker.Username,
		Email:     worker.Email,
		Avatar:    worker.Avatar,
		Status:    worker.Status,
		Roles:     roles,
		CreatedAt: domain.FormatDateTime(worker.CreatedAt),
		UpdatedAt: domain.FormatDateTime(worker.UpdatedAt),
	}, nil
}

// toWorkerRoleVO。
func toWorkerRoleVO(role domain.Role) domain.WorkerRoleVO {
	return domain.WorkerRoleVO{
		ID:              role.ID,
		Code:            role.Code,
		Name:            role.Name,
		Description:     role.Description,
		Status:          role.Status,
		PermissionCount: nil,
	}
}

// hashPassword 使用 BCrypt 默认强度（10）。
func hashWorkerPassword(raw string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("生成密码哈希失败: %w", err)
	}
	return string(hashed), nil
}
