package service

import (
	"testing"

	"github.com/acat-fun/acat-go-admin-system/domain"
	"github.com/acat-fun/acat-go-common/result"
)

func strPtr(value string) *string { return &value }

func addDict(t *testing.T, env *testEnv, id, code, name string, isEnabled, isTree, scope int) domain.DictRecord {
	t.Helper()
	record := domain.DictRecord{
		ID: id, Code: code, Name: name,
		IsEnabled: isEnabled, IsTree: isTree, Scope: scope,
		CreatedAt: fixedNow, UpdatedAt: fixedNow,
	}
	env.dicts.addDict(record)
	return record
}

func TestCreateDictDuplicateCodeFails(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)

	_, err := env.svc.CreateDict(env.ctx, rootRC(), domain.DictSavePayload{Code: strPtr("book_tag"), Name: strPtr("重复")})
	assertBusiness(t, err, 1, "字典编码已存在: book_tag")
}

func TestCreateDictDefaultsAndLabels(t *testing.T) {
	env := newTestEnv(t)
	payload := domain.DictSavePayload{
		Code: strPtr("book_tag"),
		Name: strPtr("ASCII"),
		I18nValue: []domain.I18nValue{
			{I18n: "en", Value: "Book Tag"},
			{I18n: "zh-CN", Value: "书籍标签"},
		},
	}
	entity, err := env.svc.CreateDict(env.ctx, rootRC(), payload)
	if err != nil {
		t.Fatalf("创建字典失败: %v", err)
	}
	if entity.IsEnabled == nil || *entity.IsEnabled != 1 ||
		entity.IsTree == nil || *entity.IsTree != 0 ||
		entity.Scope == nil || *entity.Scope != 0 {
		t.Fatalf("默认值异常: %+v", entity)
	}
	if entity.Name != "书籍标签" {
		t.Fatalf("名称应取 zh-CN，实际 %q", entity.Name)
	}
	if entity.IsDeleted == nil || *entity.IsDeleted != 0 || entity.Version == nil || *entity.Version != 0 {
		t.Fatalf("审计字段异常: %+v", entity)
	}
	if entity.CreatedAt == nil || *entity.CreatedAt != "2026-09-14T01:02:03" {
		t.Fatalf("createdAt 异常: %v", entity.CreatedAt)
	}
	labels, err := env.dicts.ListNameLabels(env.ctx, domain.I18nTableDict, entity.ID)
	if err != nil || len(labels) != 2 {
		t.Fatalf("标签写入异常: %v %v", labels, err)
	}
}

func TestListDictsThreeStates(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)
	addDict(t, env, "d2", "admin_icon", "后台图标", 1, 1, 0)

	// 分页
	page, err := env.svc.ListDicts(env.ctx, rootRC(), domain.DictFilter{}, 1, 10)
	if err != nil {
		t.Fatalf("分页查询失败: %v", err)
	}
	data := page.(result.PageData[domain.DictVO])
	if data.Total != 2 || len(data.List) != 2 {
		t.Fatalf("分页结果异常: %+v", data)
	}
	if data.List[0].DataItems != nil {
		t.Fatal("dataItems 必须为 null")
	}
	if data.List[0].DataCount != 0 {
		t.Fatalf("dataCount = %d", data.List[0].DataCount)
	}

	// all=true
	all, err := env.svc.ListAllDicts(env.ctx, rootRC())
	if err != nil || len(all) != 2 {
		t.Fatalf("全量查询异常: %v %v", all, err)
	}

	// format=select → label 为当前语言名称、value 为 code
	options, err := env.svc.ListDictsForSelect(env.ctx, rootRC())
	if err != nil || len(options) != 2 {
		t.Fatalf("下拉查询异常: %v %v", options, err)
	}
	codes := map[string]string{}
	for _, option := range options {
		codes[option.Value] = option.Label
	}
	if codes["book_tag"] != "书籍标签" || codes["admin_icon"] != "后台图标" {
		t.Fatalf("下拉内容异常: %v", codes)
	}
}

func TestListDictsFiltersAndCurrentLanguageName(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)
	addDict(t, env, "d2", "admin_icon", "后台图标", 0, 0, 0)
	_ = env.dicts.SaveNameLabels(env.ctx, domain.I18nTableDict, "d1",
		[]domain.I18nValue{{I18n: "en", Value: "Book Tag"}})

	enabled := 1
	page, err := env.svc.ListDicts(env.ctx, RequestContext{I18nCode: "en"}, domain.DictFilter{IsEnabled: &enabled}, 1, 10)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	data := page.(result.PageData[domain.DictVO])
	if len(data.List) != 1 || data.List[0].Name != "Book Tag" {
		t.Fatalf("语言标签回退异常: %+v", data.List)
	}

	scope := 0
	page, err = env.svc.ListDicts(env.ctx, rootRC(), domain.DictFilter{Scope: &scope}, 1, 10)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if got := page.(result.PageData[domain.DictVO]); len(got.List) != 1 || got.List[0].Code != "admin_icon" {
		t.Fatalf("scope 过滤异常: %+v", got.List)
	}
}

func TestUpdateDictOnlyNonNullFieldsApplied(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)

	entity, err := env.svc.UpdateDict(env.ctx, rootRC(), "d1",
		domain.DictSavePayload{IsEnabled: intPtr(0), Description: strPtr("备注")})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if entity.Code != "book_tag" || entity.Name != "书籍标签" {
		t.Fatalf("code/name 不应被改动: %+v", entity)
	}
	if entity.IsEnabled == nil || *entity.IsEnabled != 0 || entity.Description == nil || *entity.Description != "备注" {
		t.Fatalf("字段更新异常: %+v", entity)
	}
}

func TestUpdateDictNotFound(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.svc.UpdateDict(env.ctx, rootRC(), "missing", domain.DictSavePayload{})
	assertBusiness(t, err, 1, MessageDictNotFound)
}

func TestDeleteDictCascadesDataItems(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)
	env.dicts.addData(domain.DictDataRecord{ID: "i1", DictID: "d1", Code: "c1", Name: "n1", Value: "v1", IsEnabled: 1})
	env.dicts.addData(domain.DictDataRecord{ID: "i2", DictID: "d1", Code: "c2", Name: "n2", Value: "v2", IsEnabled: 0})
	_ = env.dicts.SaveNameLabels(env.ctx, domain.I18nTableDictData, "i1", []domain.I18nValue{{I18n: "zh-CN", Value: "n1"}})

	if err := env.svc.DeleteDict(env.ctx, "d1"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := env.dicts.FindDictByID(env.ctx, "d1"); err == nil {
		t.Fatal("字典应已软删")
	}
	for _, id := range []string{"i1", "i2"} {
		if _, err := env.dicts.FindDataItemByID(env.ctx, id); err == nil {
			t.Fatalf("数据项 %s 应已软删", id)
		}
	}
	labels, _ := env.dicts.ListNameLabels(env.ctx, domain.I18nTableDictData, "i1")
	if len(labels) != 0 {
		t.Fatalf("标签应已清理: %v", labels)
	}
}

func TestCreateDataItemValidatesFrontendLabelCode(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", domain.FrontendAdminDictCode, "管理后台文案", 1, 0, 0)

	_, err := env.svc.CreateDataItem(env.ctx, rootRC(), "d1", domain.DictDataSavePayload{
		Code: strPtr("btn.save"), Name: strPtr("保存"), Value: strPtr("save"),
	})
	assertBusiness(t, err, 1, "前端国际化编码必须符合 acat.read.admin.{nav}.{menu}/{page}.{具体显示简意}")

	entity, err := env.svc.CreateDataItem(env.ctx, rootRC(), "d1", domain.DictDataSavePayload{
		Code: strPtr("acat.read.admin.system.base-config/files.file.businessType"),
		Name: strPtr("业务类型"), Value: strPtr("businessType"),
	})
	if err != nil {
		t.Fatalf("合法编码应通过: %v", err)
	}
	if entity.DictID != "d1" || entity.SortOrder == nil || *entity.SortOrder != 0 {
		t.Fatalf("创建结果异常: %+v", entity)
	}
}

func TestCreateDataItemRejectsForeignParent(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)
	addDict(t, env, "d2", "admin_icon", "后台图标", 1, 0, 0)
	env.dicts.addData(domain.DictDataRecord{ID: "i9", DictID: "d2", Code: "x", Name: "x", Value: "x", IsEnabled: 1})

	_, err := env.svc.CreateDataItem(env.ctx, rootRC(), "d1", domain.DictDataSavePayload{
		Code: strPtr("child"), Name: strPtr("子"), Value: strPtr("v"), ParentID: strPtr("i9"),
	})
	assertBusiness(t, err, 1, MessageDictDataParentNotFd)
}

func TestListDataItemsMissingDictReturnsEmptyPage(t *testing.T) {
	env := newTestEnv(t)
	page, err := env.svc.ListDataItems(env.ctx, rootRC(), "nope", 1, 10, domain.DictDataFilter{})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	data := page.(result.PageData[domain.DictDataVO])
	if data.Total != 0 || len(data.List) != 0 {
		t.Fatalf("行为应为空分页: %+v", data)
	}
}

func TestListDataItemsTreePagingSlicesRoots(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 1, 1)
	env.dicts.addData(domain.DictDataRecord{ID: "r1", DictID: "d1", Code: "r1", Name: "根1", Value: "1", IsEnabled: 1})
	env.dicts.addData(domain.DictDataRecord{ID: "r2", DictID: "d1", Code: "r2", Name: "根2", Value: "2", IsEnabled: 1})
	env.dicts.addData(domain.DictDataRecord{ID: "c1", DictID: "d1", ParentID: strPtr("r1"), Code: "c1", Name: "子1", Value: "3", IsEnabled: 1})

	page, err := env.svc.ListDataItems(env.ctx, rootRC(), "d1", 1, 1, domain.DictDataFilter{})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	data := page.(result.PageData[domain.DictDataVO])
	// total 是全部节点数；headNodeTotal 是根节点数（*int64，树分页时为根节点数）；分页切的是根节点。
	if data.Total != 3 || data.HeadNodeTotal == nil || *data.HeadNodeTotal != 2 || len(data.List) != 1 {
		t.Fatalf("树分页异常: %+v", data)
	}
	if data.List[0].Children == nil || len(data.List[0].Children) != 1 {
		t.Fatalf("子节点应挂在根上: %+v", data.List[0])
	}
}

func TestListAllDataItemsUsesCodeOnly(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)
	env.dicts.addData(domain.DictDataRecord{ID: "i1", DictID: "d1", Code: "c1", Name: "n1", Value: "v1", IsEnabled: 0})

	// all=true 分支只按 code 解析：传 id 应得到空数组。
	byID, err := env.svc.ListAllDataItems(env.ctx, rootRC(), "d1")
	if err != nil || len(byID) != 0 {
		t.Fatalf("传 id 应返回空数组: %v %v", byID, err)
	}
	byCode, err := env.svc.ListAllDataItems(env.ctx, rootRC(), "book_tag")
	if err != nil || len(byCode) != 1 {
		t.Fatalf("传 code 应命中: %v %v", byCode, err)
	}
	if byCode[0].Children != nil {
		t.Fatal("扁平字典 children 应为 null")
	}
}

func TestListDataItemsForSelectSkipsDisabledAndUsesValue(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)
	env.dicts.addData(domain.DictDataRecord{ID: "i1", DictID: "d1", Code: "c1", Name: "启用项", Value: "v1", IsEnabled: 1})
	env.dicts.addData(domain.DictDataRecord{ID: "i2", DictID: "d1", Code: "c2", Name: "停用项", Value: "v2", IsEnabled: 0})

	value, err := env.svc.ListDataItemsForSelect(env.ctx, rootRC(), "book_tag")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	options, ok := value.([]domain.SelectVO)
	if !ok || len(options) != 1 {
		t.Fatalf("下拉结果异常: %#v", value)
	}
	if options[0].Label != "启用项" || options[0].Value != "v1" {
		t.Fatalf("label/value 异常: %+v", options[0])
	}
}

func TestDeleteDataItemCascadeRemovesDescendants(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 1, 1)
	env.dicts.addData(domain.DictDataRecord{ID: "r1", DictID: "d1", Code: "r1", Name: "根", Value: "1", IsEnabled: 1})
	env.dicts.addData(domain.DictDataRecord{ID: "c1", DictID: "d1", ParentID: strPtr("r1"), Code: "c1", Name: "子", Value: "2", IsEnabled: 1})
	env.dicts.addData(domain.DictDataRecord{ID: "g1", DictID: "d1", ParentID: strPtr("c1"), Code: "g1", Name: "孙", Value: "3", IsEnabled: 1})

	if err := env.svc.DeleteDataItem(env.ctx, "d1", "r1", false); err != nil {
		t.Fatalf("非级联删除失败: %v", err)
	}
	if _, err := env.dicts.FindDataItemByID(env.ctx, "c1"); err != nil {
		t.Fatal("非级联不应删除子节点")
	}

	env.dicts.addData(domain.DictDataRecord{ID: "r1", DictID: "d1", Code: "r1", Name: "根", Value: "1", IsEnabled: 1})
	if err := env.svc.DeleteDataItem(env.ctx, "d1", "r1", true); err != nil {
		t.Fatalf("级联删除失败: %v", err)
	}
	for _, id := range []string{"r1", "c1", "g1"} {
		if _, err := env.dicts.FindDataItemByID(env.ctx, id); err == nil {
			t.Fatalf("级联删除应移除 %s", id)
		}
	}
}

func TestDeleteDataItemRejectsForeignDict(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)
	addDict(t, env, "d2", "admin_icon", "后台图标", 1, 0, 0)
	env.dicts.addData(domain.DictDataRecord{ID: "i1", DictID: "d2", Code: "c1", Name: "n", Value: "v", IsEnabled: 1})

	err := env.svc.DeleteDataItem(env.ctx, "d1", "i1", false)
	assertBusiness(t, err, 1, MessageDictDataNotFound)
}

func TestBatchSaveDataItemsUpsertsAndDeletes(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)
	env.dicts.addData(domain.DictDataRecord{ID: "keep", DictID: "d1", Code: "keep", Name: "旧名", Value: "v", IsEnabled: 1, Version: 0})
	env.dicts.addData(domain.DictDataRecord{ID: "drop", DictID: "d1", Code: "drop", Name: "待删", Value: "v", IsEnabled: 1})

	payloads := []domain.DictDataSavePayload{
		{Code: strPtr("new"), Name: strPtr("新增"), Value: strPtr("n")},
		{ID: strPtr("keep"), Code: strPtr("keep"), Name: strPtr("新名"), Value: strPtr("v")},
		{ID: strPtr("drop"), IsDeleted: intPtr(1)},
		{IsDeleted: intPtr(1)},
	}
	if err := env.svc.BatchSaveDataItems(env.ctx, rootRC(), "d1", payloads); err != nil {
		t.Fatalf("批量保存失败: %v", err)
	}
	updated, err := env.dicts.FindDataItemByID(env.ctx, "keep")
	if err != nil || updated.Name != "新名" {
		t.Fatalf("修改分支异常: %+v %v", updated, err)
	}
	if _, err := env.dicts.FindDataItemByID(env.ctx, "drop"); err == nil {
		t.Fatal("删除分支应软删记录")
	}
	all, _ := env.dicts.ListAllDataItems(env.ctx, "d1", nil)
	if len(all) != 2 {
		t.Fatalf("批量后应有 2 条（新增 + 保留），实际 %d", len(all))
	}
}

func TestBatchSaveRejectsWholeBatchOnBadLabelCode(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", domain.FrontendAppDictCode, "用户端文案", 1, 0, 1)

	err := env.svc.BatchSaveDataItems(env.ctx, rootRC(), "d1", []domain.DictDataSavePayload{
		{Code: strPtr("bad-code"), Name: strPtr("坏"), Value: strPtr("v")},
	})
	assertBusiness(t, err, 1, "前端国际化编码必须符合 acat.read.app")
	if items, _ := env.dicts.ListAllDataItems(env.ctx, "d1", nil); len(items) != 0 {
		t.Fatalf("整批应失败且不写入: %v", items)
	}
}

func TestUpdateDataItemKeepsDictAndBumpsVersion(t *testing.T) {
	env := newTestEnv(t)
	addDict(t, env, "d1", "book_tag", "书籍标签", 1, 0, 1)
	env.dicts.addData(domain.DictDataRecord{ID: "i1", DictID: "d1", Code: "c1", Name: "旧", Value: "v1", IsEnabled: 1})

	entity, err := env.svc.UpdateDataItem(env.ctx, rootRC(), "d1", "i1", domain.DictDataSavePayload{
		Name: strPtr("新"), Value: strPtr("v2"), DictID: strPtr("其它"),
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if entity.DictID != "d1" || entity.Name != "新" || entity.Value != "v2" {
		t.Fatalf("更新结果异常: %+v", entity)
	}
	if entity.Version == nil || *entity.Version != 1 {
		t.Fatalf("版本应递增为 1，实际 %v", entity.Version)
	}
}
