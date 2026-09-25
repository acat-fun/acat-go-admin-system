package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/repo"
)

// frontendLabelCodePattern （标签码格式约束）。
var frontendLabelCodePattern = regexp.MustCompile(
	`^acat\.read\.(admin|app)\.[a-z0-9][a-z0-9-]*\.[a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9-]*\.[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ListDicts。
func (s *Service) ListDicts(ctx context.Context, rc RequestContext, filter domain.DictFilter, pageIndex, pageSize int) (any, error) {
	records, total, err := s.dicts.ListDicts(ctx, filter, pageIndex, pageSize)
	if err != nil {
		return nil, err
	}
	items := make([]domain.DictVO, 0, len(records))
	for _, record := range records {
		vo, voErr := s.toDictVO(ctx, rc, record)
		if voErr != nil {
			return nil, voErr
		}
		items = append(items, vo)
	}
	return newPageData(items, total, pageIndex, pageSize), nil
}

// ListAllDicts。
func (s *Service) ListAllDicts(ctx context.Context, rc RequestContext) ([]domain.DictVO, error) {
	records, err := s.dicts.ListAllDicts(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]domain.DictVO, 0, len(records))
	for _, record := range records {
		vo, voErr := s.toDictVO(ctx, rc, record)
		if voErr != nil {
			return nil, voErr
		}
		items = append(items, vo)
	}
	return items, nil
}

// ListDictsForSelect。
// label = 当前语言名称，value = 字典 code。
func (s *Service) ListDictsForSelect(ctx context.Context, rc RequestContext) ([]domain.SelectVO, error) {
	records, err := s.dicts.ListDictsForSelect(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]domain.SelectVO, 0, len(records))
	for _, record := range records {
		name, nameErr := s.labels.ResolveCurrentName(ctx, domain.I18nTableDict, record.ID, rc.Language())
		if nameErr != nil {
			return nil, nameErr
		}
		if strings.TrimSpace(name) == "" {
			name = record.Name
		}
		items = append(items, domain.SelectVO{Label: name, Value: record.Code})
	}
	return items, nil
}

// CreateDict。
// 唯一性预检 → 默认值 → zh-CN 名称解析 → 落库 → 写名称标签 → 返回实体。
func (s *Service) CreateDict(ctx context.Context, rc RequestContext, payload domain.DictSavePayload) (*domain.AdminDictEntity, error) {
	code := domain.DerefString(payload.Code)
	exists, err := s.dicts.CountDictsByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if exists > 0 {
		return nil, business(MessageDictCodeExists + code)
	}

	isEnabled := 1
	if payload.IsEnabled != nil {
		isEnabled = *payload.IsEnabled
	}
	isTree := 0
	if payload.IsTree != nil {
		isTree = *payload.IsTree
	}
	scope := 0
	if payload.Scope != nil {
		scope = *payload.Scope
	}
	name := domain.DerefString(resolveDefaultNameOrNil(payload.Name, payload.I18nValue))

	now := s.now()
	record := domain.DictRecord{
		ID:          s.nextID(),
		Code:        code,
		Name:        name,
		IsEnabled:   isEnabled,
		IsTree:      isTree,
		Scope:       scope,
		Description: payload.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
		Version:     0,
	}
	loginID := rc.LoginID()
	record.CreateBy = &loginID
	record.UpdateBy = &loginID
	// 事务边界：字典主记录 + i18n 标签同生共死。
	if err := s.tx.Within(ctx, func(ctx context.Context) error {
		if err := s.dicts.InsertDict(ctx, record); err != nil {
			return err
		}
		return s.labels.SaveNameLabels(ctx, domain.I18nTableDict, record.ID, payload.I18nValue)
	}); err != nil {
		return nil, err
	}
	entity := dictEntityFromRecord(record)
	entity.IsDeleted = intPtr(0)
	entity.I18nValue = payload.I18nValue
	return &entity, nil
}

// UpdateDict。
func (s *Service) UpdateDict(ctx context.Context, rc RequestContext, id string, payload domain.DictSavePayload) (*domain.AdminDictEntity, error) {
	existing, err := s.dicts.FindDictByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, business(MessageDictNotFound)
		}
		return nil, err
	}
	if resolved := resolveDefaultNameOrNil(payload.Name, payload.I18nValue); resolved != nil {
		existing.Name = *resolved
	}
	if payload.IsEnabled != nil {
		existing.IsEnabled = *payload.IsEnabled
	}
	if payload.IsTree != nil {
		existing.IsTree = *payload.IsTree
	}
	if payload.Scope != nil {
		existing.Scope = *payload.Scope
	}
	if payload.Description != nil {
		existing.Description = payload.Description
	}
	loginID := rc.LoginID()
	existing.UpdateBy = &loginID
	existing.UpdatedAt = s.now()
	// 事务边界：字典字段 + i18n 标签同生共死。
	err = s.tx.Within(ctx, func(ctx context.Context) error {
		affected, err := s.dicts.UpdateDict(ctx, *existing)
		if err != nil {
			return err
		}
		// 普通资料更新：0 行沿用 静默成功语义（乐观锁冲突静默），只记结构化日志。
		s.warnIfNotUpdated(writeFact("updateDict", TableDict, existing.ID, &existing.Version, affected))
		return s.labels.SaveNameLabels(ctx, domain.I18nTableDict, existing.ID, payload.I18nValue)
	})
	if err != nil {
		return nil, err
	}
	existing.Version++
	entity := dictEntityFromRecord(*existing)
	entity.IsDeleted = nil
	entity.CreateBy = nil
	return &entity, nil
}

// DeleteDict。
func (s *Service) DeleteDict(ctx context.Context, id string) error {
	dict, err := s.dicts.FindDictByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return business(MessageDictNotFound)
		}
		return err
	}
	items, err := s.dicts.ListAllDataItems(ctx, dict.ID, nil)
	if err != nil {
		return err
	}
	// 事务边界：级联软删 + 标签清理同生共死。
	return s.tx.Within(ctx, func(ctx context.Context) error {
		for _, item := range items {
			deleted, err := s.dicts.SoftDeleteDataItem(ctx, item.ID)
			if err != nil {
				return err
			}
			// 删除/撤销类：目标已不存在即达到目标终态，按幂等成功处理。
			s.warnIfNotUpdated(writeFact("deleteDict.dataItem", TableDictData, item.ID, nil, deleted))
			labels, err := s.labels.DeleteNameLabels(ctx, domain.I18nTableDictData, item.ID)
			if err != nil {
				return err
			}
			s.warnIfNotUpdated(writeFact("deleteDict.dataItemLabels", TableI18nLabel, item.ID, nil, labels))
		}
		deleted, err := s.dicts.SoftDeleteDict(ctx, dict.ID)
		if err != nil {
			return err
		}
		s.warnIfNotUpdated(writeFact("deleteDict", TableDict, dict.ID, nil, deleted))
		labels, err := s.labels.DeleteNameLabels(ctx, domain.I18nTableDict, dict.ID)
		if err != nil {
			return err
		}
		s.warnIfNotUpdated(writeFact("deleteDict.labels", TableI18nLabel, dict.ID, nil, labels))
		return nil
	})
}

// ---------------------------------------------------------------------------
// 字典数据项
// ---------------------------------------------------------------------------

// ListDataItems。
func (s *Service) ListDataItems(ctx context.Context, rc RequestContext, dictID string, pageIndex, pageSize int, name string) (any, error) {
	realID, err := s.resolveDictID(ctx, dictID)
	if err != nil {
		return nil, err
	}
	if realID == "" {
		return newPageData([]domain.DictDataVO{}, 0, pageIndex, pageSize), nil
	}
	dict, err := s.dicts.FindDictByID(ctx, realID)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	isTree := dict != nil && dict.IsTree == 1

	if isTree {
		records, listErr := s.dicts.ListAllDataItems(ctx, realID, nil)
		if listErr != nil {
			return nil, listErr
		}
		filtered := filterDataItemsByName(records, name)
		vos, mapErr := s.toDictDataVOs(ctx, rc, filtered)
		if mapErr != nil {
			return nil, mapErr
		}
		tree := domain.BuildDictDataTree(vos)
		from, to := treeWindow(pageIndex, pageSize, len(tree))
		paged := tree[from:to]
		page := newPageData(paged, int64(len(vos)), pageIndex, pageSize)
		return withHeadNodeTotal(page, int64(len(tree))), nil
	}

	records, total, err := s.dicts.ListDataItems(ctx, realID, domain.DictDataFilter{Name: name}, pageIndex, pageSize)
	if err != nil {
		return nil, err
	}
	vos, err := s.toDictDataVOs(ctx, rc, records)
	if err != nil {
		return nil, err
	}
	return newPageData(vos, total, pageIndex, pageSize), nil
}

// ListAllDataItems。
func (s *Service) ListAllDataItems(ctx context.Context, rc RequestContext, dictCode string) ([]domain.DictDataVO, error) {
	dictID, err := s.dictIDByCode(ctx, dictCode)
	if err != nil {
		return nil, err
	}
	if dictID == "" {
		return []domain.DictDataVO{}, nil
	}
	records, err := s.dicts.ListAllDataItems(ctx, dictID, nil)
	if err != nil {
		return nil, err
	}
	vos, err := s.toDictDataVOs(ctx, rc, records)
	if err != nil {
		return nil, err
	}
	dict, err := s.dicts.FindDictByID(ctx, dictID)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	if dict != nil && dict.IsTree == 1 {
		return domain.BuildDictDataTree(vos), nil
	}
	return vos, nil
}

// ListDataItemsForSelect。
// 仅启用项；树形字典返回 SelectTreeVO，扁平字典返回 SelectVO（value 取 value 列）。
func (s *Service) ListDataItemsForSelect(ctx context.Context, rc RequestContext, dictCode string) (any, error) {
	dictID, err := s.dictIDByCode(ctx, dictCode)
	if err != nil {
		return nil, err
	}
	if dictID == "" {
		return []any{}, nil
	}
	enabled := 1
	records, err := s.dicts.ListAllDataItems(ctx, dictID, &enabled)
	if err != nil {
		return nil, err
	}
	vos, err := s.toDictDataVOs(ctx, rc, records)
	if err != nil {
		return nil, err
	}
	dict, err := s.dicts.FindDictByID(ctx, dictID)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	if dict != nil && dict.IsTree == 1 {
		return domain.MapDictDataToSelectTree(domain.BuildDictDataTree(vos)), nil
	}
	options := make([]domain.SelectVO, 0, len(vos))
	for _, vo := range vos {
		options = append(options, domain.SelectVO{Label: vo.Name, Value: vo.Value})
	}
	return options, nil
}

// CreateDataItem。
func (s *Service) CreateDataItem(ctx context.Context, rc RequestContext, dictID string, payload domain.DictDataSavePayload) (*domain.AdminDictDataEntity, error) {
	realID, err := s.resolveDictID(ctx, dictID)
	if err != nil {
		return nil, err
	}
	if realID == "" {
		return nil, business(MessageDictNotFound)
	}
	dict, err := s.dicts.FindDictByID(ctx, realID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, business(MessageDictNotFound)
		}
		return nil, err
	}
	if err := s.validateFrontendLabelCode(dict, payload.Code); err != nil {
		return nil, err
	}
	if payload.ParentID != nil {
		parent, parentErr := s.dicts.FindDataItemByID(ctx, *payload.ParentID)
		if parentErr != nil {
			if errors.Is(parentErr, repo.ErrNotFound) {
				return nil, business(MessageDictDataParentNotFd)
			}
			return nil, parentErr
		}
		if parent.DictID != realID {
			return nil, business(MessageDictDataParentNotFd)
		}
	}
	isEnabled := 1
	if payload.IsEnabled != nil {
		isEnabled = *payload.IsEnabled
	}
	sortOrder := 0
	if payload.SortOrder != nil {
		sortOrder = *payload.SortOrder
	}
	now := s.now()
	loginID := rc.LoginID()
	record := domain.DictDataRecord{
		ID:          s.nextID(),
		DictID:      realID,
		ParentID:    payload.ParentID,
		Code:        domain.DerefString(payload.Code),
		Name:        domain.DerefString(resolveDefaultNameOrNil(payload.Name, payload.I18nValue)),
		Value:       domain.DerefString(payload.Value),
		AgeLevel:    intValueOr(payload.AgeLevel, 8),
		SortOrder:   sortOrder,
		IsEnabled:   isEnabled,
		Description: payload.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
		Version:     0,
	}
	record.CreateBy = &loginID
	record.UpdateBy = &loginID
	// 事务边界：数据项 + i18n 标签同生共死。
	if err := s.tx.Within(ctx, func(ctx context.Context) error {
		if err := s.dicts.InsertDataItem(ctx, record); err != nil {
			return err
		}
		return s.labels.SaveNameLabels(ctx, domain.I18nTableDictData, record.ID, payload.I18nValue)
	}); err != nil {
		return nil, err
	}
	entity := dictDataEntityFromRecord(record)
	entity.IsDeleted = intPtr(0)
	entity.I18nValue = payload.I18nValue
	return &entity, nil
}

// UpdateDataItem。
func (s *Service) UpdateDataItem(ctx context.Context, rc RequestContext, dictID, id string, payload domain.DictDataSavePayload) (*domain.AdminDictDataEntity, error) {
	realID, err := s.resolveDictID(ctx, dictID)
	if err != nil {
		return nil, err
	}
	if realID == "" {
		return nil, business(MessageDictNotFound)
	}
	existing, err := s.dicts.FindDataItemByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, business(MessageDictDataNotFound)
		}
		return nil, err
	}
	if existing.DictID != realID {
		return nil, business(MessageDictDataNotFound)
	}
	dict, err := s.dicts.FindDictByID(ctx, realID)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	codeForValidation := payload.Code
	if codeForValidation == nil {
		codeForValidation = &existing.Code
	}
	if err := s.validateFrontendLabelCode(dict, codeForValidation); err != nil {
		return nil, err
	}

	if payload.Code != nil {
		existing.Code = *payload.Code
	}
	if resolved := resolveDefaultNameOrNil(payload.Name, payload.I18nValue); resolved != nil {
		existing.Name = *resolved
	}
	if payload.Value != nil {
		existing.Value = *payload.Value
	}
	if payload.AgeLevel != nil {
		existing.AgeLevel = *payload.AgeLevel
	}
	if payload.ParentID != nil {
		existing.ParentID = payload.ParentID
	}
	if payload.SortOrder != nil {
		existing.SortOrder = *payload.SortOrder
	}
	if payload.IsEnabled != nil {
		existing.IsEnabled = *payload.IsEnabled
	}
	if payload.Description != nil {
		existing.Description = payload.Description
	}
	loginID := rc.LoginID()
	existing.UpdateBy = &loginID
	existing.UpdatedAt = s.now()
	// 事务边界：数据项 + i18n 标签同生共死。
	err = s.tx.Within(ctx, func(ctx context.Context) error {
		affected, err := s.dicts.UpdateDataItem(ctx, *existing)
		if err != nil {
			return err
		}
		s.warnIfNotUpdated(writeFact("updateDataItem", TableDictData, existing.ID, &existing.Version, affected))
		return s.labels.SaveNameLabels(ctx, domain.I18nTableDictData, existing.ID, payload.I18nValue)
	})
	if err != nil {
		return nil, err
	}
	existing.Version++
	entity := dictDataEntityFromRecord(*existing)
	entity.IsDeleted = nil
	entity.CreateBy = nil
	return &entity, nil
}

// DeleteDataItem。
func (s *Service) DeleteDataItem(ctx context.Context, dictID, id string, cascade bool) error {
	realID, err := s.resolveDictID(ctx, dictID)
	if err != nil {
		return err
	}
	if realID == "" {
		return business(MessageDictNotFound)
	}
	existing, err := s.dicts.FindDataItemByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return business(MessageDictDataNotFound)
		}
		return err
	}
	if existing.DictID != realID {
		return business(MessageDictDataNotFound)
	}
	// 事务边界：级联软删 + 标签清理同生共死。
	return s.tx.Within(ctx, func(ctx context.Context) error {
		if !cascade {
			deleted, err := s.dicts.SoftDeleteDataItem(ctx, id)
			if err != nil {
				return err
			}
			s.warnIfNotUpdated(writeFact("deleteDataItem", TableDictData, id, nil, deleted))
			labels, err := s.labels.DeleteNameLabels(ctx, domain.I18nTableDictData, id)
			if err != nil {
				return err
			}
			s.warnIfNotUpdated(writeFact("deleteDataItem.labels", TableI18nLabel, id, nil, labels))
			return nil
		}
		ids, err := s.collectDescendantIDs(ctx, realID, id)
		if err != nil {
			return err
		}
		ids = append(ids, id)
		for _, target := range ids {
			deleted, err := s.dicts.SoftDeleteDataItem(ctx, target)
			if err != nil {
				return err
			}
			s.warnIfNotUpdated(writeFact("deleteDataItem.cascade", TableDictData, target, nil, deleted))
			labels, err := s.labels.DeleteNameLabels(ctx, domain.I18nTableDictData, target)
			if err != nil {
				return err
			}
			s.warnIfNotUpdated(writeFact("deleteDataItem.cascadeLabels", TableI18nLabel, target, nil, labels))
		}
		return nil
	})
}

// BatchSaveDataItems。
// 按 id + isDeleted 决定新增/修改/删除，非全量替换；返回 data=null。
func (s *Service) BatchSaveDataItems(ctx context.Context, rc RequestContext, dictID string, payloads []domain.DictDataSavePayload) error {
	// 事务边界：整批新增/修改/删除 + 标签同生共死。
	return s.tx.Within(ctx, func(ctx context.Context) error {
		return s.batchSaveDataItems(ctx, rc, dictID, payloads)
	})
}

// batchSaveDataItems 是 BatchSaveDataItems 的事务体内实现。
func (s *Service) batchSaveDataItems(ctx context.Context, rc RequestContext, dictID string, payloads []domain.DictDataSavePayload) error {
	realID, err := s.resolveDictID(ctx, dictID)
	if err != nil {
		return err
	}
	if realID == "" {
		return business(MessageDictNotFound)
	}
	dict, err := s.dicts.FindDictByID(ctx, realID)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return err
	}
	// 预校验：任一条编码非法则整批失败。
	for index := range payloads {
		if err := s.validateFrontendLabelCode(dict, payloads[index].Code); err != nil {
			return err
		}
		payloads[index].DictID = &realID
		if resolved := resolveDefaultNameOrNil(payloads[index].Name, payloads[index].I18nValue); resolved != nil {
			payloads[index].Name = resolved
		}
	}
	now := s.now()
	loginID := rc.LoginID()
	for index := range payloads {
		payload := payloads[index]
		switch {
		case payload.ID == nil && !payload.DictDataDeleted():
			record := domain.DictDataRecord{
				ID:          s.nextID(),
				DictID:      realID,
				ParentID:    payload.ParentID,
				Code:        domain.DerefString(payload.Code),
				Name:        domain.DerefString(payload.Name),
				Value:       domain.DerefString(payload.Value),
				AgeLevel:    intValueOr(payload.AgeLevel, 8),
				SortOrder:   intValueOr(payload.SortOrder, 0),
				IsEnabled:   intValueOr(payload.IsEnabled, 1),
				Description: payload.Description,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			record.CreateBy = &loginID
			record.UpdateBy = &loginID
			if err := s.dicts.InsertDataItem(ctx, record); err != nil {
				return err
			}
			inserted := record.ID
			payloads[index].ID = &inserted
		case payload.ID != nil && !payload.DictDataDeleted():
			record := domain.DictDataRecord{
				ID:          *payload.ID,
				DictID:      realID,
				ParentID:    payload.ParentID,
				Code:        domain.DerefString(payload.Code),
				Name:        domain.DerefString(payload.Name),
				Value:       domain.DerefString(payload.Value),
				AgeLevel:    intValueOr(payload.AgeLevel, 8),
				SortOrder:   intValueOr(payload.SortOrder, 0),
				IsEnabled:   intValueOr(payload.IsEnabled, 1),
				Description: payload.Description,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			record.UpdateBy = &loginID
			affected, err := s.dicts.UpdateDataItemByID(ctx, record)
			if err != nil {
				return err
			}
			s.warnIfNotUpdated(writeFact("batchSaveDataItems.update", TableDictData, record.ID, nil, affected))
		case payload.ID != nil && payload.DictDataDeleted():
			deleted, err := s.dicts.SoftDeleteDataItem(ctx, *payload.ID)
			if err != nil {
				return err
			}
			// 删除/撤销类：目标已不存在即达到目标终态，按幂等成功处理。
			s.warnIfNotUpdated(writeFact("batchSaveDataItems.delete", TableDictData, *payload.ID, nil, deleted))
		default:
			// id == null && isDeleted == 1：收集到 errors 后静默跳过。
		}
	}
	for index := range payloads {
		payload := payloads[index]
		if payload.ID == nil {
			continue
		}
		if payload.DictDataDeleted() {
			labels, err := s.labels.DeleteNameLabels(ctx, domain.I18nTableDictData, *payload.ID)
			if err != nil {
				return err
			}
			s.warnIfNotUpdated(writeFact("batchSaveDataItems.deleteLabels", TableI18nLabel, *payload.ID, nil, labels))
			continue
		}
		if err := s.labels.SaveNameLabels(ctx, domain.I18nTableDictData, *payload.ID, payload.I18nValue); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 内部工具
// ---------------------------------------------------------------------------

// resolveDictID。
func (s *Service) resolveDictID(ctx context.Context, dictID string) (string, error) {
	if strings.TrimSpace(dictID) == "" {
		return "", nil
	}
	record, err := s.dicts.FindDictByID(ctx, dictID)
	if err == nil {
		return record.ID, nil
	}
	if !errors.Is(err, repo.ErrNotFound) {
		return "", err
	}
	return s.dictIDByCode(ctx, dictID)
}

// dictIDByCode。
func (s *Service) dictIDByCode(ctx context.Context, code string) (string, error) {
	if strings.TrimSpace(code) == "" {
		return "", nil
	}
	record, err := s.dicts.FindDictByCode(ctx, code)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return record.ID, nil
}

// collectDescendantIDs。
func (s *Service) collectDescendantIDs(ctx context.Context, dictID, parentID string) ([]string, error) {
	children, err := s.dicts.ListChildDataItems(ctx, dictID, parentID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(children))
	for _, child := range children {
		out = append(out, child.ID)
		nested, nestedErr := s.collectDescendantIDs(ctx, dictID, child.ID)
		if nestedErr != nil {
			return nil, nestedErr
		}
		out = append(out, nested...)
	}
	return out, nil
}

// validateFrontendLabelCode。
func (s *Service) validateFrontendLabelCode(dict *domain.DictRecord, code *string) error {
	if dict == nil {
		return nil
	}
	var expectedScope string
	switch dict.Code {
	case domain.FrontendAdminDictCode:
		expectedScope = "admin"
	case domain.FrontendAppDictCode:
		expectedScope = "app"
	default:
		return nil
	}
	if code == nil || !frontendLabelCodePattern.MatchString(*code) ||
		!strings.HasPrefix(*code, "acat.read."+expectedScope+".") {
		return business("前端国际化编码必须符合 acat.read." + expectedScope +
			".{nav}.{menu}/{page}.{具体显示简意}")
	}
	return nil
}

// toDictVO。
func (s *Service) toDictVO(ctx context.Context, rc RequestContext, record domain.DictRecord) (domain.DictVO, error) {
	name, err := s.labels.ResolveCurrentName(ctx, domain.I18nTableDict, record.ID, rc.Language())
	if err != nil {
		return domain.DictVO{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = record.Name
	}
	labels, err := s.labels.ListNameLabels(ctx, domain.I18nTableDict, record.ID)
	if err != nil {
		return domain.DictVO{}, err
	}
	count, err := s.dicts.CountDataItemsByDictID(ctx, record.ID)
	if err != nil {
		return domain.DictVO{}, err
	}
	description := record.Description
	return domain.DictVO{
		ID:          record.ID,
		Code:        record.Code,
		Name:        name,
		IsEnabled:   intPtr(record.IsEnabled),
		IsTree:      intPtr(record.IsTree),
		Scope:       intPtr(record.Scope),
		Description: description,
		I18nValue:   toI18nValues(labels),
		DataItems:   nil,
		DataCount:   int(count),
		CreatedAt:   domain.StringOrNil(formatTime(record.CreatedAt)),
		UpdatedAt:   domain.StringOrNil(formatTime(record.UpdatedAt)),
	}, nil
}

// toDictDataVOs。
func (s *Service) toDictDataVOs(ctx context.Context, rc RequestContext, records []domain.DictDataRecord) ([]domain.DictDataVO, error) {
	out := make([]domain.DictDataVO, 0, len(records))
	for _, record := range records {
		name, err := s.labels.ResolveCurrentName(ctx, domain.I18nTableDictData, record.ID, rc.Language())
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(name) == "" {
			name = record.Name
		}
		labels, err := s.labels.ListNameLabels(ctx, domain.I18nTableDictData, record.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.DictDataVO{
			ID:          record.ID,
			DictID:      record.DictID,
			ParentID:    record.ParentID,
			Code:        record.Code,
			Name:        name,
			Value:       record.Value,
			AgeLevel:    intPtr(record.AgeLevel),
			SortOrder:   intPtr(record.SortOrder),
			IsEnabled:   intPtr(record.IsEnabled),
			Description: record.Description,
			I18nValue:   toI18nValues(labels),
			Children:    nil,
			CreatedAt:   domain.StringOrNil(formatTime(record.CreatedAt)),
		})
	}
	return out, nil
}

// filterDataItemsByName。
func filterDataItemsByName(records []domain.DictDataRecord, name string) []domain.DictDataRecord {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return records
	}
	lowered := strings.ToLower(trimmed)
	out := make([]domain.DictDataRecord, 0, len(records))
	for _, record := range records {
		if strings.Contains(strings.ToLower(record.Name), lowered) ||
			strings.Contains(strings.ToLower(record.Code), lowered) {
			out = append(out, record)
		}
	}
	return out
}

// treeWindow。
//
// Go 侧按公共库 NormalizePage 口径收敛为第 1 页（差异记录在 README）。
func treeWindow(pageIndex, pageSize, length int) (int, int) {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize < 0 {
		pageSize = 0
	}
	from := (pageIndex - 1) * pageSize
	if from > length {
		from = length
	}
	to := from + pageSize
	if to > length {
		to = length
	}
	return from, to
}

// dictEntityFromRecord 把数据库行转换为响应实体。
func dictEntityFromRecord(record domain.DictRecord) domain.AdminDictEntity {
	createdAt := formatTime(record.CreatedAt)
	updatedAt := formatTime(record.UpdatedAt)
	return domain.AdminDictEntity{
		ID:          record.ID,
		CreatedAt:   domain.StringOrNil(createdAt),
		UpdatedAt:   domain.StringOrNil(updatedAt),
		Version:     intPtr(record.Version),
		Code:        record.Code,
		Name:        record.Name,
		IsEnabled:   intPtr(record.IsEnabled),
		IsTree:      intPtr(record.IsTree),
		Scope:       intPtr(record.Scope),
		Description: record.Description,
	}
}

// dictDataEntityFromRecord 把数据库行转换为响应实体。
func dictDataEntityFromRecord(record domain.DictDataRecord) domain.AdminDictDataEntity {
	createdAt := formatTime(record.CreatedAt)
	updatedAt := formatTime(record.UpdatedAt)
	return domain.AdminDictDataEntity{
		ID:          record.ID,
		CreatedAt:   domain.StringOrNil(createdAt),
		UpdatedAt:   domain.StringOrNil(updatedAt),
		Version:     intPtr(record.Version),
		DictID:      record.DictID,
		ParentID:    record.ParentID,
		Code:        record.Code,
		Name:        record.Name,
		Value:       record.Value,
		AgeLevel:    intPtr(record.AgeLevel),
		SortOrder:   intPtr(record.SortOrder),
		IsEnabled:   intPtr(record.IsEnabled),
		Description: record.Description,
	}
}

func intPtr(value int) *int { return &value }

func intValueOr(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

// formatTime 把数据库时间转换为 时间文本。
func formatTime(value time.Time) string {
	return domain.FormatDateTime(value)
}
