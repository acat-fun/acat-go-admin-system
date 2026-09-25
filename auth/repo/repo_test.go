package repo

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"47.108.230.93/acat-fun/acat-go-admin-system/auth/domain"
)

func newMock(t *testing.T) (*MySQLWorkerRepo, *MySQLPageRepo, *MySQLFrontendModuleRepo, sqlmock.Sqlmock, func()) {
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
	return NewWorkerRepo(db), NewPageRepo(db), NewFrontendModuleRepo(db), mock, cleanup
}

func TestFindByUsernameSQL(t *testing.T) {
	workers, _, _, mock, cleanup := newMock(t)
	defer cleanup()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, username, password, email, avatar, status FROM t_acat_user_worker WHERE username = ? AND is_deleted = 0")).
		WithArgs("admin").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password", "email", "avatar", "status"}).
			AddRow("0", "admin", "$2a$10$hash", "admin@acat.fun", nil, 1))

	worker, err := workers.FindByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if worker.ID != "0" || worker.Username != "admin" || worker.Status != 1 {
		t.Errorf("结果 = %+v", worker)
	}
	if worker.Avatar != "" {
		t.Errorf("NULL avatar 应映射为空串，实际 %q", worker.Avatar)
	}
}

func TestFindByUsernameNotFound(t *testing.T) {
	workers, _, _, mock, cleanup := newMock(t)
	defer cleanup()

	mock.ExpectQuery("SELECT id, username, password, email, avatar, status FROM t_acat_user_worker").
		WithArgs("ghost").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password", "email", "avatar", "status"}))

	_, err := workers.FindByUsername(context.Background(), "ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("应返回 ErrNotFound，实际 %v", err)
	}
}

func TestRoleCodesExcludesDisabledRoles(t *testing.T) {
	workers, _, _, mock, cleanup := newMock(t)
	defer cleanup()

	query := regexp.QuoteMeta(`
		SELECT r.code
		FROM t_acat_role r
		INNER JOIN t_acat_user_role ur ON r.id = ur.role_id
		WHERE ur.user_id = ?
		  AND ur.is_deleted = 0
		  AND r.is_deleted = 0
		  AND r.status = 1`)
	mock.ExpectQuery(query).WithArgs("0").
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("root").AddRow("admin"))

	roles, err := workers.RoleCodes(context.Background(), "0")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(roles) != 2 || roles[0] != "root" || roles[1] != "admin" {
		t.Errorf("角色 = %v", roles)
	}
}

func TestPermissionsQueryCoversPagesAndButtons(t *testing.T) {
	workers, _, _, mock, cleanup := newMock(t)
	defer cleanup()

	mock.ExpectQuery("SELECT code FROM \\(").WithArgs("u1", "u1").
		WillReturnRows(sqlmock.NewRows([]string{"code"}).
			AddRow("acat:admin:system:dicts").
			AddRow("acat:admin:system:dicts:create"))

	codes, err := workers.URLAndButtonCodes(context.Background(), "u1")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(codes) != 2 {
		t.Errorf("权限码 = %v", codes)
	}
}

func TestRootAllPermissionCodes(t *testing.T) {
	workers, _, _, mock, cleanup := newMock(t)
	defer cleanup()

	mock.ExpectQuery("SELECT code FROM t_acat_page WHERE is_deleted = 0 AND is_enabled = 1").
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("acat:admin:system"))

	codes, err := workers.AllPermissionCodes(context.Background())
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(codes) != 1 || codes[0] != "acat:admin:system" {
		t.Errorf("权限码 = %v", codes)
	}
}

func TestPageListByScopeUsesI18nFallback(t *testing.T) {
	_, pages, _, mock, cleanup := newMock(t)
	defer cleanup()

	mock.ExpectQuery("COALESCE\\(label.label_value, uc.name\\)").
		WithArgs("zh-CN", 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "code", "name", "type", "path", "icon", "parent_id",
			"sort_order", "scope", "is_enabled", "frontend_module_code", "route_key",
			"created_at", "updated_at",
		}).
			AddRow("p1", "acat:admin:system", "系统管理", 0, "/admin/system", "SettingOutlined", nil, 1, 0, 1, "system", nil,
				time.Date(2026, 8, 1, 9, 0, 57, 0, time.UTC), time.Date(2026, 8, 1, 9, 1, 57, 0, time.UTC)).
			AddRow("p2", "acat:admin:system:dicts", "字典管理", 2, "/admin/system/dicts", nil, "p1", 2, 0, 1, "system", "system.config.dicts",
				time.Date(2026, 8, 1, 9, 2, 57, 0, time.UTC), time.Date(2026, 8, 1, 9, 3, 57, 0, time.UTC)))

	list, err := pages.ListByScope(context.Background(), 0, "zh-CN")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("页面数 = %d", len(list))
	}
	if list[0].ParentID != "" || list[0].Icon != "SettingOutlined" {
		t.Errorf("首行字段异常: %+v", list[0])
	}
	if list[1].ParentID != "p1" || list[1].RouteKey != "system.config.dicts" {
		t.Errorf("次行字段异常: %+v", list[1])
	}
	// 审计列必须被 SELECT 并映射。
	if got := domain.FormatDateTime(list[0].CreatedAt); got != "2026-08-01T09:00:57" {
		t.Errorf("createdAt = %q, 期望 2026-08-01T09:00:57", got)
	}
	if got := domain.FormatDateTime(list[0].UpdatedAt); got != "2026-08-01T09:01:57" {
		t.Errorf("updatedAt = %q, 期望 2026-08-01T09:01:57", got)
	}
	if list[1].CreatedAt.IsZero() || list[1].UpdatedAt.IsZero() {
		t.Errorf("次行时间未映射: %+v", list[1])
	}
}

func TestFrontendModulesSkipEmptyCodes(t *testing.T) {
	_, _, modules, _, cleanup := newMock(t)
	defer cleanup()

	// 空 code 列表不应触发查询。
	list, err := modules.ListEnabledByCodes(context.Background(), nil)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("结果 = %v", list)
	}
}

func TestFrontendModulesQueryRows(t *testing.T) {
	_, _, modules, mock, cleanup := newMock(t)
	defer cleanup()

	mock.ExpectQuery("ROW_NUMBER\\(\\) OVER").
		WithArgs("system", "content").
		WillReturnRows(sqlmock.NewRows([]string{
			"module_code", "name", "release_version", "contract_version", "manifest_path",
			"fallback_version", "fallback_manifest_path", "status", "sort_order",
		}).
			AddRow("system", "系统管理", "1.0.0", 1, "/modules/system/manifest.json", nil, nil, 1, 0).
			AddRow("content", "内容运营", "1.0.0", 1, "/modules/content/manifest.json", "0.9.0", "/modules/content/manifest-0.9.0.json", 1, 1))

	list, err := modules.ListEnabledByCodes(context.Background(), []string{"system", "content"})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("模块数 = %d", len(list))
	}
	if list[0].ManifestPath != "/modules/system/manifest.json" || list[0].FallbackVersion != "" {
		t.Errorf("模块字段异常: %+v", list[0])
	}
	// fallback_manifest_path 为 NULL → nil（描述符输出 JSON null，不能是 ""）。
	if list[0].FallbackManifestPath != nil {
		t.Errorf("NULL fallback_manifest_path 应为 nil，实际 %q", *list[0].FallbackManifestPath)
	}
	if list[1].FallbackManifestPath == nil || *list[1].FallbackManifestPath != "/modules/content/manifest-0.9.0.json" {
		t.Errorf("回退 manifest 异常: %+v", list[1])
	}
}
