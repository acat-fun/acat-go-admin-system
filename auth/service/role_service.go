package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
	"github.com/acat-fun/acat-go-admin-system/auth/repo"
	"github.com/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/result"
)

// 角色管理文案。
const (
	// MessageRoleOnlyRootCreateAdmin 非 root 不能创建 code=admin 角色。
	MessageRoleOnlyRootCreateAdmin = "仅超级管理员可创建管理员角色"
	// MessageRoleOnlyRootEditAdminCode 非 root 不能修改 root/admin 角色编码。
	MessageRoleOnlyRootEditAdminCode = "仅超级管理员可修改管理员角色编码"
	// MessageRoleOnlyRootDeleteAdmin 非 root 不能删除 admin 角色。
	MessageRoleOnlyRootDeleteAdmin = "仅超级管理员可删除管理员角色"
	// MessageRoleRootUndeletable root 角色不可删除。
	MessageRoleRootUndeletable = "超级管理员角色不可删除"
	// MessageRoleRootUndisabled root 角色不可禁用。
	MessageRoleRootUndisabled = "超级管理员角色不可禁用"
	// MessageRoleStatusInvalid 角色状态值非法。
	MessageRoleStatusInvalid = "状态值无效"
)

// 角色权限关联的资源类型。
const (
	// resourceTypePage 页面（t_acat_page）。
	resourceTypePage = 0
	// resourceTypeButton 按钮（t_acat_permission）。
	resourceTypeButton = 1
)

// ListRolesForSelect 返回下拉用角色列表：
// label = "名称 (code)"，value = id 字符串。
func (s *Service) ListRolesForSelect(ctx context.Context) ([]domain.SelectVO, error) {
	roles, err := s.listAllRoles(ctx)
	if err != nil {
		return nil, err
	}
	options := make([]domain.SelectVO, 0, len(roles))
	for _, role := range roles {
		options = append(options, domain.SelectVO{
			Label: fmt.Sprintf("%s (%s)", role.Name, role.Code),
			Value: role.ID,
		})
	}
	return options, nil
}

// ListRolesAll 按 AdminRoleEntity 返回全量角色（GET /roles?all=true）。
func (s *Service) ListRolesAll(ctx context.Context) ([]domain.AdminRoleEntity, error) {
	roles, err := s.listAllRoles(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.AdminRoleEntity, 0, len(roles))
	for _, role := range roles {
		out = append(out, toAdminRoleEntity(role))
	}
	return out, nil
}

// ListRolesPage 返回角色分页列表（默认 GET /roles，元素组装为 WorkerRoleVO）。
func (s *Service) ListRolesPage(ctx context.Context, pageIndex, pageSize int) (result.PageData[domain.WorkerRoleVO], error) {
	var empty result.PageData[domain.WorkerRoleVO]
	if s.roleRepo == nil {
		return empty, fmt.Errorf("service: 角色数据访问未注入")
	}
	pageIndex, pageSize = result.NormalizePage(pageIndex, pageSize)
	roles, total, err := s.roleRepo.ListPage(ctx, result.Offset(pageIndex, pageSize), pageSize)
	if err != nil {
		return empty, err
	}
	list := make([]domain.WorkerRoleVO, 0, len(roles))
	for _, role := range roles {
		list = append(list, toWorkerRoleVO(role))
	}
	return result.NewPageData(list, total, pageIndex, pageSize), nil
}

// CreateRole 新增角色：status 固定 1，name 优先取 i18nValue 的中文值。
func (s *Service) CreateRole(ctx context.Context, actor *logic.Actor, dto domain.RoleSaveDTO) (domain.WorkerRoleVO, error) {
	if s.roleRepo == nil {
		return domain.WorkerRoleVO{}, fmt.Errorf("service: 角色数据访问未注入")
	}
	// root/admin 角色编码保护：code 与超管/管理员角色相同的都仅超管可创建。
	if dto.Code != nil && (*dto.Code == logic.RoleCodeAdmin || *dto.Code == logic.RoleCodeRoot) && !actor.IsRoot() {
		return domain.WorkerRoleVO{}, apperr.NewBusiness(MessageRoleOnlyRootCreateAdmin)
	}
	now := domain.Now()
	role := &domain.Role{
		ID:          s.nextID(),
		Description: dto.Description,
		Status:      1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if dto.Code != nil {
		role.Code = *dto.Code
	}
	// 未做 trim（与权限注册表的 normalize 不同）。
	if name := resolveRoleName(dto.Name, dto.I18nValue); name != nil {
		role.Name = *name
	}
	if err := s.roleRepo.Insert(ctx, role); err != nil {
		return domain.WorkerRoleVO{}, err
	}
	return toWorkerRoleVO(*role), nil
}

// UpdateRole 更新角色：code 不可改，name/description/status 非空才更新。
func (s *Service) UpdateRole(ctx context.Context, actor *logic.Actor, id string, dto domain.RoleSaveDTO) (domain.WorkerRoleVO, error) {
	if s.roleRepo == nil {
		return domain.WorkerRoleVO{}, fmt.Errorf("service: 角色数据访问未注入")
	}
	existing, err := s.roleRepo.FindByID(ctx, id)
	if errors.Is(err, repo.ErrNotFound) {
		return domain.WorkerRoleVO{}, apperr.NewBusiness(MessageRoleNotExist)
	}
	if err != nil {
		return domain.WorkerRoleVO{}, err
	}
	if dto.Code != nil && (existing.Code == logic.RoleCodeRoot || existing.Code == logic.RoleCodeAdmin || existing.ID == domain.RootRoleID) && !actor.IsRoot() {
		return domain.WorkerRoleVO{}, apperr.NewBusiness(MessageRoleOnlyRootEditAdminCode)
	}
	if name := resolveRoleName(dto.Name, dto.I18nValue); name != nil {
		existing.Name = *name
	}
	if dto.Description != nil {
		existing.Description = dto.Description
	}
	if dto.Status != nil {
		existing.Status = *dto.Status
	}
	existing.UpdatedAt = domain.Now()
	affected, err := s.roleRepo.Update(ctx, existing)
	if err != nil {
		return domain.WorkerRoleVO{}, err
	}
	s.warnIfNotUpdated(writeFact("updateRole", TableRole, id, affected))
	return toWorkerRoleVO(*existing), nil
}

// DeleteRole 删除角色：root 不可删、admin 仅 root 可删，并清理关联。
func (s *Service) DeleteRole(ctx context.Context, actor *logic.Actor, id string) error {
	if s.roleRepo == nil {
		return fmt.Errorf("service: 角色数据访问未注入")
	}
	existing, err := s.roleRepo.FindByID(ctx, id)
	if errors.Is(err, repo.ErrNotFound) {
		return apperr.NewBusiness(MessageRoleNotExist)
	}
	if err != nil {
		return err
	}
	if existing.Code == logic.RoleCodeRoot || existing.ID == domain.RootRoleID {
		return apperr.NewBusiness(MessageRoleRootUndeletable)
	}
	if existing.Code == logic.RoleCodeAdmin && !actor.IsRoot() {
		return apperr.NewBusiness(MessageRoleOnlyRootDeleteAdmin)
	}
	// 事务边界：角色软删 + 用户关联 + 权限关联同生共死。
	return s.tx.Within(ctx, func(ctx context.Context) error {
		userRel, err := s.roleRepo.SoftDeleteUserRelations(ctx, id)
		if err != nil {
			return err
		}
		s.warnIfNotUpdated(writeFact("deleteRole.userRelations", TableUserRole, id, userRel))
		permRel, err := s.roleRepo.SoftDeletePermissions(ctx, id)
		if err != nil {
			return err
		}
		s.warnIfNotUpdated(writeFact("deleteRole.permissions", TableRolePermission, id, permRel))
		// 删除/撤销类：目标已不存在即达到目标终态，按幂等成功处理。
		deleted, err := s.roleRepo.SoftDelete(ctx, id)
		if err != nil {
			return err
		}
		s.warnIfNotUpdated(writeFact("deleteRole", TableRole, id, deleted))
		return nil
	})
}

// UpdateRoleStatus 更新角色状态：root 角色不可禁用、状态值仅允许 0/1。
func (s *Service) UpdateRoleStatus(ctx context.Context, id string, status *int) error {
	if s.roleRepo == nil {
		return fmt.Errorf("service: 角色数据访问未注入")
	}
	existing, err := s.roleRepo.FindByID(ctx, id)
	if errors.Is(err, repo.ErrNotFound) {
		return apperr.NewBusiness(MessageRoleNotExist)
	}
	if err != nil {
		return err
	}
	if existing.Code == logic.RoleCodeRoot || existing.ID == domain.RootRoleID {
		return apperr.NewBusiness(MessageRoleRootUndisabled)
	}
	if status == nil || (*status != 0 && *status != 1) {
		return apperr.NewBusiness(MessageRoleStatusInvalid)
	}
	existing.Status = *status
	existing.UpdatedAt = domain.Now()
	affected, err := s.roleRepo.Update(ctx, existing)
	if err != nil {
		return err
	}
	// 状态迁移（启用/禁用角色）：0 行说明并发下角色已被删除或改动，必须失败。
	if err := requireUpdated(writeFact("updateRoleStatus", TableRole, id, affected), ErrVersionConflict); err != nil {
		return apperr.TargetMissing(MessageRoleNotExist).WithCause(err)
	}
	return nil
}

// ListRolePermissionIDs 查询角色已分配的权限 id：
// 角色不存在 → 业务失败；否则返回 t_acat_role_permission 中 is_deleted=0 的权限 id。
func (s *Service) ListRolePermissionIDs(ctx context.Context, roleID string) ([]string, error) {
	if s.roleRepo == nil {
		return nil, fmt.Errorf("service: 角色数据访问未注入")
	}
	if _, err := s.roleRepo.FindByID(ctx, roleID); errors.Is(err, repo.ErrNotFound) {
		return nil, apperr.NewBusiness(MessageRoleNotExist)
	} else if err != nil {
		return nil, err
	}
	return s.roleRepo.PermissionIDs(ctx, roleID)
}

// AssignRolePermissions 分配角色权限：
// 先软删该角色全部 role_permission，再按“页面/按钮是否存在”分别以 resource_type 0/1 幂等插入，
// 既不在 t_acat_page 也不在 t_acat_permission 的 id 直接丢弃。
func (s *Service) AssignRolePermissions(ctx context.Context, roleID string, permissionIDs []string) error {
	if s.roleRepo == nil || s.permissionRepo == nil {
		return fmt.Errorf("service: 角色/权限数据访问未注入")
	}
	if _, err := s.roleRepo.FindByID(ctx, roleID); errors.Is(err, repo.ErrNotFound) {
		return apperr.NewBusiness(MessageRoleNotExist)
	} else if err != nil {
		return err
	}

	candidates := make([]string, 0, len(permissionIDs))
	seen := make(map[string]struct{}, len(permissionIDs))
	for _, permissionID := range permissionIDs {
		if permissionID == "" {
			continue
		}
		if _, dup := seen[permissionID]; dup {
			continue
		}
		seen[permissionID] = struct{}{}
		candidates = append(candidates, permissionID)
	}

	return s.tx.Within(ctx, func(ctx context.Context) error {
		return s.assignRolePermissions(ctx, roleID, candidates)
	})
}

// assignRolePermissions 是 AssignRolePermissions 的事务体内实现
// 。
func (s *Service) assignRolePermissions(ctx context.Context, roleID string, candidates []string) error {
	deleted, err := s.roleRepo.SoftDeletePermissions(ctx, roleID)
	if err != nil {
		return err
	}
	// 关联重建属于幂等补偿操作：0 行只记观测。
	s.warnIfNotUpdated(writeFact("assignRolePermissions.clear", TableRolePermission, roleID, deleted))
	if len(candidates) == 0 {
		return nil
	}

	pageIDs, err := s.roleRepo.ExistingPageIDs(ctx, candidates)
	if err != nil {
		return err
	}
	isPage := make(map[string]struct{}, len(pageIDs))
	for _, pageID := range pageIDs {
		isPage[pageID] = struct{}{}
	}
	urlIDs := make([]string, 0, len(candidates))
	actionCandidates := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := isPage[candidate]; ok {
			urlIDs = append(urlIDs, candidate)
			continue
		}
		actionCandidates = append(actionCandidates, candidate)
	}

	actionIDs := make([]string, 0, len(actionCandidates))
	if len(actionCandidates) > 0 {
		permissions, err := s.permissionRepo.FindByIDs(ctx, actionCandidates)
		if err != nil {
			return err
		}
		exists := make(map[string]struct{}, len(permissions))
		for _, permission := range permissions {
			exists[permission.ID] = struct{}{}
		}
		for _, candidate := range actionCandidates {
			if _, ok := exists[candidate]; ok {
				actionIDs = append(actionIDs, candidate)
			}
		}
	}

	if len(urlIDs) > 0 {
		inserted, err := s.roleRepo.InsertPermissions(ctx, roleID, urlIDs, resourceTypePage)
		if err != nil {
			return err
		}
		s.warnIfNotUpdated(writeFact("assignRolePermissions.insertPage", TableRolePermission, roleID, inserted))
	}
	if len(actionIDs) > 0 {
		inserted, err := s.roleRepo.InsertPermissions(ctx, roleID, actionIDs, resourceTypeButton)
		if err != nil {
			return err
		}
		s.warnIfNotUpdated(writeFact("assignRolePermissions.insertButton", TableRolePermission, roleID, inserted))
	}
	return nil
}

func (s *Service) listAllRoles(ctx context.Context) ([]domain.Role, error) {
	if s.roleRepo == nil {
		return nil, fmt.Errorf("service: 角色数据访问未注入")
	}
	return s.roleRepo.List(ctx)
}

// resolveRoleName 解析角色的缺省展示名（取 zh-CN 标签值，不做 trim）。
func resolveRoleName(name *string, values []domain.I18nValue) *string {
	return domain.ResolveDefaultName(name, values)
}

// toAdminRoleEntity 组装 AdminRoleEntity 响应。
func toAdminRoleEntity(role domain.Role) domain.AdminRoleEntity {
	return domain.AdminRoleEntity{
		ID:          role.ID,
		IsDeleted:   0,
		CreatedAt:   domain.FormatDateTime(role.CreatedAt),
		UpdatedAt:   domain.FormatDateTime(role.UpdatedAt),
		Code:        role.Code,
		Name:        role.Name,
		Description: role.Description,
		Status:      role.Status,
	}
}
