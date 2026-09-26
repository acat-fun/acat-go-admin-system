package service

import (
	"context"
	"errors"
	"strings"

	"github.com/acat-fun/acat-go-admin-system/domain"
	"github.com/acat-fun/acat-go-admin-system/repo"
)

// I18nTypeSavePayload。
type I18nTypeSavePayload struct {
	Code      *string `json:"code"`
	Name      *string `json:"name"`
	SortOrder *int    `json:"sortOrder"`
	IsEnabled *int    `json:"isEnabled"`
	IsBuiltin *int    `json:"isBuiltin"`
}

// ListTypeOptions。
func (s *Service) ListTypeOptions(ctx context.Context) ([]domain.SelectVO, error) {
	records, err := s.i18nTypes.ListEnabledTypes(ctx)
	if err != nil {
		return nil, err
	}
	options := make([]domain.SelectVO, 0, len(records))
	for _, record := range records {
		options = append(options, domain.SelectVO{Label: record.Name, Value: record.Code})
	}
	return options, nil
}

// ListTypes。
func (s *Service) ListTypes(ctx context.Context, pageIndex, pageSize int, keyword string, isEnabled *int) (any, error) {
	records, total, err := s.i18nTypes.ListTypesPaged(ctx, keyword, isEnabled, pageIndex, pageSize)
	if err != nil {
		return nil, err
	}
	items := make([]domain.I18nTypeEntity, 0, len(records))
	for _, record := range records {
		items = append(items, i18nTypeEntityFromRecord(record))
	}
	return newPageData(items, total, pageIndex, pageSize), nil
}

// ListAllTypes。
func (s *Service) ListAllTypes(ctx context.Context) ([]domain.I18nTypeEntity, error) {
	records, err := s.i18nTypes.ListEnabledTypes(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]domain.I18nTypeEntity, 0, len(records))
	for _, record := range records {
		items = append(items, i18nTypeEntityFromRecord(record))
	}
	return items, nil
}

// FrontendLabels。
// dictCode 为空时回退 i18n_admin_label；语言取 Accept-Language 原样值。
//
// 返回有序 Map：插入顺序为 data.sort_order ASC, data.code ASC，
// 每个键后紧跟其历史兼容别名（putIfAbsent，不覆盖已存在的 code）。
func (s *Service) FrontendLabels(ctx context.Context, rc RequestContext, dictCode string) (*domain.OrderedMap, error) {
	resolved := strings.TrimSpace(dictCode)
	if resolved == "" {
		resolved = DefaultFrontendDictCode
	}
	records, err := s.labels.ListFrontendLabels(ctx, resolved, rc.Language())
	if err != nil {
		return nil, err
	}
	labels := domain.NewOrderedMap()
	for _, record := range records {
		if strings.TrimSpace(record.Code) == "" {
			continue
		}
		labels.Put(record.Code, record.LabelValue)
		if alias := domain.LegacyFrontendAlias(record.Code); alias != "" {
			labels.PutIfAbsent(alias, record.LabelValue)
		}
	}
	return labels, nil
}

// CreateType。
func (s *Service) CreateType(ctx context.Context, rc RequestContext, payload I18nTypeSavePayload) (*domain.I18nTypeEntity, error) {
	code := domain.DerefString(payload.Code)
	exists, err := s.i18nTypes.CountTypesByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if exists > 0 {
		return nil, business(MessageI18nCodeExists + code)
	}
	now := s.now()
	record := domain.I18nTypeRecord{
		ID:        s.nextID(),
		Code:      code,
		Name:      domain.DerefString(payload.Name),
		SortOrder: intValueOr(payload.SortOrder, 0),
		IsEnabled: intValueOr(payload.IsEnabled, 1),
		IsBuiltin: intValueOr(payload.IsBuiltin, 0),
		CreatedAt: now,
		UpdatedAt: now,
		Version:   0,
	}
	if err := s.i18nTypes.InsertType(ctx, record); err != nil {
		return nil, err
	}
	entity := i18nTypeEntityFromRecord(record)
	entity.IsDeleted = intPtr(0)
	loginID := rc.LoginID()
	entity.CreateBy = &loginID
	entity.UpdateBy = &loginID
	return &entity, nil
}

// UpdateType。
func (s *Service) UpdateType(ctx context.Context, rc RequestContext, id string, payload I18nTypeSavePayload) (*domain.I18nTypeEntity, error) {
	existing, err := s.i18nTypes.FindTypeByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, business(MessageI18nTypeGone)
		}
		return nil, err
	}
	if payload.Code != nil {
		existing.Code = *payload.Code
	}
	if payload.Name != nil {
		existing.Name = *payload.Name
	}
	if payload.SortOrder != nil {
		existing.SortOrder = *payload.SortOrder
	}
	if payload.IsEnabled != nil {
		existing.IsEnabled = *payload.IsEnabled
	}
	if payload.IsBuiltin != nil {
		existing.IsBuiltin = *payload.IsBuiltin
	}
	existing.UpdatedAt = s.now()
	if _, err := s.i18nTypes.UpdateType(ctx, *existing); err != nil {
		return nil, err
	}
	existing.Version++
	entity := i18nTypeEntityFromRecord(*existing)
	entity.IsDeleted = nil
	entity.CreateBy = nil
	return &entity, nil
}

// DeleteType。
func (s *Service) DeleteType(ctx context.Context, id string) error {
	existing, err := s.i18nTypes.FindTypeByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return business(MessageI18nTypeGone)
		}
		return err
	}
	if existing.IsBuiltin == 1 {
		return business(MessageI18nTypeBuiltinUndeletable)
	}
	deleted, err := s.i18nTypes.SoftDeleteType(ctx, id)
	if err != nil {
		return err
	}
	// 删除/撤销类：记录已不存在即达到目标终态，按幂等成功处理。
	s.warnIfNotUpdated(writeFact("deleteI18nType", TableI18nType, id, nil, deleted))
	return nil
}

// i18nTypeEntityFromRecord 把数据库行转换为响应实体。
func i18nTypeEntityFromRecord(record domain.I18nTypeRecord) domain.I18nTypeEntity {
	return domain.I18nTypeEntity{
		ID:        record.ID,
		CreatedAt: domain.StringOrNil(domain.FormatDateTime(record.CreatedAt)),
		UpdatedAt: domain.StringOrNil(domain.FormatDateTime(record.UpdatedAt)),
		Version:   intPtr(record.Version),
		Code:      record.Code,
		Name:      record.Name,
		SortOrder: intPtr(record.SortOrder),
		IsEnabled: intPtr(record.IsEnabled),
		IsBuiltin: intPtr(record.IsBuiltin),
	}
}
