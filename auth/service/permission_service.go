package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
	"github.com/acat-fun/acat-go-admin-system/auth/repo"
	"github.com/acat-fun/acat-go-common/apperr"
)

// 权限管理文案。
const (
	// MessagePermissionNotExist 权限不存在。
	MessagePermissionNotExist = "权限不存在"
	// MessagePermissionCodeRequired 权限标识不能为空。
	MessagePermissionCodeRequired = "权限标识不能为空"
	// MessagePermissionNameRequired 权限名称不能为空。
	MessagePermissionNameRequired = "权限名称不能为空"
	// MessagePermissionCodeExists 权限标识已存在。
	MessagePermissionCodeExists = "权限标识已存在"
)

// ListPermissions 复刻 PermissionServiceImpl.listAll：按 id 升序返回实体列表。
func (s *Service) ListPermissions(ctx context.Context) ([]domain.AdminPermissionEntity, error) {
	permissions, err := s.listAllPermissions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.AdminPermissionEntity, 0, len(permissions))
	for _, permission := range permissions {
		out = append(out, toAdminPermissionEntity(permission))
	}
	return out, nil
}

// ListPermissionsAsVO 复刻 PermissionServiceImpl.listAsVO（format=tree，扁平结构）。
func (s *Service) ListPermissionsAsVO(ctx context.Context) ([]domain.PermissionVO, error) {
	permissions, err := s.listAllPermissions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.PermissionVO, 0, len(permissions))
	for _, permission := range permissions {
		out = append(out, domain.PermissionVO{
			ID:     permission.ID,
			Code:   permission.Code,
			Name:   permission.Name,
			PageID: permission.PageID,
		})
	}
	return out, nil
}

// CreatePermission 复刻 PermissionServiceImpl.create：
// 同 code 存在已软删记录则恢复（更新 name/page_id），否则校验 code 唯一后插入。
func (s *Service) CreatePermission(ctx context.Context, dto domain.PermissionSaveDTO) (domain.AdminPermissionEntity, error) {
	if s.permissionRepo == nil {
		return domain.AdminPermissionEntity{}, fmt.Errorf("service: 权限数据访问未注入")
	}
	code := trimToNil(dto.Code)
	name := domain.ResolveDefaultName(trimToNil(dto.Name), dto.I18nValue)
	if code == nil {
		return domain.AdminPermissionEntity{}, apperr.NewBusiness(MessagePermissionCodeRequired)
	}
	if name == nil {
		return domain.AdminPermissionEntity{}, apperr.NewBusiness(MessagePermissionNameRequired)
	}
	now := domain.Now()

	deleted, err := s.permissionRepo.FindDeletedByCode(ctx, *code)
	if err == nil {
		if err := s.permissionRepo.Restore(ctx, deleted.ID, *name, dto.PageID, now); err != nil {
			return domain.AdminPermissionEntity{}, err
		}
		restored, err := s.permissionRepo.FindByID(ctx, deleted.ID)
		if err != nil {
			return domain.AdminPermissionEntity{}, err
		}
		return toAdminPermissionEntity(*restored), nil
	}
	if !errors.Is(err, repo.ErrNotFound) {
		return domain.AdminPermissionEntity{}, err
	}

	exists, err := s.permissionRepo.ExistsByCode(ctx, *code, "")
	if err != nil {
		return domain.AdminPermissionEntity{}, err
	}
	if exists {
		return domain.AdminPermissionEntity{}, apperr.NewBusiness(MessagePermissionCodeExists)
	}

	permission := &domain.AdminPermission{
		ID:        s.nextID(),
		Code:      *code,
		Name:      *name,
		PageID:    dto.PageID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.permissionRepo.Insert(ctx, permission); err != nil {
		return domain.AdminPermissionEntity{}, err
	}
	return toAdminPermissionEntity(*permission), nil
}

// UpdatePermission 复刻 PermissionServiceImpl.update：
// code/name 必填且唯一（排除自身），pageId 非空才更新。
func (s *Service) UpdatePermission(ctx context.Context, id string, dto domain.PermissionSaveDTO) (domain.AdminPermissionEntity, error) {
	if s.permissionRepo == nil {
		return domain.AdminPermissionEntity{}, fmt.Errorf("service: 权限数据访问未注入")
	}
	existing, err := s.permissionRepo.FindByID(ctx, id)
	if errors.Is(err, repo.ErrNotFound) {
		return domain.AdminPermissionEntity{}, apperr.NewBusiness(MessagePermissionNotExist)
	}
	if err != nil {
		return domain.AdminPermissionEntity{}, err
	}
	code := trimToNil(dto.Code)
	name := domain.ResolveDefaultName(trimToNil(dto.Name), dto.I18nValue)
	if code == nil {
		return domain.AdminPermissionEntity{}, apperr.NewBusiness(MessagePermissionCodeRequired)
	}
	if name == nil {
		return domain.AdminPermissionEntity{}, apperr.NewBusiness(MessagePermissionNameRequired)
	}
	exists, err := s.permissionRepo.ExistsByCode(ctx, *code, id)
	if err != nil {
		return domain.AdminPermissionEntity{}, err
	}
	if exists {
		return domain.AdminPermissionEntity{}, apperr.NewBusiness(MessagePermissionCodeExists)
	}

	existing.Code = *code
	existing.Name = *name
	if dto.PageID != nil {
		existing.PageID = dto.PageID
	}
	existing.UpdatedAt = domain.Now()
	affected, err := s.permissionRepo.Update(ctx, existing)
	if err != nil {
		return domain.AdminPermissionEntity{}, err
	}
	s.warnIfNotUpdated(writeFact("updatePermission", TablePermission, id, affected))
	return toAdminPermissionEntity(*existing), nil
}

// DeletePermission 复刻 PermissionServiceImpl.delete：软删除。
func (s *Service) DeletePermission(ctx context.Context, id string) error {
	if s.permissionRepo == nil {
		return fmt.Errorf("service: 权限数据访问未注入")
	}
	if _, err := s.permissionRepo.FindByID(ctx, id); errors.Is(err, repo.ErrNotFound) {
		return apperr.NewBusiness(MessagePermissionNotExist)
	} else if err != nil {
		return err
	}
	affected, err := s.permissionRepo.SoftDelete(ctx, id)
	if err != nil {
		return err
	}
	// 删除/撤销类：目标已不存在即达到目标终态，按幂等成功处理。
	s.warnIfNotUpdated(writeFact("deletePermission", TablePermission, id, affected))
	return nil
}

func (s *Service) listAllPermissions(ctx context.Context) ([]domain.AdminPermission, error) {
	if s.permissionRepo == nil {
		return nil, fmt.Errorf("service: 权限数据访问未注入")
	}
	return s.permissionRepo.List(ctx)
}

// toAdminPermissionEntity 组装 AdminPermissionEntity 响应（查询行必然 is_deleted=0）。
func toAdminPermissionEntity(permission domain.AdminPermission) domain.AdminPermissionEntity {
	return domain.AdminPermissionEntity{
		ID:        permission.ID,
		IsDeleted: 0,
		CreatedAt: domain.FormatDateTime(permission.CreatedAt),
		UpdatedAt: domain.FormatDateTime(permission.UpdatedAt),
		Code:      permission.Code,
		Name:      permission.Name,
		PageID:    permission.PageID,
	}
}

// trimToNil。
func trimToNil(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
