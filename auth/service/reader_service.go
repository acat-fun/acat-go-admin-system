package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
	"github.com/acat-fun/acat-go-admin-system/auth/repo"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/result"
)

// 业务失败提示文案。
const (
	// MessageUsernameExists 用户名重复。
	MessageUsernameExists = "用户名已存在"
	// MessageRoleNotExist 角色不存在。
	MessageRoleNotExist = "角色不存在"
)

// ListReaders 分页查询读者用户。
//
// keyword 对 username/email 做 LIKE（%kw%）；status 非空时等值过滤。
func (s *Service) ListReaders(ctx context.Context, pageIndex, pageSize int, keyword string, status *int) (result.PageData[domain.UserVO], error) {
	var empty result.PageData[domain.UserVO]
	if s.readerRepo == nil {
		return empty, fmt.Errorf("service: 读者用户数据访问未注入")
	}
	pageIndex, pageSize = result.NormalizePage(pageIndex, pageSize)
	users, total, err := s.readerRepo.List(ctx, keyword, status, result.Offset(pageIndex, pageSize), pageSize)
	if err != nil {
		return empty, err
	}
	list := make([]domain.UserVO, 0, len(users))
	for i := range users {
		vo, err := s.toUserVO(ctx, &users[i])
		if err != nil {
			return empty, err
		}
		list = append(list, vo)
	}
	return result.NewPageData(list, total, pageIndex, pageSize), nil
}

// CreateReader 新增读者用户：MD5 十六进制小写密码、status 固定 1。
func (s *Service) CreateReader(ctx context.Context, dto domain.ReaderCreateDTO) (domain.UserVO, error) {
	if s.readerRepo == nil {
		return domain.UserVO{}, fmt.Errorf("service: 读者用户数据访问未注入")
	}
	if strings.TrimSpace(dto.Username) == "" {
		return domain.UserVO{}, apperr.BadRequest("用户名不能为空")
	}
	if dto.Password == "" {
		return domain.UserVO{}, apperr.BadRequest("密码不能为空")
	}
	if _, err := s.readerRepo.FindByUsername(ctx, dto.Username); err == nil {
		return domain.UserVO{}, apperr.NewBusiness(MessageUsernameExists)
	} else if !errors.Is(err, repo.ErrNotFound) {
		return domain.UserVO{}, err
	}

	now := domain.Now()
	user := &domain.ReaderUser{
		ID:        s.nextID(),
		Username:  dto.Username,
		Password:  md5HexLower(dto.Password),
		Email:     dto.Email,
		Status:    1,
		Muted:     0,
		AgeLevel:  16,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if dto.AgeLevel != nil {
		user.AgeLevel = *dto.AgeLevel
	}
	if err := s.readerRepo.Insert(ctx, user); err != nil {
		return domain.UserVO{}, err
	}
	return toUserVO(user, []domain.RoleSimpleVO{}), nil
}

// UpdateReader 更新读者用户：字段为空表示不更新。
func (s *Service) UpdateReader(ctx context.Context, id string, dto domain.ReaderUpdateDTO) (domain.UserVO, error) {
	if s.readerRepo == nil {
		return domain.UserVO{}, fmt.Errorf("service: 读者用户数据访问未注入")
	}
	user, err := s.readerRepo.FindByID(ctx, id)
	if errors.Is(err, repo.ErrNotFound) {
		return domain.UserVO{}, apperr.NewBusiness(MessageUserNotFound)
	}
	if err != nil {
		return domain.UserVO{}, err
	}
	if dto.Username != nil {
		user.Username = *dto.Username
	}
	if dto.Email != nil {
		user.Email = dto.Email
	}
	if dto.Status != nil {
		user.Status = *dto.Status
	}
	if dto.AgeLevel != nil {
		user.AgeLevel = *dto.AgeLevel
	}
	user.UpdatedAt = domain.Now()
	affected, err := s.readerRepo.Update(ctx, user)
	if err != nil {
		return domain.UserVO{}, err
	}
	// 普通资料更新：0 行按静默成功语义，只记结构化日志。
	s.warnIfNotUpdated(writeFact("updateReader", TableReaderUser, id, affected))
	return toUserVO(user, []domain.RoleSimpleVO{}), nil
}

// UpdateReaderStatus 更新读者用户的启用状态。
func (s *Service) UpdateReaderStatus(ctx context.Context, id string, status int) error {
	return s.updateReaderField(ctx, id, func(user *domain.ReaderUser) { user.Status = status })
}

// UpdateReaderMute 更新读者用户的禁言状态。
func (s *Service) UpdateReaderMute(ctx context.Context, id string, muted int) error {
	return s.updateReaderField(ctx, id, func(user *domain.ReaderUser) { user.Muted = muted })
}

func (s *Service) updateReaderField(ctx context.Context, id string, apply func(*domain.ReaderUser)) error {
	if s.readerRepo == nil {
		return fmt.Errorf("service: 读者用户数据访问未注入")
	}
	user, err := s.readerRepo.FindByID(ctx, id)
	if errors.Is(err, repo.ErrNotFound) {
		return apperr.NewBusiness(MessageUserNotFound)
	}
	if err != nil {
		return err
	}
	apply(user)
	user.UpdatedAt = domain.Now()
	affected, err := s.readerRepo.Update(ctx, user)
	if err != nil {
		return err
	}
	// 状态迁移（启用/禁用、禁言）：0 行说明并发下用户已被删除或改动，必须失败。
	if err := requireUpdated(writeFact("updateReaderField", TableReaderUser, id, affected),
		ErrVersionConflict); err != nil {
		return apperr.TargetMissing(MessageUserNotFound).WithCause(err)
	}
	return nil
}

// DeleteReader 软删除读者用户；用户不存在也返回成功（幂等）。
func (s *Service) DeleteReader(ctx context.Context, id string) error {
	if s.readerRepo == nil {
		return fmt.Errorf("service: 读者用户数据访问未注入")
	}
	affected, err := s.readerRepo.SoftDelete(ctx, id)
	if err != nil {
		return err
	}
	// 删除/撤销类：目标已不存在即达到目标终态，按幂等成功处理。
	s.warnIfNotUpdated(writeFact("deleteReader", TableReaderUser, id, affected))
	return nil
}

// AssignReaderRoles 分配读者用户角色：
// 先软删该用户全部角色关联，再对存在的 roleId 做幂等插入（不存在的 id 丢弃）。
func (s *Service) AssignReaderRoles(ctx context.Context, userID string, roleIDs []string) error {
	if s.readerRepo == nil || s.roleRepo == nil {
		return fmt.Errorf("service: 读者用户/角色数据访问未注入")
	}
	// 事务边界：先软删再插入必须原子。
	return s.tx.Within(ctx, func(ctx context.Context) error {
		deleted, err := s.readerRepo.SoftDeleteRoles(ctx, userID)
		if err != nil {
			return err
		}
		// 关联重建属于幂等补偿操作：0 行只记观测。
		s.warnIfNotUpdated(writeFact("assignReaderRoles.clear", TableUserRole, userID, deleted))
		valid, err := s.filterExistingRoleIDs(ctx, roleIDs)
		if err != nil {
			return err
		}
		if len(valid) == 0 {
			return nil
		}
		inserted, err := s.readerRepo.InsertRoles(ctx, userID, valid)
		if err != nil {
			return err
		}
		s.warnIfNotUpdated(writeFact("assignReaderRoles.insert", TableUserRole, userID, inserted))
		return nil
	})
}

// toUserVO 构造读者用户视图：角色只填 code。
func (s *Service) toUserVO(ctx context.Context, user *domain.ReaderUser) (domain.UserVO, error) {
	codes, err := s.readerRepo.RoleCodes(ctx, user.ID)
	if err != nil {
		return domain.UserVO{}, err
	}
	roles := make([]domain.RoleSimpleVO, 0, len(codes))
	for _, code := range codes {
		roles = append(roles, domain.RoleSimpleVO{Code: code})
	}
	return toUserVO(user, roles), nil
}

// toUserVO 组装 UserVO（avatar 固定为 null）。
func toUserVO(user *domain.ReaderUser, roles []domain.RoleSimpleVO) domain.UserVO {
	if roles == nil {
		roles = []domain.RoleSimpleVO{}
	}
	return domain.UserVO{
		ID:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		Avatar:    user.Avatar,
		Status:    user.Status,
		Muted:     user.Muted,
		AgeLevel:  user.AgeLevel,
		Roles:     roles,
		CreatedAt: domain.FormatDateTime(user.CreatedAt),
	}
}

// filterExistingRoleIDs。
func (s *Service) filterExistingRoleIDs(ctx context.Context, roleIDs []string) ([]string, error) {
	if len(roleIDs) == 0 {
		return []string{}, nil
	}
	if s.roleRepo == nil {
		return nil, fmt.Errorf("service: 角色数据访问未注入")
	}
	roles, err := s.roleRepo.FindByIDs(ctx, roleIDs)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		existing[role.ID] = struct{}{}
	}
	valid := make([]string, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		if _, ok := existing[roleID]; ok {
			valid = append(valid, roleID)
		}
	}
	return valid, nil
}

// md5HexLower。
func md5HexLower(raw string) string {
	sum := md5.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}
