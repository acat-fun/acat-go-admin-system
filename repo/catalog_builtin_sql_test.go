package repo

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

// 本文件覆盖页面/字典/语言类型的内置标记与字典项颜色在 SQL 层的一致性：
// 查询列、写入参数、删除语句的内置行兜底。

func TestPageSelectColumnsCarryBuiltinFields(t *testing.T) {
	repo, mock, cleanup := newPageMock(t)
	defer cleanup()

	created := time.Date(2026, 8, 1, 9, 0, 57, 0, time.UTC)
	updated := time.Date(2026, 8, 1, 9, 1, 57, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + pagePlainColumns + " FROM t_acat_page WHERE id = ? AND is_deleted = 0")).
		WithArgs("p1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "name", "type", "path", "icon", "parent_id",
			"sort_order", "scope", "is_enabled", "frontend_module_code", "route_key",
			"permission_code", "is_builtin", "description", "created_at", "updated_at", "version"}).
			AddRow("p1", "page-servers", "服务器", 2, "/system/servers", nil, nil,
				1, 0, 1, nil, nil, "devops:server:view", 1, "服务器目录", created, updated, 0))

	record, err := repo.FindPageByID(context.Background(), "p1")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if record.PermissionCode != "devops:server:view" || record.IsBuiltin != 1 || record.Description != "服务器目录" {
		t.Fatalf("新列映射异常: %+v", record)
	}
}

func TestInsertPageCarriesBuiltinFields(t *testing.T) {
	repo, mock, cleanup := newPageMock(t)
	defer cleanup()

	now := time.Now()
	mock.ExpectExec("INSERT INTO t_acat_page").
		WithArgs("p1", "acat:admin:system:dicts", "字典管理", 2, "/admin/system/dicts", nil, nil,
			1, 0, 1, nil, nil, "acat:admin:system:dicts:view", 1, "字典页",
			"0", "0", now, now).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.InsertPage(context.Background(), domain.PageRecord{
		ID: "p1", Code: "acat:admin:system:dicts", Name: "字典管理", Type: 2,
		Path: strPtr("/admin/system/dicts"), SortOrder: 1, Scope: 0, IsEnabled: 1,
		PermissionCode: "acat:admin:system:dicts:view", IsBuiltin: 1, Description: "字典页",
		CreateBy: strPtr("0"), UpdateBy: strPtr("0"), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
}

func TestUpdatePageCarriesBuiltinFields(t *testing.T) {
	repo, mock, cleanup := newPageMock(t)
	defer cleanup()

	now := time.Now()
	mock.ExpectExec("UPDATE t_acat_page").
		WithArgs("acat:admin:system:dicts", "字典管理", 2, "/admin/system/dicts", nil, nil,
			1, 0, 1, nil, nil, "acat:admin:system:dicts:view", 1, "字典页", now, "0", now, "p1", 3).
		WillReturnResult(sqlmock.NewResult(0, 1))

	affected, err := repo.UpdatePage(context.Background(), domain.PageRecord{
		ID: "p1", Code: "acat:admin:system:dicts", Name: "字典管理", Type: 2,
		Path: strPtr("/admin/system/dicts"), SortOrder: 1, Scope: 0, IsEnabled: 1,
		PermissionCode: "acat:admin:system:dicts:view", IsBuiltin: 1, Description: "字典页",
		CreatedAt: now, UpdateBy: strPtr("0"), UpdatedAt: now, Version: 3,
	})
	if err != nil || affected != 1 {
		t.Fatalf("更新失败: affected=%d err=%v", affected, err)
	}
}

func TestRestoreDeletedPageCarriesBuiltinFields(t *testing.T) {
	repo, mock, cleanup := newPageMock(t)
	defer cleanup()

	now := time.Now()
	mock.ExpectExec("UPDATE t_acat_page\\s+SET is_deleted = 0").
		WithArgs("acat:admin:system:dicts", "字典管理", 2, "/admin/system/dicts", nil, nil,
			1, 0, 1, nil, nil, "acat:admin:system:dicts:view", 1, "字典页",
			sqlmock.AnyArg(), "p1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	affected, err := repo.RestoreDeletedPage(context.Background(), domain.PageRecord{
		ID: "p1", Code: "acat:admin:system:dicts", Name: "字典管理", Type: 2,
		Path: strPtr("/admin/system/dicts"), SortOrder: 1, Scope: 0, IsEnabled: 1,
		PermissionCode: "acat:admin:system:dicts:view", IsBuiltin: 1, Description: "字典页",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || affected != 1 {
		t.Fatalf("恢复失败: affected=%d err=%v", affected, err)
	}
}

func TestSoftDeletePageExcludesBuiltin(t *testing.T) {
	repo, mock, cleanup := newPageMock(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta("UPDATE t_acat_page SET is_deleted = 1, updated_at = ? WHERE id = ? AND is_deleted = 0 AND is_builtin = 0")).
		WithArgs(sqlmock.AnyArg(), "p1").
		WillReturnResult(sqlmock.NewResult(0, 0))

	affected, err := repo.SoftDeletePage(context.Background(), "p1")
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if affected != 0 {
		t.Fatalf("内置行不应被删除，影响行数 = %d", affected)
	}
}

func TestDictDataColumnsCarryBuiltinAndColor(t *testing.T) {
	repo, mock, cleanup := newDictMock(t)
	defer cleanup()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + dictDataColumns + " FROM t_acat_dict_data WHERE dict_id = ? AND is_deleted = 0 ORDER BY sort_order ASC, id ASC")).
		WithArgs("d1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "dict_id", "parent_id", "code", "name", "value",
			"age_level", "sort_order", "is_enabled", "description", "is_builtin", "color",
			"created_at", "updated_at", "version"}).
			AddRow("i1", "d1", nil, "c1", "启用", "enabled", nil, 1, 1, nil, 1, "green", time.Now(), time.Now(), 0))

	records, err := repo.ListAllDataItems(context.Background(), "d1", nil)
	if err != nil || len(records) != 1 {
		t.Fatalf("查询失败: %+v %v", records, err)
	}
	if records[0].IsBuiltin != 1 || records[0].Color != "green" {
		t.Fatalf("新列映射异常: %+v", records[0])
	}
}

func TestDictDataWriteStatementsCarryBuiltinAndColor(t *testing.T) {
	repo, mock, cleanup := newDictMock(t)
	defer cleanup()

	now := time.Now()
	mock.ExpectExec("INSERT INTO t_acat_dict_data").
		WithArgs("i1", "d1", nil, "c1", "启用", "enabled", 8, 1, 1, nil, 1, "green",
			"0", "0", now, now).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE t_acat_dict_data").
		WithArgs("d1", nil, "c1", "启用", "enabled", 1, 1, 8, nil, 1, "green", now, "0", now, "i1", 2).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE t_acat_dict_data").
		WithArgs("d1", nil, "c1", "启用", "enabled", 1, 1, 8, nil, 1, "green", now, "0", now, "i1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	record := domain.DictDataRecord{
		ID: "i1", DictID: "d1", Code: "c1", Name: "启用", Value: "enabled", AgeLevel: 8,
		SortOrder: 1, IsEnabled: 1, IsBuiltin: 1, Color: "green",
		CreateBy: strPtr("0"), UpdateBy: strPtr("0"), CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.InsertDataItem(context.Background(), record); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	record.Version = 2
	if _, err := repo.UpdateDataItem(context.Background(), record); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if _, err := repo.UpdateDataItemByID(context.Background(), record); err != nil {
		t.Fatalf("按主键更新失败: %v", err)
	}
}

func TestSoftDeleteDataItemExcludesBuiltin(t *testing.T) {
	repo, mock, cleanup := newDictMock(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta("UPDATE t_acat_dict_data SET is_deleted = 1, updated_at = ? WHERE id = ? AND is_deleted = 0 AND is_builtin = 0")).
		WithArgs(sqlmock.AnyArg(), "i1").
		WillReturnResult(sqlmock.NewResult(0, 0))

	if _, err := repo.SoftDeleteDataItem(context.Background(), "i1"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
}

func TestI18nTypeWriteStatementsCarryBuiltin(t *testing.T) {
	repo, mock, cleanup := newTypeMock(t)
	defer cleanup()

	now := time.Now()
	mock.ExpectExec("INSERT INTO t_acat_i18n_type").
		WithArgs("t1", "zh-CN", "中文", 1, 1, 1, now, now).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE t_acat_i18n_type").
		WithArgs("zh-CN", "中文", 1, 1, 1, now, now, "t1", 3).
		WillReturnResult(sqlmock.NewResult(0, 1))

	record := domain.I18nTypeRecord{
		ID: "t1", Code: "zh-CN", Name: "中文", SortOrder: 1, IsEnabled: 1, IsBuiltin: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.InsertType(context.Background(), record); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	record.Version = 3
	if _, err := repo.UpdateType(context.Background(), record); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
}

func TestSoftDeleteTypeExcludesBuiltin(t *testing.T) {
	repo, mock, cleanup := newTypeMock(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta("UPDATE t_acat_i18n_type SET is_deleted = 1, updated_at = ? WHERE id = ? AND is_deleted = 0 AND is_builtin = 0")).
		WithArgs(sqlmock.AnyArg(), "t1").
		WillReturnResult(sqlmock.NewResult(0, 0))

	if _, err := repo.SoftDeleteType(context.Background(), "t1"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
}
