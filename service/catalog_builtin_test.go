package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/acat-fun/acat-go-admin-system/domain"
	"github.com/acat-fun/acat-go-common/result"
)

// 本文件覆盖内置标记（isBuiltin）与字典项颜色（color）、页面权限码（permissionCode）
// 在读写路径上的传递，以及删除内置行时的业务失败。

func assertJSONContains(t *testing.T, value any, wants ...string) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	for _, want := range wants {
		if !strings.Contains(string(raw), want) {
			t.Errorf("JSON 缺少 %s: %s", want, raw)
		}
	}
}

func TestListPagesReturnsBuiltinFields(t *testing.T) {
	env := newTestEnv(t)
	addPage(env, domain.PageRecord{
		ID: "p1", Code: "page-servers", Name: "服务器", Type: domain.PageTypePage,
		Scope: 0, IsEnabled: 1, IsBuiltin: 1, SortOrder: 1,
		PermissionCode: "devops:server:view", Description: "服务器目录",
	})

	tree, err := env.svc.ListPages(env.ctx, rootRC())
	if err != nil || len(tree) != 1 {
		t.Fatalf("查询失败: %+v %v", tree, err)
	}
	node := tree[0]
	if node.IsBuiltin == nil || *node.IsBuiltin != 1 ||
		node.PermissionCode != "devops:server:view" || node.Description != "服务器目录" {
		t.Fatalf("新字段缺失: %+v", node)
	}
	assertJSONContains(t, node, `"isBuiltin":1`, `"permissionCode":"devops:server:view"`, `"description":"服务器目录"`)
}

func TestCreatePageCarriesBuiltinFields(t *testing.T) {
	env := newTestEnv(t)
	entity, err := env.svc.CreatePage(env.ctx, rootRC(), domain.PageSavePayload{
		Name: strPtr("服务器"), Path: strPtr("/admin/system/servers"),
		PermissionCode: strPtr("devops:server:view"), IsBuiltin: intPtr(1), Description: strPtr("服务器目录"),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if entity.IsBuiltin == nil || *entity.IsBuiltin != 1 ||
		entity.PermissionCode != "devops:server:view" || entity.Description != "服务器目录" {
		t.Fatalf("新字段缺失: %+v", entity)
	}
	stored, err := env.pages.FindPageByID(env.ctx, entity.ID)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if stored.IsBuiltin != 1 || stored.PermissionCode != "devops:server:view" || stored.Description != "服务器目录" {
		t.Fatalf("落库值异常: %+v", stored)
	}
}

func TestUpdatePageCarriesBuiltinFields(t *testing.T) {
	env := newTestEnv(t)
	addPage(env, domain.PageRecord{
		ID: "p1", Code: "acat:admin:system:dicts", Name: "字典管理", Type: domain.PageTypePage,
		Scope: 0, IsEnabled: 1, Version: 0,
	})

	entity, err := env.svc.UpdatePage(env.ctx, rootRC(), "p1", domain.PageSavePayload{
		PermissionCode: strPtr("devops:server:edit"), IsBuiltin: intPtr(1), Description: strPtr("说明"),
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if entity.IsBuiltin == nil || *entity.IsBuiltin != 1 ||
		entity.PermissionCode != "devops:server:edit" || entity.Description != "说明" {
		t.Fatalf("新字段未更新: %+v", entity)
	}
	stored, err := env.pages.FindPageByID(env.ctx, "p1")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if stored.IsBuiltin != 1 || stored.PermissionCode != "devops:server:edit" || stored.Description != "说明" {
		t.Fatalf("落库值异常: %+v", stored)
	}
}

func TestDeleteBuiltinPageRejected(t *testing.T) {
	env := newTestEnv(t)
	addPage(env, domain.PageRecord{
		ID: "p1", Code: "page-servers", Name: "服务器", Type: domain.PageTypeNav,
		Scope: 0, IsEnabled: 1, IsBuiltin: 1,
	})

	err := env.svc.DeletePage(env.ctx, rootRC(), "p1")
	assertBusiness(t, err, 1, MessagePageBuiltinUndeletable)
	if _, err := env.pages.FindPageByID(env.ctx, "p1"); err != nil {
		t.Fatal("内置页面应保留")
	}
}

func TestDeletePageRejectedWhenChildBuiltin(t *testing.T) {
	env := newTestEnv(t)
	addPage(env, domain.PageRecord{
		ID: "nav", Code: "nav.code", Name: "导航", Type: domain.PageTypeNav, Scope: 0, IsEnabled: 1,
	})
	addPage(env, domain.PageRecord{
		ID: "child", Code: "child.code", Name: "子页", Type: domain.PageTypePage, ParentID: strPtr("nav"),
		Scope: 0, IsEnabled: 1, IsBuiltin: 1,
	})

	err := env.svc.DeletePage(env.ctx, rootRC(), "nav")
	assertBusiness(t, err, 1, MessagePageBuiltinUndeletable)
	if _, err := env.pages.FindPageByID(env.ctx, "nav"); err != nil {
		t.Fatal("含内置子页的父页面应保留")
	}
}

func TestListDictsAndDataItemsReturnBuiltinAndColor(t *testing.T) {
	env := newTestEnv(t)
	record := addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)
	record.IsBuiltin = 1
	env.dicts.addDict(record)
	env.dicts.addData(domain.DictDataRecord{
		ID: "i1", DictID: "d1", Code: "enabled", Name: "启用", Value: "enabled",
		IsEnabled: 1, IsBuiltin: 1, Color: "green", SortOrder: 1,
		CreatedAt: fixedNow, UpdatedAt: fixedNow,
	})

	page, err := env.svc.ListDicts(env.ctx, rootRC(), domain.DictFilter{}, 1, 10)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	dicts := page.(result.PageData[domain.DictVO])
	if len(dicts.List) != 1 || dicts.List[0].IsBuiltin == nil || *dicts.List[0].IsBuiltin != 1 {
		t.Fatalf("字典内置标记缺失: %+v", dicts.List)
	}
	assertJSONContains(t, dicts.List[0], `"isBuiltin":1`)

	items, err := env.svc.ListAllDataItems(env.ctx, rootRC(), "book_tag")
	if err != nil || len(items) != 1 {
		t.Fatalf("数据项查询失败: %+v %v", items, err)
	}
	if items[0].IsBuiltin == nil || *items[0].IsBuiltin != 1 || items[0].Color != "green" {
		t.Fatalf("数据项新字段缺失: %+v", items[0])
	}
	assertJSONContains(t, items[0], `"isBuiltin":1`, `"color":"green"`)
}

func TestCreateAndUpdateDictCarryBuiltin(t *testing.T) {
	env := newTestEnv(t)
	entity, err := env.svc.CreateDict(env.ctx, rootRC(), domain.DictSavePayload{
		Code: strPtr("book_tag"), Name: strPtr("书籍标签"), IsBuiltin: intPtr(1),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if entity.IsBuiltin == nil || *entity.IsBuiltin != 1 {
		t.Fatalf("创建未携带内置标记: %+v", entity)
	}
	stored, err := env.dicts.FindDictByID(env.ctx, entity.ID)
	if err != nil || stored.IsBuiltin != 1 {
		t.Fatalf("落库值异常: %+v %v", stored, err)
	}

	updated, err := env.svc.UpdateDict(env.ctx, rootRC(), entity.ID, domain.DictSavePayload{
		IsBuiltin: intPtr(0), Description: strPtr("备注"),
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if updated.IsBuiltin == nil || *updated.IsBuiltin != 0 || updated.Description == nil || *updated.Description != "备注" {
		t.Fatalf("更新未生效: %+v", updated)
	}
}

func TestDeleteBuiltinDictRejected(t *testing.T) {
	env := newTestEnv(t)
	record := addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)
	record.IsBuiltin = 1
	env.dicts.addDict(record)

	err := env.svc.DeleteDict(env.ctx, "d1")
	assertBusiness(t, err, 1, MessageDictBuiltinUndeletable)
	if _, err := env.dicts.FindDictByID(env.ctx, "d1"); err != nil {
		t.Fatal("内置字典应保留")
	}
}

func TestCreateAndUpdateDataItemCarryColorAndBuiltin(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)

	entity, err := env.svc.CreateDataItem(env.ctx, rootRC(), "d1", domain.DictDataSavePayload{
		Code: strPtr("enabled"), Name: strPtr("启用"), Value: strPtr("enabled"),
		Color: strPtr("green"), IsBuiltin: intPtr(1),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if entity.IsBuiltin == nil || *entity.IsBuiltin != 1 || entity.Color != "green" {
		t.Fatalf("创建未携带新字段: %+v", entity)
	}
	stored, err := env.dicts.FindDataItemByID(env.ctx, entity.ID)
	if err != nil || stored.IsBuiltin != 1 || stored.Color != "green" {
		t.Fatalf("落库值异常: %+v %v", stored, err)
	}

	updated, err := env.svc.UpdateDataItem(env.ctx, rootRC(), "d1", entity.ID, domain.DictDataSavePayload{
		Color: strPtr("red"), IsBuiltin: intPtr(0),
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if updated.Color != "red" || updated.IsBuiltin == nil || *updated.IsBuiltin != 0 {
		t.Fatalf("更新未生效: %+v", updated)
	}
}

func TestDeleteBuiltinDataItemRejected(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)
	env.dicts.addData(domain.DictDataRecord{
		ID: "i1", DictID: "d1", Code: "enabled", Name: "启用", Value: "enabled",
		IsEnabled: 1, IsBuiltin: 1, CreatedAt: fixedNow, UpdatedAt: fixedNow,
	})

	err := env.svc.DeleteDataItem(env.ctx, "d1", "i1", false)
	assertBusiness(t, err, 1, MessageDictDataBuiltinUndeletable)
	if _, err := env.dicts.FindDataItemByID(env.ctx, "i1"); err != nil {
		t.Fatal("内置数据项应保留")
	}
}

func TestDeleteDataItemRejectedWhenDescendantBuiltin(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)
	env.dicts.addData(domain.DictDataRecord{
		ID: "parent", DictID: "d1", Code: "p", Name: "父", Value: "p",
		IsEnabled: 1, CreatedAt: fixedNow, UpdatedAt: fixedNow,
	})
	env.dicts.addData(domain.DictDataRecord{
		ID: "child", DictID: "d1", ParentID: strPtr("parent"), Code: "c", Name: "子", Value: "c",
		IsEnabled: 1, IsBuiltin: 1, CreatedAt: fixedNow, UpdatedAt: fixedNow,
	})

	err := env.svc.DeleteDataItem(env.ctx, "d1", "parent", true)
	assertBusiness(t, err, 1, MessageDictDataBuiltinUndeletable)
	if _, err := env.dicts.FindDataItemByID(env.ctx, "parent"); err != nil {
		t.Fatal("含内置子项的父数据项应保留")
	}
}

func TestBatchSaveDataItemsKeepsBuiltinFlagWhenAbsent(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)
	env.dicts.addData(domain.DictDataRecord{
		ID: "i1", DictID: "d1", Code: "enabled", Name: "启用", Value: "enabled",
		IsEnabled: 1, IsBuiltin: 1, Color: "green", CreatedAt: fixedNow, UpdatedAt: fixedNow,
	})

	if err := env.svc.BatchSaveDataItems(env.ctx, rootRC(), "d1", []domain.DictDataSavePayload{{
		ID: strPtr("i1"), Code: strPtr("enabled"), Name: strPtr("启用"), Value: strPtr("enabled"),
		Color: strPtr("blue"),
	}}); err != nil {
		t.Fatalf("批量保存失败: %v", err)
	}
	stored, err := env.dicts.FindDataItemByID(env.ctx, "i1")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if stored.IsBuiltin != 1 || stored.Color != "blue" {
		t.Fatalf("批量保存结果异常: %+v", stored)
	}
}

func TestListTypesReturnsBuiltin(t *testing.T) {
	env := newTestEnv(t)
	env.types.addType(domain.I18nTypeRecord{
		ID: "t1", Code: "zh-CN", Name: "中文", SortOrder: 1, IsEnabled: 1, IsBuiltin: 1,
		CreatedAt: fixedNow, UpdatedAt: fixedNow,
	})

	page, err := env.svc.ListTypes(env.ctx, 1, 10, "", nil)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	list := page.(result.PageData[domain.I18nTypeEntity]).List
	if len(list) != 1 || list[0].IsBuiltin == nil || *list[0].IsBuiltin != 1 {
		t.Fatalf("内置标记缺失: %+v", list)
	}
	assertJSONContains(t, list[0], `"isBuiltin":1`)
}

func TestCreateAndUpdateTypeCarryBuiltin(t *testing.T) {
	env := newTestEnv(t)
	entity, err := env.svc.CreateType(env.ctx, rootRC(), I18nTypeSavePayload{
		Code: strPtr("zh-CN"), Name: strPtr("中文"), IsBuiltin: intPtr(1),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if entity.IsBuiltin == nil || *entity.IsBuiltin != 1 {
		t.Fatalf("创建未携带内置标记: %+v", entity)
	}
	stored, err := env.types.FindTypeByID(env.ctx, entity.ID)
	if err != nil || stored.IsBuiltin != 1 {
		t.Fatalf("落库值异常: %+v %v", stored, err)
	}

	updated, err := env.svc.UpdateType(env.ctx, rootRC(), entity.ID, I18nTypeSavePayload{IsBuiltin: intPtr(0)})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if updated.IsBuiltin == nil || *updated.IsBuiltin != 0 {
		t.Fatalf("更新未生效: %+v", updated)
	}
}

func TestSavePayloadsDecodeNewFields(t *testing.T) {
	var page domain.PageSavePayload
	if err := json.Unmarshal([]byte(`{"permissionCode":"devops:server:view","isBuiltin":1,"description":"说明"}`), &page); err != nil {
		t.Fatalf("页面载荷解析失败: %v", err)
	}
	if page.PermissionCode == nil || *page.PermissionCode != "devops:server:view" ||
		page.IsBuiltin == nil || *page.IsBuiltin != 1 ||
		page.Description == nil || *page.Description != "说明" {
		t.Fatalf("页面载荷字段缺失: %+v", page)
	}

	var dict domain.DictSavePayload
	if err := json.Unmarshal([]byte(`{"isBuiltin":1}`), &dict); err != nil {
		t.Fatalf("字典载荷解析失败: %v", err)
	}
	if dict.IsBuiltin == nil || *dict.IsBuiltin != 1 {
		t.Fatalf("字典载荷字段缺失: %+v", dict)
	}

	var data domain.DictDataSavePayload
	if err := json.Unmarshal([]byte(`{"color":"green","isBuiltin":1}`), &data); err != nil {
		t.Fatalf("数据项载荷解析失败: %v", err)
	}
	if data.Color == nil || *data.Color != "green" || data.IsBuiltin == nil || *data.IsBuiltin != 1 {
		t.Fatalf("数据项载荷字段缺失: %+v", data)
	}

	var i18nType I18nTypeSavePayload
	if err := json.Unmarshal([]byte(`{"isBuiltin":1}`), &i18nType); err != nil {
		t.Fatalf("语言类型载荷解析失败: %v", err)
	}
	if i18nType.IsBuiltin == nil || *i18nType.IsBuiltin != 1 {
		t.Fatalf("语言类型载荷字段缺失: %+v", i18nType)
	}
}

func TestDeleteBuiltinTypeRejected(t *testing.T) {
	env := newTestEnv(t)
	env.types.addType(domain.I18nTypeRecord{
		ID: "t1", Code: "zh-CN", Name: "中文", SortOrder: 1, IsEnabled: 1, IsBuiltin: 1,
		CreatedAt: fixedNow, UpdatedAt: fixedNow,
	})

	err := env.svc.DeleteType(env.ctx, "t1")
	assertBusiness(t, err, 1, MessageI18nTypeBuiltinUndeletable)
	if _, err := env.types.FindTypeByID(env.ctx, "t1"); err != nil {
		t.Fatal("内置语言类型应保留")
	}
}
