package service

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/repo"
)

// 本文件提供各 repo 接口的内存假实现：service 层单测不依赖 MySQL，
// 真实 SQL 由 internal/repo 的 sqlmock 测试覆盖。

// fakeDictRepo 同时实现 DictRepo 与 LabelRepo。
type fakeDictRepo struct {
	mu     sync.Mutex
	dicts  map[string]domain.DictRecord
	data   map[string]domain.DictDataRecord
	labels map[string]domain.LabelRecord // key: sourceTable|tableDataID|i18nCode
	// frontendLabels 模拟 selectFrontendLabels 的返回（按插入顺序）。
	frontendLabels []domain.FrontendLabelRecord
	insertOrder    []string
}

func newFakeDictRepo() *fakeDictRepo {
	return &fakeDictRepo{
		dicts:  map[string]domain.DictRecord{},
		data:   map[string]domain.DictDataRecord{},
		labels: map[string]domain.LabelRecord{},
	}
}

func (f *fakeDictRepo) addDict(record domain.DictRecord) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dicts[record.ID] = record
}

func (f *fakeDictRepo) addData(record domain.DictDataRecord) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[record.ID] = record
}

func labelKey(sourceTable, tableDataID, i18nCode string) string {
	return sourceTable + "|" + tableDataID + "|" + i18nCode
}

func (f *fakeDictRepo) ListDicts(_ context.Context, filter domain.DictFilter, pageIndex, pageSize int) ([]domain.DictRecord, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.DictRecord, 0, len(f.dicts))
	for _, record := range f.dicts {
		if filter.Name != "" && !strings.Contains(strings.ToLower(record.Name), strings.ToLower(filter.Name)) {
			continue
		}
		if filter.IsEnabled != nil && record.IsEnabled != *filter.IsEnabled {
			continue
		}
		if filter.IsTree != nil && record.IsTree != *filter.IsTree {
			continue
		}
		if filter.Scope != nil && record.Scope != *filter.Scope {
			continue
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	total := int64(len(out))
	out = paginate(out, pageIndex, pageSize)
	return out, total, nil
}

func (f *fakeDictRepo) ListAllDicts(_ context.Context) ([]domain.DictRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.DictRecord, 0, len(f.dicts))
	for _, record := range f.dicts {
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (f *fakeDictRepo) ListDictsForSelect(context.Context) ([]domain.DictRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.DictRecord, 0, len(f.dicts))
	for _, record := range f.dicts {
		out = append(out, record)
	}
	return out, nil
}

func (f *fakeDictRepo) CountDataItemsByDictID(_ context.Context, dictID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var count int64
	for _, record := range f.data {
		if record.DictID == dictID {
			count++
		}
	}
	return count, nil
}

func (f *fakeDictRepo) FindDictByID(_ context.Context, id string) (*domain.DictRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.dicts[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return &record, nil
}

func (f *fakeDictRepo) FindDictByCode(_ context.Context, code string) (*domain.DictRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var found *domain.DictRecord
	for _, record := range f.dicts {
		if record.Code == code {
			if found != nil {
				return nil, repo.ErrMultipleResults
			}
			copyRecord := record
			found = &copyRecord
		}
	}
	if found == nil {
		return nil, repo.ErrNotFound
	}
	return found, nil
}

func (f *fakeDictRepo) CountDictsByCode(_ context.Context, code string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var count int64
	for _, record := range f.dicts {
		if record.Code == code {
			count++
		}
	}
	return count, nil
}

func (f *fakeDictRepo) InsertDict(_ context.Context, record domain.DictRecord) error {
	f.addDict(record)
	f.mu.Lock()
	f.insertOrder = append(f.insertOrder, record.ID)
	f.mu.Unlock()
	return nil
}

func (f *fakeDictRepo) UpdateDict(_ context.Context, record domain.DictRecord) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.dicts[record.ID]
	if !ok || existing.Version != record.Version {
		return 0, nil
	}
	f.dicts[record.ID] = record
	return 1, nil
}

func (f *fakeDictRepo) SoftDeleteDict(_ context.Context, id string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.dicts, id)
	return 1, nil
}

func (f *fakeDictRepo) ListDataItems(_ context.Context, dictID string, filter domain.DictDataFilter, pageIndex, pageSize int) ([]domain.DictDataRecord, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.DictDataRecord, 0, len(f.data))
	for _, record := range f.data {
		if record.DictID != dictID {
			continue
		}
		if filter.Name != "" && !strings.Contains(strings.ToLower(record.Name), strings.ToLower(filter.Name)) &&
			!strings.Contains(strings.ToLower(record.Code), strings.ToLower(filter.Name)) {
			continue
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ID < out[j].ID
	})
	total := int64(len(out))
	return paginate(out, pageIndex, pageSize), total, nil
}

func (f *fakeDictRepo) ListAllDataItems(_ context.Context, dictID string, isEnabled *int) ([]domain.DictDataRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.DictDataRecord, 0, len(f.data))
	for _, record := range f.data {
		if record.DictID != dictID {
			continue
		}
		if isEnabled != nil && record.IsEnabled != *isEnabled {
			continue
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (f *fakeDictRepo) FindDataItemByID(_ context.Context, id string) (*domain.DictDataRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.data[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return &record, nil
}

func (f *fakeDictRepo) ListChildDataItems(_ context.Context, dictID, parentID string) ([]domain.DictDataRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.DictDataRecord, 0, 4)
	for _, record := range f.data {
		if record.DictID == dictID && record.ParentID != nil && *record.ParentID == parentID {
			out = append(out, record)
		}
	}
	return out, nil
}

func (f *fakeDictRepo) InsertDataItem(_ context.Context, record domain.DictDataRecord) error {
	f.addData(record)
	return nil
}

func (f *fakeDictRepo) UpdateDataItem(_ context.Context, record domain.DictDataRecord) (int64, error) {
	return f.updateData(record, true)
}

func (f *fakeDictRepo) UpdateDataItemByID(_ context.Context, record domain.DictDataRecord) (int64, error) {
	return f.updateData(record, false)
}

func (f *fakeDictRepo) updateData(record domain.DictDataRecord, checkVersion bool) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.data[record.ID]
	if !ok {
		return 0, nil
	}
	if checkVersion && existing.Version != record.Version {
		return 0, nil
	}
	existing.Code = record.Code
	existing.Name = record.Name
	existing.Value = record.Value
	existing.ParentID = record.ParentID
	existing.SortOrder = record.SortOrder
	existing.IsEnabled = record.IsEnabled
	existing.Color = record.Color
	existing.IsBuiltin = record.IsBuiltin
	existing.Description = record.Description
	existing.UpdatedAt = record.UpdatedAt
	existing.Version++
	if record.Code != "" {
		existing.Code = record.Code
	}
	f.data[record.ID] = existing
	return 1, nil
}

func (f *fakeDictRepo) SoftDeleteDataItem(_ context.Context, id string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, id)
	return 1, nil
}

func (f *fakeDictRepo) ListNameLabels(_ context.Context, sourceTable, tableDataID string) ([]domain.LabelRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.LabelRecord, 0, 2)
	for key, record := range f.labels {
		if strings.HasPrefix(key, sourceTable+"|"+tableDataID+"|") {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].I18nCode < out[j].I18nCode })
	return out, nil
}

func (f *fakeDictRepo) ResolveCurrentName(_ context.Context, sourceTable, tableDataID, i18nCode string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.labels[labelKey(sourceTable, tableDataID, i18nCode)]
	if !ok {
		return "", nil
	}
	return record.LabelValue, nil
}

func (f *fakeDictRepo) SaveNameLabels(_ context.Context, sourceTable, tableDataID string, values []domain.I18nValue) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, value := range values {
		if strings.TrimSpace(value.I18n) == "" || strings.TrimSpace(value.Value) == "" {
			continue
		}
		f.labels[labelKey(sourceTable, tableDataID, value.I18n)] = domain.LabelRecord{
			I18nCode: value.I18n, LabelValue: value.Value,
		}
	}
	return nil
}

func (f *fakeDictRepo) DeleteNameLabels(_ context.Context, sourceTable, tableDataID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key := range f.labels {
		if strings.HasPrefix(key, sourceTable+"|"+tableDataID+"|") {
			delete(f.labels, key)
		}
	}
	return 1, nil
}

func (f *fakeDictRepo) ListFrontendLabels(context.Context, string, string) ([]domain.FrontendLabelRecord, error) {
	return f.frontendLabels, nil
}

// fakePageRepo 实现 PageRepo。
type fakePageRepo struct {
	mu      sync.Mutex
	pages   map[string]domain.PageRecord
	deleted map[string]domain.PageRecord
	history []domain.PageHistoryRecord
	grants  []string
	// deletedGrantRestores 记录恢复授权的调用顺序（用于断言调用次序）。
	callOrder []string
}

func newFakePageRepo() *fakePageRepo {
	return &fakePageRepo{pages: map[string]domain.PageRecord{}, deleted: map[string]domain.PageRecord{}}
}

func (f *fakePageRepo) addPage(record domain.PageRecord) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages[record.ID] = record
}

func (f *fakePageRepo) ListPagesByScope(_ context.Context, scope int, _ string) ([]domain.PageRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.PageRecord, 0, len(f.pages))
	for _, record := range f.pages {
		if record.Scope != scope {
			continue
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (f *fakePageRepo) ListPagesByParentID(_ context.Context, parentID, _ string) ([]domain.PageRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.PageRecord, 0, 4)
	for _, record := range f.pages {
		if record.ParentID != nil && *record.ParentID == parentID {
			out = append(out, record)
		}
	}
	return out, nil
}

func (f *fakePageRepo) FindPageByID(_ context.Context, id string) (*domain.PageRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.pages[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return &record, nil
}

func (f *fakePageRepo) FindPageByCode(_ context.Context, code string) (*domain.PageRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, record := range f.pages {
		if record.Code == code {
			copyRecord := record
			return &copyRecord, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (f *fakePageRepo) FindDeletedPageByCode(_ context.Context, code string) (*domain.PageRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, record := range f.deleted {
		if record.Code == code {
			copyRecord := record
			return &copyRecord, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (f *fakePageRepo) RestoreDeletedPage(_ context.Context, record domain.PageRecord) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.deleted[record.ID]; !ok {
		return 0, nil
	}
	delete(f.deleted, record.ID)
	f.pages[record.ID] = record
	return 1, nil
}

func (f *fakePageRepo) InsertPage(_ context.Context, record domain.PageRecord) error {
	f.addPage(record)
	return nil
}

func (f *fakePageRepo) UpdatePage(_ context.Context, record domain.PageRecord) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.pages[record.ID]
	if !ok || existing.Version != record.Version {
		return 0, nil
	}
	f.pages[record.ID] = record
	return 1, nil
}

func (f *fakePageRepo) SoftDeletePage(_ context.Context, id string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if record, ok := f.pages[id]; ok {
		f.deleted[id] = record
		delete(f.pages, id)
	}
	return 1, nil
}

func (f *fakePageRepo) SelectPageIDsByPermissions(_ context.Context, permissions []string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	allowed := map[string]struct{}{}
	for _, permission := range permissions {
		allowed[permission] = struct{}{}
	}
	out := make([]string, 0, 4)
	for _, record := range f.pages {
		if _, ok := allowed[record.Code]; ok {
			out = append(out, record.ID)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (f *fakePageRepo) InsertPageHistory(_ context.Context, record domain.PageHistoryRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.history = append(f.history, record)
	return nil
}

func (f *fakePageRepo) GrantPageToRootAndAdmin(_ context.Context, pageID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants = append(f.grants, pageID)
	f.callOrder = append(f.callOrder, "grant")
	return nil
}

func (f *fakePageRepo) RestoreRolePermissionsByPageID(_ context.Context, pageID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callOrder = append(f.callOrder, "restore")
	return 1, nil
}

func (f *fakePageRepo) DeleteRolePermissionsByPageID(_ context.Context, _ string) (int64, error) {
	return 1, nil
}

// fakeModuleRepo 实现 FrontendModuleRepo。
type fakeModuleRepo struct {
	mu      sync.Mutex
	modules map[string]domain.FrontendModuleRecord
	// forceUpdateMiss 模拟并发导致的 0 行受影响（用于 publish 的 40901 分支）。
	forceUpdateMiss bool
}

func newFakeModuleRepo() *fakeModuleRepo {
	return &fakeModuleRepo{modules: map[string]domain.FrontendModuleRecord{}}
}

func (f *fakeModuleRepo) addModule(record domain.FrontendModuleRecord) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.modules[record.ID] = record
}

func (f *fakeModuleRepo) ListModules(_ context.Context, moduleCode string) ([]domain.FrontendModuleRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.FrontendModuleRecord, 0, len(f.modules))
	for _, record := range f.modules {
		if strings.TrimSpace(moduleCode) != "" && record.ModuleCode != moduleCode {
			continue
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ModuleCode < out[j].ModuleCode
	})
	return out, nil
}

func (f *fakeModuleRepo) FindModuleByID(_ context.Context, id string) (*domain.FrontendModuleRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.modules[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return &record, nil
}

func (f *fakeModuleRepo) FindModuleByCodeAndVersion(_ context.Context, moduleCode, releaseVersion string) (*domain.FrontendModuleRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, record := range f.modules {
		if record.ModuleCode == moduleCode && record.ReleaseVersion == releaseVersion {
			copyRecord := record
			return &copyRecord, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (f *fakeModuleRepo) InsertModule(_ context.Context, record domain.FrontendModuleRecord) error {
	f.addModule(record)
	return nil
}

func (f *fakeModuleRepo) UpdateModule(_ context.Context, record domain.FrontendModuleRecord) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.forceUpdateMiss {
		return 0, nil
	}
	existing, ok := f.modules[record.ID]
	if !ok || existing.Version != record.Version {
		return 0, nil
	}
	f.modules[record.ID] = record
	return 1, nil
}

func (f *fakeModuleRepo) DisableOtherEnabledVersions(_ context.Context, moduleCode, keepID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, record := range f.modules {
		if record.ModuleCode == moduleCode && id != keepID && record.Status == domain.FrontendModuleStatusEnabled {
			record.Status = domain.FrontendModuleStatusDisabled
			f.modules[id] = record
		}
	}
	return nil
}

func (f *fakeModuleRepo) ListEnabledModuleCodes(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.modules))
	for _, record := range f.modules {
		if record.Status == domain.FrontendModuleStatusEnabled {
			out = append(out, record.ModuleCode)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (f *fakeModuleRepo) CountEnabledModuleByCode(_ context.Context, moduleCode string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var count int64
	for _, record := range f.modules {
		if record.ModuleCode == moduleCode && record.Status == domain.FrontendModuleStatusEnabled {
			count++
		}
	}
	return count, nil
}

func (f *fakeModuleRepo) ListEnabledModulesByCodes(_ context.Context, codes []string) ([]domain.FrontendModuleRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	wanted := map[string]struct{}{}
	for _, code := range codes {
		wanted[code] = struct{}{}
	}
	out := make([]domain.FrontendModuleRecord, 0, len(codes))
	for _, record := range f.modules {
		if record.Status != domain.FrontendModuleStatusEnabled {
			continue
		}
		if _, ok := wanted[record.ModuleCode]; !ok {
			continue
		}
		out = append(out, record)
	}
	return out, nil
}

// fakeI18nTypeRepo 实现 I18nTypeRepo。
type fakeI18nTypeRepo struct {
	mu    sync.Mutex
	types map[string]domain.I18nTypeRecord
}

func newFakeI18nTypeRepo() *fakeI18nTypeRepo {
	return &fakeI18nTypeRepo{types: map[string]domain.I18nTypeRecord{}}
}

func (f *fakeI18nTypeRepo) addType(record domain.I18nTypeRecord) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.types[record.ID] = record
}

func (f *fakeI18nTypeRepo) ListTypesPaged(_ context.Context, keyword string, isEnabled *int, pageIndex, pageSize int) ([]domain.I18nTypeRecord, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.I18nTypeRecord, 0, len(f.types))
	for _, record := range f.types {
		if keyword != "" && !strings.Contains(record.Code, keyword) && !strings.Contains(record.Name, keyword) {
			continue
		}
		if isEnabled != nil && record.IsEnabled != *isEnabled {
			continue
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].Code < out[j].Code
	})
	total := int64(len(out))
	return paginate(out, pageIndex, pageSize), total, nil
}

func (f *fakeI18nTypeRepo) ListEnabledTypes(context.Context) ([]domain.I18nTypeRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.I18nTypeRecord, 0, len(f.types))
	for _, record := range f.types {
		if record.IsEnabled == 1 {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].Code < out[j].Code
	})
	return out, nil
}

func (f *fakeI18nTypeRepo) FindTypeByID(_ context.Context, id string) (*domain.I18nTypeRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.types[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return &record, nil
}

func (f *fakeI18nTypeRepo) CountTypesByCode(_ context.Context, code string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var count int64
	for _, record := range f.types {
		if record.Code == code {
			count++
		}
	}
	return count, nil
}

func (f *fakeI18nTypeRepo) InsertType(_ context.Context, record domain.I18nTypeRecord) error {
	f.addType(record)
	return nil
}

func (f *fakeI18nTypeRepo) UpdateType(_ context.Context, record domain.I18nTypeRecord) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.types[record.ID]
	if !ok || existing.Version != record.Version {
		return 0, nil
	}
	f.types[record.ID] = record
	return 1, nil
}

func (f *fakeI18nTypeRepo) SoftDeleteType(_ context.Context, id string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.types, id)
	return 1, nil
}

// fakeFileRepo 实现 FileRepo。
type fakeFileRepo struct {
	mu    sync.Mutex
	files map[string]domain.FileRecord
}

func newFakeFileRepo() *fakeFileRepo {
	return &fakeFileRepo{files: map[string]domain.FileRecord{}}
}

func (f *fakeFileRepo) ListFiles(_ context.Context, fileType string, pageIndex, pageSize int) ([]domain.FileRecord, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.FileRecord, 0, len(f.files))
	for _, record := range f.files {
		if strings.TrimSpace(fileType) != "" && record.FileType != fileType {
			continue
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	total := int64(len(out))
	return paginate(out, pageIndex, pageSize), total, nil
}

func (f *fakeFileRepo) FindFileByID(_ context.Context, id string) (*domain.FileRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.files[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return &record, nil
}

func (f *fakeFileRepo) InsertFile(_ context.Context, record domain.FileRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[record.ID] = record
	return nil
}

func (f *fakeFileRepo) SoftDeleteFile(_ context.Context, id string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, id)
	return 1, nil
}

// paginate 按 1 基页码切片。
func paginate[T any](list []T, pageIndex, pageSize int) []T {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize < 0 {
		pageSize = 0
	}
	from := (pageIndex - 1) * pageSize
	if from > len(list) {
		from = len(list)
	}
	to := from + pageSize
	if to > len(list) {
		to = len(list)
	}
	return list[from:to]
}

// fixedClock 提供可注入的固定时间。
func fixedClock(value time.Time) func() time.Time {
	return func() time.Time { return value }
}
