package repo

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"github.com/acat-fun/acat-go-common/mysqlx"
)

func newDictMock(t *testing.T) (*MySQLDictRepo, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	cleanup := func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("SQL 预期未满足: %v", err)
		}
		_ = db.Close()
	}
	return NewDictRepo(db), mock, cleanup
}

func TestFindDictByIDUsesSoftDeleteFilter(t *testing.T) {
	repo, mock, cleanup := newDictMock(t)
	defer cleanup()

	query := regexp.QuoteMeta("SELECT " + dictColumns + " FROM t_acat_dict WHERE id = ? AND is_deleted = 0")
	mock.ExpectQuery(query).WithArgs("d1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "name", "is_enabled", "is_tree", "scope",
			"description", "is_builtin", "created_at", "updated_at", "version"}).
			AddRow("d1", "book_tag", "书籍标签", 1, 0, 1, nil, 0, time.Now(), time.Now(), 0))

	record, err := repo.FindDictByID(context.Background(), "d1")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if record.Code != "book_tag" || record.Description != nil || record.IsBuiltin != 0 {
		t.Fatalf("结果异常: %+v", record)
	}
}

func TestFindDictByIDNotFound(t *testing.T) {
	repo, mock, cleanup := newDictMock(t)
	defer cleanup()

	mock.ExpectQuery("FROM t_acat_dict WHERE id = \\? AND is_deleted = 0").
		WithArgs("missing").WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, err := repo.FindDictByID(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("应返回 ErrNotFound，实际 %v", err)
	}
}

func TestFindDictByCodeRejectsMultipleRows(t *testing.T) {
	repo, mock, cleanup := newDictMock(t)
	defer cleanup()

	mock.ExpectQuery("FROM t_acat_dict WHERE code = \\? AND is_deleted = 0 LIMIT 2").
		WithArgs("dup").
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "name", "is_enabled", "is_tree", "scope",
			"description", "is_builtin", "created_at", "updated_at", "version"}).
			AddRow("d1", "dup", "一", 1, 0, 0, nil, 0, time.Now(), time.Now(), 0).
			AddRow("d2", "dup", "二", 1, 0, 1, nil, 0, time.Now(), time.Now(), 0))

	_, err := repo.FindDictByCode(context.Background(), "dup")
	if !errors.Is(err, ErrMultipleResults) {
		t.Fatalf("应返回 ErrMultipleResults，实际 %v", err)
	}
}

func TestListDictsBuildsFiltersAndOrder(t *testing.T) {
	repo, mock, cleanup := newDictMock(t)
	defer cleanup()

	enabled := 1
	scope := 0
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM t_acat_dict WHERE is_deleted = 0 AND name LIKE ? AND is_enabled = ? AND scope = ?")).
		WithArgs("%标签%", 1, 0).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT "+dictColumns+" FROM t_acat_dict WHERE is_deleted = 0 AND name LIKE ? AND is_enabled = ? AND scope = ? ORDER BY created_at DESC LIMIT ? OFFSET ?")).
		WithArgs("%标签%", 1, 0, 10, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "name", "is_enabled", "is_tree", "scope",
			"description", "is_builtin", "created_at", "updated_at", "version"}).
			AddRow("d1", "book_tag", "书籍标签", 1, 0, 0, nil, 0, time.Now(), time.Now(), 0))

	records, total, err := repo.ListDicts(context.Background(),
		domain.DictFilter{Name: "标签", IsEnabled: &enabled, Scope: &scope}, 1, 10)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 1 || len(records) != 1 {
		t.Fatalf("结果异常: total=%d records=%v", total, records)
	}
}

func TestListDictsOffsetForSecondPage(t *testing.T) {
	repo, mock, cleanup := newDictMock(t)
	defer cleanup()

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM t_acat_dict").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("FROM t_acat_dict WHERE is_deleted = 0 ORDER BY created_at DESC LIMIT \\? OFFSET \\?").
		WithArgs(5, 5).
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "name", "is_enabled", "is_tree", "scope",
			"description", "is_builtin", "created_at", "updated_at", "version"}))

	if _, _, err := repo.ListDicts(context.Background(), domain.DictFilter{}, 2, 5); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
}

func TestInsertDictWritesAuditColumns(t *testing.T) {
	repo, mock, cleanup := newDictMock(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO t_acat_dict")).
		WithArgs("d1", "book_tag", "书籍标签", 1, 0, 1, nil, 1, "0", "0", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := repo.InsertDict(context.Background(), domain.DictRecord{
		ID: "d1", Code: "book_tag", Name: "书籍标签", IsEnabled: 1, IsTree: 0, Scope: 1, IsBuiltin: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(), CreateBy: strPtr("0"), UpdateBy: strPtr("0"),
	})
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
}

func TestUpdateDictUsesOptimisticLock(t *testing.T) {
	repo, mock, cleanup := newDictMock(t)
	defer cleanup()

	mock.ExpectExec("UPDATE t_acat_dict\\s+SET .*version = version \\+ 1\\s+WHERE id = \\? AND version = \\? AND is_deleted = 0").
		WillReturnResult(sqlmock.NewResult(0, 1))

	affected, err := repo.UpdateDict(context.Background(), domain.DictRecord{
		ID: "d1", Code: "book_tag", Name: "书籍标签", IsEnabled: 1, IsTree: 0, Scope: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(), Version: 3,
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if affected != 1 {
		t.Fatalf("影响行数 = %d", affected)
	}
}

func TestSoftDeleteDictSetsFlag(t *testing.T) {
	repo, mock, cleanup := newDictMock(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta("UPDATE t_acat_dict SET is_deleted = 1, updated_at = ? WHERE id = ? AND is_deleted = 0 AND is_builtin = 0")).
		WithArgs(sqlmock.AnyArg(), "d1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	affected, err := repo.SoftDeleteDict(context.Background(), "d1")
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if affected == 0 {
		t.Fatalf("删除应影响至少一行: %d", affected)
	}
}

func TestSaveNameLabelsUpsertIsIdempotent(t *testing.T) {
	repo, mock, cleanup := newDictMock(t)
	defer cleanup()

	for _, value := range []string{"书籍标签", "Book Tag"} {
		mock.ExpectExec("INSERT INTO t_acat_i18n_label").
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), value, domain.I18nTableDict, "d1",
				sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
	}
	// 空白值应被跳过（不产生 SQL 预期）。
	if err := repo.SaveNameLabels(context.Background(), domain.I18nTableDict, "d1", []domain.I18nValue{
		{I18n: "zh-CN", Value: "书籍标签"},
		{I18n: "en", Value: "Book Tag"},
		{I18n: "ja", Value: "   "},
		{I18n: "", Value: "x"},
	}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
}

func TestListFrontendLabelsQueryShape(t *testing.T) {
	repo, mock, cleanup := newDictMock(t)
	defer cleanup()

	mock.ExpectQuery("INNER JOIN t_acat_i18n_label label").
		WithArgs("zh-CN", "zh-CN", "i18n_admin_label").
		WillReturnRows(sqlmock.NewRows([]string{"table_data_id", "label_value", "i18n_code", "source_table", "source_field"}).
			AddRow("acat.read.admin.x.y/z.w", "文案", "zh-CN", "acat_user.t_acat_dict_data", "name"))

	records, err := repo.ListFrontendLabels(context.Background(), "i18n_admin_label", "zh-CN")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(records) != 1 || records[0].Code != "acat.read.admin.x.y/z.w" || records[0].LabelValue != "文案" {
		t.Fatalf("结果异常: %+v", records)
	}
}

func newPageMock(t *testing.T) (*MySQLPageRepo, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	cleanup := func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("SQL 预期未满足: %v", err)
		}
		_ = db.Close()
	}
	return NewPageRepo(db), mock, cleanup
}

func TestPageListByScopeJoinsI18nLabel(t *testing.T) {
	repo, mock, cleanup := newPageMock(t)
	defer cleanup()

	created := time.Date(2026, 8, 1, 9, 0, 57, 0, time.UTC)
	updated := time.Date(2026, 8, 1, 9, 1, 57, 0, time.UTC)
	mock.ExpectQuery("COALESCE\\(label.label_value, uc.name\\)").
		WithArgs("zh-CN", 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "name", "type", "path", "icon", "parent_id",
			"sort_order", "scope", "is_enabled", "frontend_module_code", "route_key",
			"permission_code", "is_builtin", "description",
			"created_at", "updated_at", "version"}).
			AddRow("p1", "acat:admin:system", "系统管理", 0, "/admin/system", "SettingOutlined", nil,
				1, 0, 1, "system", nil, "", 0, "", created, updated, 0).
			AddRow("p2", "acat:admin:system:dicts", "字典管理", 2, "/admin/system/dicts", nil, "p1",
				2, 0, 1, "system", "system.config.dicts", "acat:admin:system:dicts:view", 1, "字典页", created, updated, 0))

	pages, err := repo.ListPagesByScope(context.Background(), 0, "zh-CN")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(pages) != 2 || pages[0].ParentID != nil || pages[1].ParentID == nil || *pages[1].ParentID != "p1" {
		t.Fatalf("结果异常: %+v", pages)
	}
	if pages[1].PermissionCode != "acat:admin:system:dicts:view" || pages[1].IsBuiltin != 1 ||
		pages[1].Description != "字典页" {
		t.Errorf("新列映射异常: %+v", pages[1])
	}
	// 审计列必须被 SELECT 并映射为 时间文本。
	if got := domain.FormatDateTime(pages[0].CreatedAt); got != "2026-08-01T09:00:57" {
		t.Errorf("createdAt = %q, 期望 2026-08-01T09:00:57", got)
	}
	if got := domain.FormatDateTime(pages[0].UpdatedAt); got != "2026-08-01T09:01:57" {
		t.Errorf("updatedAt = %q, 期望 2026-08-01T09:01:57", got)
	}
}

func TestRestoreDeletedPageOnlyMatchesSoftDeleted(t *testing.T) {
	repo, mock, cleanup := newPageMock(t)
	defer cleanup()

	mock.ExpectExec("UPDATE t_acat_page\\s+SET is_deleted = 0.*WHERE id = \\? AND is_deleted = 1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	affected, err := repo.RestoreDeletedPage(context.Background(), domain.PageRecord{
		ID: "p1", Code: "acat:admin:system:dicts", Name: "字典管理", Type: 2,
		Scope: 0, IsEnabled: 1, SortOrder: 1,
	})
	if err != nil || affected != 1 {
		t.Fatalf("恢复失败: affected=%d err=%v", affected, err)
	}
}

func TestGrantPageToRootAndAdminIsIdempotent(t *testing.T) {
	repo, mock, cleanup := newPageMock(t)
	defer cleanup()

	mock.ExpectExec("INSERT INTO t_acat_role_permission").
		WithArgs("p1").
		WillReturnResult(sqlmock.NewResult(1, 2))

	if err := repo.GrantPageToRootAndAdmin(context.Background(), "p1"); err != nil {
		t.Fatalf("授权失败: %v", err)
	}
}

func TestSelectPageIDsByPermissionsEmptyShortCircuits(t *testing.T) {
	repo, _, cleanup := newPageMock(t)
	defer cleanup()

	// 空权限列表不应触发 SQL。
	ids, err := repo.SelectPageIDsByPermissions(context.Background(), nil)
	if err != nil || len(ids) != 0 {
		t.Fatalf("结果异常: %v %v", ids, err)
	}
}

func TestSelectPageIDsByPermissionsUsesPlaceholders(t *testing.T) {
	repo, mock, cleanup := newPageMock(t)
	defer cleanup()

	mock.ExpectQuery("SELECT id FROM t_acat_page WHERE code IN \\(\\?, \\?\\) AND is_deleted = 0").
		WithArgs("a", "b").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("p1").AddRow("p2"))

	ids, err := repo.SelectPageIDsByPermissions(context.Background(), []string{"a", "b"})
	if err != nil || len(ids) != 2 {
		t.Fatalf("结果异常: %v %v", ids, err)
	}
}

func TestInsertPageHistoryUsesCompactID(t *testing.T) {
	repo, mock, cleanup := newPageMock(t)
	defer cleanup()

	now := time.Now()
	mock.ExpectExec("INSERT INTO t_acat_page_history").
		WithArgs(sqlmock.AnyArg(), "p1", "acat:admin:system", "系统管理", 2, "/admin/system", nil, nil,
			1, 0, 1, "system", "system.config", 0, nil, nil, now, now, 0).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.InsertPageHistory(context.Background(), domain.PageHistoryRecord{
		PageID: "p1", Code: "acat:admin:system", Name: "系统管理", Type: intPtr(2),
		Path: strPtr("/admin/system"), SortOrder: intPtr(1), Scope: intPtr(0), IsEnabled: intPtr(1),
		FrontendModuleCode: strPtr("system"), RouteKey: strPtr("system.config"),
		IsDeleted: intPtr(0), CreatedAt: &now, UpdatedAt: now, Version: intPtr(0),
	}); err != nil {
		t.Fatalf("写入历史失败: %v", err)
	}
}

// TestNewCompactID 验证历史表 id 为 32 位无连字符。
func TestNewCompactID(t *testing.T) {
	id := newCompactID()
	if len(id) != 32 {
		t.Fatalf("长度 = %d，期望 32（%s）", len(id), id)
	}
	if containsDash(id) {
		t.Fatalf("不应包含连字符: %s", id)
	}
}

func containsDash(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] == '-' {
			return true
		}
	}
	return false
}

func newModuleMock(t *testing.T) (*MySQLFrontendModuleRepo, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	cleanup := func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("SQL 预期未满足: %v", err)
		}
		_ = db.Close()
	}
	return NewFrontendModuleRepo(db), mock, cleanup
}

func TestFrontendModuleListOrder(t *testing.T) {
	repo, mock, cleanup := newModuleMock(t)
	defer cleanup()

	mock.ExpectQuery("FROM t_acat_frontend_module WHERE is_deleted = 0 ORDER BY sort_order ASC, module_code ASC, created_at DESC").
		WillReturnRows(sqlmock.NewRows([]string{"id", "module_code", "name", "release_version", "contract_version",
			"manifest_path", "fallback_version", "fallback_manifest_path", "status", "sort_order",
			"created_at", "updated_at", "version"}).
			AddRow("m1", "system", "系统管理", "1.0.0", 1, "/admin-remotes/system/1.0.0/mf-manifest.json",
				nil, nil, 1, 1, time.Now(), time.Now(), 0))

	records, err := repo.ListModules(context.Background(), "")
	if err != nil || len(records) != 1 {
		t.Fatalf("查询失败: %v %v", records, err)
	}
	if records[0].FallbackVersion != nil || records[0].ModuleCode != "system" {
		t.Fatalf("结果异常: %+v", records[0])
	}
}

func TestFrontendModuleListByCodeOrder(t *testing.T) {
	repo, mock, cleanup := newModuleMock(t)
	defer cleanup()

	mock.ExpectQuery("WHERE is_deleted = 0 AND module_code = \\? ORDER BY created_at DESC").
		WithArgs("content").
		WillReturnRows(sqlmock.NewRows([]string{"id", "module_code", "name", "release_version", "contract_version",
			"manifest_path", "fallback_version", "fallback_manifest_path", "status", "sort_order",
			"created_at", "updated_at", "version"}))

	if _, err := repo.ListModules(context.Background(), "content"); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
}

func TestFrontendModuleUpdateAffectedRows(t *testing.T) {
	repo, mock, cleanup := newModuleMock(t)
	defer cleanup()

	mock.ExpectExec("UPDATE t_acat_frontend_module").
		WillReturnResult(sqlmock.NewResult(0, 0))

	affected, err := repo.UpdateModule(context.Background(), domain.FrontendModuleRecord{
		ID: "m1", ModuleCode: "system", Name: "系统管理", ReleaseVersion: "1.0.0", ContractVersion: 1,
		ManifestPath: "/admin-remotes/system/1.0.0/mf-manifest.json", Status: 1, SortOrder: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(), Version: 7,
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if affected != 0 {
		t.Fatalf("0 行受影响应原样返回，实际 %d", affected)
	}
}

func TestDisableOtherEnabledVersionsSkipsVersion(t *testing.T) {
	repo, mock, cleanup := newModuleMock(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta("UPDATE t_acat_frontend_module SET status = ? WHERE module_code = ? AND status = ? AND id <> ? AND is_deleted = 0")).
		WithArgs(domain.FrontendModuleStatusDisabled, "content", domain.FrontendModuleStatusEnabled, "m2").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.DisableOtherEnabledVersions(context.Background(), "content", "m2"); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
}

func TestListEnabledModulesByCodesUsesWindowFunction(t *testing.T) {
	repo, mock, cleanup := newModuleMock(t)
	defer cleanup()

	mock.ExpectQuery("ROW_NUMBER\\(\\) OVER").
		WithArgs("system", "content").
		WillReturnRows(sqlmock.NewRows([]string{"module_code", "name", "release_version", "contract_version",
			"manifest_path", "fallback_version", "fallback_manifest_path", "status", "sort_order",
			"created_at", "updated_at", "version", "id"}).
			AddRow("system", "系统管理", "1.0.0", 1, "/admin-remotes/system/1.0.0/mf-manifest.json",
				nil, nil, 1, 1, time.Now(), time.Now(), 0, "m1"))

	records, err := repo.ListEnabledModulesByCodes(context.Background(), []string{"system", "content"})
	if err != nil || len(records) != 1 || records[0].ID != "m1" {
		t.Fatalf("查询失败: %+v %v", records, err)
	}
	// NULL fallback_manifest_path 必须保持 nil（VO 输出 JSON null）。
	if records[0].FallbackManifestPath != nil || records[0].FallbackVersion != nil {
		t.Errorf("NULL 回退字段应为 nil: %+v", records[0])
	}

	// 空列表不应触发查询。
	if list, err := repo.ListEnabledModulesByCodes(context.Background(), nil); err != nil || len(list) != 0 {
		t.Fatalf("空列表应短路: %v %v", list, err)
	}
}

func newTypeMock(t *testing.T) (*MySQLI18nTypeRepo, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	cleanup := func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("SQL 预期未满足: %v", err)
		}
		_ = db.Close()
	}
	return NewI18nTypeRepo(db), mock, cleanup
}

func TestI18nTypeListUsesKeywordAndEnabled(t *testing.T) {
	repo, mock, cleanup := newTypeMock(t)
	defer cleanup()

	enabled := 1
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM t_acat_i18n_type WHERE is_deleted = 0 AND \\(code LIKE \\? OR name LIKE \\?\\) AND is_enabled = \\?").
		WithArgs("%en%", "%en%", 1).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("FROM t_acat_i18n_type WHERE is_deleted = 0 AND \\(code LIKE \\? OR name LIKE \\?\\) AND is_enabled = \\? ORDER BY sort_order ASC, code ASC LIMIT \\? OFFSET \\?").
		WithArgs("%en%", "%en%", 1, 10, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "name", "sort_order", "is_enabled",
			"is_builtin", "created_at", "updated_at", "version"}).
			AddRow("t1", "en", "English", 2, 1, 0, time.Now(), time.Now(), 0))

	records, total, err := repo.ListTypesPaged(context.Background(), "en", &enabled, 1, 10)
	if err != nil || total != 1 || len(records) != 1 {
		t.Fatalf("查询失败: %+v %d %v", records, total, err)
	}
	if records[0].IsBuiltin != 0 {
		t.Fatalf("内置标记异常: %+v", records[0])
	}
}

func TestI18nTypeListEnabledForcesFlag(t *testing.T) {
	repo, mock, cleanup := newTypeMock(t)
	defer cleanup()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + i18nTypeColumns + " FROM t_acat_i18n_type WHERE is_deleted = 0 AND is_enabled = 1 ORDER BY sort_order ASC, code ASC")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "name", "sort_order", "is_enabled",
			"is_builtin", "created_at", "updated_at", "version"}).
			AddRow("t1", "zh-CN", "中文", 1, 1, 0, time.Now(), time.Now(), 0))

	if _, err := repo.ListEnabledTypes(context.Background()); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
}

func TestI18nTypeUniqueCodeCheck(t *testing.T) {
	repo, mock, cleanup := newTypeMock(t)
	defer cleanup()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM t_acat_i18n_type WHERE is_deleted = 0 AND code = ?")).
		WithArgs("en").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	count, err := repo.CountTypesByCode(context.Background(), "en")
	if err != nil || count != 1 {
		t.Fatalf("计数异常: %d %v", count, err)
	}
}

func newFileMock(t *testing.T) (*MySQLFileRepo, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	cleanup := func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("SQL 预期未满足: %v", err)
		}
		_ = db.Close()
	}
	return NewFileRepo(db), mock, cleanup
}

func TestFileListFilterAndOrder(t *testing.T) {
	repo, mock, cleanup := newFileMock(t)
	defer cleanup()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM t_acat_file WHERE is_deleted = 0 AND file_type = ?")).
		WithArgs("avatar").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT "+fileColumns+" FROM t_acat_file WHERE is_deleted = 0 AND file_type = ? ORDER BY created_at DESC LIMIT ? OFFSET ?")).
		WithArgs("avatar", 10, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "type", "file_type", "size",
			"created_at", "updated_at"}).
			AddRow("f1", "a.png", "acat-local/acat-fun/read/user/avatar/x.png", "image/png", "avatar", 10,
				time.Now(), time.Now()))

	records, total, err := repo.ListFiles(context.Background(), "avatar", 1, 10)
	if err != nil || total != 1 || len(records) != 1 {
		t.Fatalf("查询失败: %+v %d %v", records, total, err)
	}
}

func TestFileInsertAndSoftDelete(t *testing.T) {
	repo, mock, cleanup := newFileMock(t)
	defer cleanup()

	mock.ExpectExec("INSERT INTO t_acat_file").
		WithArgs("f1", "a.png", "acat-local/acat-fun/read/file/x.png", "image/png", "other", int64(3),
			sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE t_acat_file SET is_deleted = 1, updated_at = ? WHERE id = ? AND is_deleted = 0")).
		WithArgs(sqlmock.AnyArg(), "f1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.InsertFile(context.Background(), domain.FileRecord{
		ID: "f1", Name: "a.png", Path: "acat-local/acat-fun/read/file/x.png", Type: "image/png",
		FileType: "other", Size: 3, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	affected, err := repo.SoftDeleteFile(context.Background(), "f1")
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if affected == 0 {
		t.Fatalf("删除应影响至少一行: %d", affected)
	}
}

func TestOffsetAndPlaceholders(t *testing.T) {
	if offset(1, 10) != 0 || offset(3, 10) != 20 || offset(0, 10) != 0 {
		t.Fatalf("offset 计算异常: %d %d %d", offset(1, 10), offset(3, 10), offset(0, 10))
	}
	if mysqlx.Placeholders(3) != "?, ?, ?" {
		t.Fatalf("占位符异常: %q", mysqlx.Placeholders(3))
	}
	if mysqlx.Placeholders(0) != "" {
		t.Fatalf("空占位符异常: %q", mysqlx.Placeholders(0))
	}
}

func strPtr(value string) *string { return &value }
func intPtr(value int) *int       { return &value }
