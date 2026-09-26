package service

import (
	"context"
	"errors"
	"strings"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/repo"
)

// ListPages。
func (s *Service) ListPages(ctx context.Context, rc RequestContext) ([]domain.PageVO, error) {
	records, err := s.pages.ListPagesByScope(ctx, domain.PageScopeAdmin, rc.Language())
	if err != nil {
		return nil, err
	}
	filtered, err := s.filterByEnabledFrontendModules(ctx, records)
	if err != nil {
		return nil, err
	}
	vos, err := s.toPageVOs(ctx, filtered)
	if err != nil {
		return nil, err
	}
	return domain.BuildPageTree(vos), nil
}

// ListAccessiblePages。
// root 等价 listAll；其余按会话权限码取页面并补齐祖先。
func (s *Service) ListAccessiblePages(ctx context.Context, rc RequestContext) ([]domain.PageVO, error) {
	if rc.IsRoot() {
		return s.ListPages(ctx, rc)
	}
	permissions := []string{}
	if rc.Actor != nil && rc.Actor.Session != nil {
		permissions = rc.Actor.Session.StringList("permissions")
	}
	if len(permissions) == 0 {
		return []domain.PageVO{}, nil
	}
	accessibleIDs, err := s.pages.SelectPageIDsByPermissions(ctx, permissions)
	if err != nil {
		return nil, err
	}
	if len(accessibleIDs) == 0 {
		return []domain.PageVO{}, nil
	}
	records, err := s.pages.ListPagesByScope(ctx, domain.PageScopeAdmin, rc.Language())
	if err != nil {
		return nil, err
	}
	filtered, err := s.filterByEnabledFrontendModules(ctx, records)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.PageRecord, len(filtered))
	for _, record := range filtered {
		if _, exists := byID[record.ID]; !exists {
			byID[record.ID] = record
		}
	}
	allowed := expandWithAncestors(accessibleIDs, byID)
	visible := make([]domain.PageRecord, 0, len(allowed))
	for _, record := range filtered {
		if _, ok := allowed[record.ID]; ok {
			visible = append(visible, record)
		}
	}
	vos, err := s.toPageVOs(ctx, visible)
	if err != nil {
		return nil, err
	}
	return domain.BuildPageTree(vos), nil
}

// ListEnabledFrontendModuleCodes。
func (s *Service) ListEnabledFrontendModuleCodes(ctx context.Context) ([]string, error) {
	return s.modules.ListEnabledModuleCodes(ctx)
}

// CreatePage。
func (s *Service) CreatePage(ctx context.Context, rc RequestContext, payload domain.PageSavePayload) (*domain.AdminPageEntity, error) {
	name := resolveDefaultNameOrNil(payload.Name, payload.I18nValue)

	scope := domain.PageScopeAdmin
	isEnabled := intValueOr(payload.IsEnabled, 1)
	sortOrder := intValueOr(payload.SortOrder, 0)
	pageType := intValueOr(payload.Type, domain.PageTypePage)

	path := domain.DerefString(payload.Path)
	if strings.TrimSpace(path) == "" {
		return nil, business(MessagePagePathRequired)
	}

	validationRecord := domain.PageRecord{
		Scope:              scope,
		Type:               pageType,
		FrontendModuleCode: payload.FrontendModuleCode,
		RouteKey:           payload.RouteKey,
	}
	if err := s.validatePageSource(ctx, validationRecord); err != nil {
		return nil, err
	}

	code := domain.GenerateAdminPermissionCode(path)
	if payload.ParentID != nil {
		parent, err := s.pages.FindPageByID(ctx, *payload.ParentID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return nil, business(MessagePageParentNotFound)
			}
			return nil, err
		}
		if parent.Scope != domain.PageScopeAdmin {
			return nil, business(MessagePageParentNotAdmin)
		}
	}

	now := s.now()
	loginID := rc.LoginID()
	record := domain.PageRecord{
		ID:                 s.nextID(),
		Code:               code,
		Name:               domain.DerefString(name),
		Type:               pageType,
		Path:               payload.Path,
		Icon:               payload.Icon,
		ParentID:           payload.ParentID,
		SortOrder:          sortOrder,
		Scope:              scope,
		IsEnabled:          isEnabled,
		IsBuiltin:          intValueOr(payload.IsBuiltin, 0),
		FrontendModuleCode: payload.FrontendModuleCode,
		RouteKey:           payload.RouteKey,
		PermissionCode:     domain.DerefString(payload.PermissionCode),
		Description:        domain.DerefString(payload.Description),
		CreatedAt:          now,
		UpdatedAt:          now,
		Version:            0,
	}
	record.CreateBy = &loginID
	record.UpdateBy = &loginID

	// 恢复分支：同 code 存在已软删记录时复用该行（保留 id/created_at/version）。
	deleted, err := s.pages.FindDeletedPageByCode(ctx, code)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	if deleted != nil {
		record.ID = deleted.ID
		if payload.IsBuiltin == nil {
			// 未显式指定时沿用原行的内置标记。
			record.IsBuiltin = deleted.IsBuiltin
		}
		if _, err := s.pages.RestoreDeletedPage(ctx, record); err != nil {
			return nil, err
		}
		restored, err := s.pages.FindPageByID(ctx, deleted.ID)
		if err != nil {
			return nil, err
		}
		if err := s.labels.SaveNameLabels(ctx, domain.I18nTablePage, restored.ID, payload.I18nValue); err != nil {
			return nil, err
		}
		if err := s.savePageHistory(ctx, *restored); err != nil {
			return nil, err
		}
		if _, err := s.pages.RestoreRolePermissionsByPageID(ctx, restored.ID); err != nil {
			return nil, err
		}
		if err := s.syncPagePermission(ctx, *restored); err != nil {
			return nil, err
		}
		entity := pageEntityFromRecord(*restored)
		return &entity, nil
	}

	existing, err := s.pages.FindPageByCode(ctx, code)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	if existing != nil {
		return nil, business("页面标识 '" + code + "' 已存在")
	}
	// 事务边界：页面 + i18n 标签同生共死。
	if err := s.tx.Within(ctx, func(ctx context.Context) error {
		if err := s.pages.InsertPage(ctx, record); err != nil {
			return err
		}
		return s.labels.SaveNameLabels(ctx, domain.I18nTablePage, record.ID, payload.I18nValue)
	}); err != nil {
		return nil, err
	}
	if err := s.savePageHistory(ctx, record); err != nil {
		return nil, err
	}
	if err := s.syncPagePermission(ctx, record); err != nil {
		return nil, err
	}
	entity := pageEntityFromRecord(record)
	entity.IsDeleted = intPtr(0)
	entity.I18nValue = payload.I18nValue
	return &entity, nil
}

// UpdatePage。
func (s *Service) UpdatePage(ctx context.Context, rc RequestContext, id string, payload domain.PageSavePayload) (*domain.AdminPageEntity, error) {
	existing, err := s.pages.FindPageByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, business(MessagePageNotFound)
		}
		return nil, err
	}
	if resolved := resolveDefaultNameOrNil(payload.Name, payload.I18nValue); resolved != nil {
		existing.Name = *resolved
	}
	if payload.Type != nil {
		existing.Type = *payload.Type
	}
	if payload.Path != nil {
		existing.Path = payload.Path
	}
	if payload.Icon != nil {
		existing.Icon = payload.Icon
	}
	if payload.ParentID != nil {
		existing.ParentID = payload.ParentID
	}
	if payload.SortOrder != nil {
		existing.SortOrder = *payload.SortOrder
	}
	existing.Scope = domain.PageScopeAdmin
	if payload.IsEnabled != nil {
		existing.IsEnabled = *payload.IsEnabled
	}
	if payload.IsBuiltin != nil {
		existing.IsBuiltin = *payload.IsBuiltin
	}
	existing.FrontendModuleCode = payload.FrontendModuleCode
	existing.RouteKey = payload.RouteKey
	if payload.PermissionCode != nil {
		existing.PermissionCode = *payload.PermissionCode
	}
	if payload.Description != nil {
		existing.Description = *payload.Description
	}

	if err := s.validatePageSource(ctx, *existing); err != nil {
		return nil, err
	}
	loginID := rc.LoginID()
	existing.UpdateBy = &loginID
	existing.UpdatedAt = s.now()
	// 事务边界：页面 + i18n 标签同生共死。
	err = s.tx.Within(ctx, func(ctx context.Context) error {
		affected, err := s.pages.UpdatePage(ctx, *existing)
		if err != nil {
			return err
		}
		s.warnIfNotUpdated(writeFact("updatePage", TablePage, existing.ID, &existing.Version, affected))
		return s.labels.SaveNameLabels(ctx, domain.I18nTablePage, existing.ID, payload.I18nValue)
	})
	if err != nil {
		return nil, err
	}
	if err := s.savePageHistory(ctx, *existing); err != nil {
		return nil, err
	}
	existing.Version++
	entity := pageEntityFromRecord(*existing)
	entity.IsDeleted = nil
	entity.CreateBy = nil
	return &entity, nil
}

// DeletePage。
func (s *Service) DeletePage(ctx context.Context, rc RequestContext, id string) error {
	// 事务边界：整棵子树 + 标签 + 角色权限关联同生共死。
	return s.tx.Within(ctx, func(ctx context.Context) error {
		return s.deletePageRecursive(ctx, rc, id, 0)
	})
}

// deletePageRecursive 递归删除；depth 用于防御性限制。
func (s *Service) deletePageRecursive(ctx context.Context, rc RequestContext, id string, depth int) error {
	if depth > maxPageDeleteDepth {
		return business(MessagePageNotFound)
	}
	existing, err := s.pages.FindPageByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return business(MessagePageNotFound)
		}
		return err
	}
	if existing.IsBuiltin == 1 {
		return business(MessagePageBuiltinUndeletable)
	}
	if err := s.savePageHistory(ctx, *existing); err != nil {
		return err
	}
	children, err := s.pages.ListPagesByParentID(ctx, id, rc.Language())
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := s.deletePageRecursive(ctx, rc, child.ID, depth+1); err != nil {
			return err
		}
	}
	deleted, err := s.pages.SoftDeletePage(ctx, id)
	if err != nil {
		return err
	}
	// 删除/撤销类：记录已不存在即达到目标终态，按幂等成功处理。
	s.warnIfNotUpdated(writeFact("deletePageRecursive", TablePage, id, nil, deleted))
	labels, err := s.labels.DeleteNameLabels(ctx, domain.I18nTablePage, id)
	if err != nil {
		return err
	}
	s.warnIfNotUpdated(writeFact("deletePageRecursive.labels", TableI18nLabel, id, nil, labels))
	permissions, err := s.pages.DeleteRolePermissionsByPageID(ctx, id)
	if err != nil {
		return err
	}
	s.warnIfNotUpdated(writeFact("deletePageRecursive.rolePermissions", TablePage, id, nil, permissions))
	return nil
}

// maxPageDeleteDepth 防御 parentId 成环导致的无限递归。
const maxPageDeleteDepth = 64

// validatePageSource。
func (s *Service) validatePageSource(ctx context.Context, record domain.PageRecord) error {
	if record.Scope != domain.PageScopeAdmin || record.Type != domain.PageTypePage {
		return nil
	}
	moduleCode := domain.DerefString(record.FrontendModuleCode)
	if strings.TrimSpace(moduleCode) == "" || moduleCode == domain.ShellModuleCode {
		return nil
	}
	if record.RouteKey == nil || !domain.RouteKeyPattern.MatchString(*record.RouteKey) {
		return business(MessagePageRouteKeyInvalid)
	}
	count, err := s.modules.CountEnabledModuleByCode(ctx, moduleCode)
	if err != nil {
		return err
	}
	if count == 0 {
		return business(MessagePageModuleNotEnabled)
	}
	return nil
}

// syncPagePermission。
func (s *Service) syncPagePermission(ctx context.Context, record domain.PageRecord) error {
	if record.Type != domain.PageTypeNav && record.Type != domain.PageTypeFolder &&
		record.Type != domain.PageTypePage {
		return nil
	}
	return s.pages.GrantPageToRootAndAdmin(ctx, record.ID)
}

// savePageHistory。
func (s *Service) savePageHistory(ctx context.Context, record domain.PageRecord) error {
	createdAt := record.CreatedAt
	if createdAt.IsZero() {
		createdAt = s.now()
	}
	now := s.now()
	history := domain.PageHistoryRecord{
		PageID:             record.ID,
		Code:               record.Code,
		Name:               record.Name,
		Type:               intPtr(record.Type),
		Path:               record.Path,
		Icon:               record.Icon,
		ParentID:           record.ParentID,
		SortOrder:          intPtr(record.SortOrder),
		Scope:              intPtr(record.Scope),
		IsEnabled:          intPtr(record.IsEnabled),
		FrontendModuleCode: record.FrontendModuleCode,
		RouteKey:           record.RouteKey,
		IsDeleted:          intPtr(record.IsDeleted),
		CreateBy:           record.CreateBy,
		UpdateBy:           record.UpdateBy,
		CreatedAt:          &createdAt,
		UpdatedAt:          now,
		Version:            intPtr(record.Version),
	}
	return s.pages.InsertPageHistory(ctx, history)
}

// filterByEnabledFrontendModules。
func (s *Service) filterByEnabledFrontendModules(ctx context.Context, records []domain.PageRecord) ([]domain.PageRecord, error) {
	seen := map[string]struct{}{}
	codes := make([]string, 0, 8)
	for _, record := range records {
		code := domain.DerefString(record.FrontendModuleCode)
		if strings.TrimSpace(code) == "" || code == domain.ShellModuleCode {
			continue
		}
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		return records, nil
	}
	modules, err := s.modules.ListEnabledModulesByCodes(ctx, codes)
	if err != nil {
		return nil, err
	}
	enabled := make(map[string]struct{}, len(modules))
	for _, module := range modules {
		enabled[module.ModuleCode] = struct{}{}
	}
	out := make([]domain.PageRecord, 0, len(records))
	for _, record := range records {
		code := domain.DerefString(record.FrontendModuleCode)
		if strings.TrimSpace(code) == "" || code == domain.ShellModuleCode {
			out = append(out, record)
			continue
		}
		if _, ok := enabled[code]; ok {
			out = append(out, record)
		}
	}
	return out, nil
}

// expandWithAncestors。
func expandWithAncestors(leafIDs []string, byID map[string]domain.PageRecord) map[string]struct{} {
	result := make(map[string]struct{}, len(leafIDs))
	for _, id := range leafIDs {
		result[id] = struct{}{}
	}
	for _, id := range leafIDs {
		record, ok := byID[id]
		for ok && record.ParentID != nil {
			result[*record.ParentID] = struct{}{}
			record, ok = byID[*record.ParentID]
		}
	}
	return result
}

// toPageVOs。
func (s *Service) toPageVOs(ctx context.Context, records []domain.PageRecord) ([]domain.PageVO, error) {
	out := make([]domain.PageVO, 0, len(records))
	for _, record := range records {
		labels, err := s.labels.ListNameLabels(ctx, domain.I18nTablePage, record.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.PageVO{
			ID:                 record.ID,
			Code:               record.Code,
			Name:               record.Name,
			Type:               intPtr(record.Type),
			Path:               record.Path,
			Icon:               record.Icon,
			ParentID:           record.ParentID,
			SortOrder:          intPtr(record.SortOrder),
			Scope:              intPtr(record.Scope),
			IsEnabled:          intPtr(record.IsEnabled),
			IsBuiltin:          intPtr(record.IsBuiltin),
			FrontendModuleCode: record.FrontendModuleCode,
			RouteKey:           record.RouteKey,
			PermissionCode:     record.PermissionCode,
			Description:        record.Description,
			I18nValue:          toI18nValues(labels),
			CreatedAt:          domain.StringOrNil(domain.FormatDateTime(record.CreatedAt)),
			UpdatedAt:          domain.StringOrNil(domain.FormatDateTime(record.UpdatedAt)),
		})
	}
	return out, nil
}

// pageEntityFromRecord 把数据库行转换为响应实体。
func pageEntityFromRecord(record domain.PageRecord) domain.AdminPageEntity {
	return domain.AdminPageEntity{
		ID:                 record.ID,
		CreatedAt:          domain.StringOrNil(domain.FormatDateTime(record.CreatedAt)),
		UpdatedAt:          domain.StringOrNil(domain.FormatDateTime(record.UpdatedAt)),
		Version:            intPtr(record.Version),
		Code:               record.Code,
		Name:               record.Name,
		Type:               intPtr(record.Type),
		Path:               record.Path,
		Icon:               record.Icon,
		ParentID:           record.ParentID,
		SortOrder:          intPtr(record.SortOrder),
		Scope:              intPtr(record.Scope),
		IsEnabled:          intPtr(record.IsEnabled),
		IsBuiltin:          intPtr(record.IsBuiltin),
		FrontendModuleCode: record.FrontendModuleCode,
		RouteKey:           record.RouteKey,
		PermissionCode:     record.PermissionCode,
		Description:        record.Description,
	}
}
