package service

import (
	"testing"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"github.com/acat-fun/acat-go-common/result"
)

func addType(env *testEnv, id, code, name string, sortOrder, isEnabled int) {
	env.types.addType(domain.I18nTypeRecord{
		ID: id, Code: code, Name: name, SortOrder: sortOrder, IsEnabled: isEnabled,
		CreatedAt: fixedNow, UpdatedAt: fixedNow,
	})
}

func TestListTypeOptionsOnlyEnabledSorted(t *testing.T) {
	env := newTestEnv(t)
	addType(env, "t1", "en", "English", 2, 1)
	addType(env, "t2", "zh-CN", "中文", 1, 1)
	addType(env, "t3", "ja", "日本語", 3, 0)

	options, err := env.svc.ListTypeOptions(env.ctx)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(options) != 2 {
		t.Fatalf("应只返回启用类型: %+v", options)
	}
	if options[0].Value != "zh-CN" || options[0].Label != "中文" || options[1].Value != "en" {
		t.Fatalf("排序/映射异常: %+v", options)
	}
}

func TestListTypesThreeStates(t *testing.T) {
	env := newTestEnv(t)
	addType(env, "t1", "en", "English", 2, 1)
	addType(env, "t2", "zh-CN", "中文", 1, 1)

	page, err := env.svc.ListTypes(env.ctx, 1, 10, "", nil)
	if err != nil {
		t.Fatalf("分页失败: %v", err)
	}
	data := page.(result.PageData[domain.I18nTypeEntity])
	if data.Total != 2 || len(data.List) != 2 {
		t.Fatalf("分页结果异常: %+v", data)
	}
	if data.List[0].Version == nil || *data.List[0].Version != 0 {
		t.Fatalf("version 字段缺失: %+v", data.List[0])
	}

	all, err := env.svc.ListAllTypes(env.ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("全量失败: %v %v", all, err)
	}

	enabled := 1
	page, err = env.svc.ListTypes(env.ctx, 1, 10, "zh", &enabled)
	if err != nil {
		t.Fatalf("过滤失败: %v", err)
	}
	if got := page.(result.PageData[domain.I18nTypeEntity]); len(got.List) != 1 || got.List[0].Code != "zh-CN" {
		t.Fatalf("keyword/isEnabled 过滤异常: %+v", got.List)
	}
}

func TestCreateTypeDuplicate(t *testing.T) {
	env := newTestEnv(t)
	addType(env, "t1", "en", "English", 1, 1)

	_, err := env.svc.CreateType(env.ctx, rootRC(), I18nTypeSavePayload{Code: strPtr("en"), Name: strPtr("英文")})
	assertBusiness(t, err, 1, "语言编码已存在: en")
}

func TestCreateTypeDefaults(t *testing.T) {
	env := newTestEnv(t)
	entity, err := env.svc.CreateType(env.ctx, rootRC(), I18nTypeSavePayload{Code: strPtr("ja"), Name: strPtr("日本語")})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if entity.IsEnabled == nil || *entity.IsEnabled != 1 || entity.SortOrder == nil || *entity.SortOrder != 0 {
		t.Fatalf("默认值异常: %+v", entity)
	}
	if entity.IsDeleted == nil || *entity.IsDeleted != 0 {
		t.Fatalf("isDeleted 应为 0: %+v", entity)
	}
}

func TestUpdateAndDeleteType(t *testing.T) {
	env := newTestEnv(t)
	addType(env, "t1", "en", "English", 1, 1)

	entity, err := env.svc.UpdateType(env.ctx, rootRC(), "t1", I18nTypeSavePayload{Name: strPtr("英文"), IsEnabled: intPtr(0)})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if entity.Code != "en" || entity.Name != "英文" || entity.IsEnabled == nil || *entity.IsEnabled != 0 {
		t.Fatalf("更新结果异常: %+v", entity)
	}
	if entity.Version == nil || *entity.Version != 1 {
		t.Fatalf("版本应递增: %v", entity.Version)
	}

	if err := env.svc.DeleteType(env.ctx, "t1"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := env.types.FindTypeByID(env.ctx, "t1"); err == nil {
		t.Fatal("类型应已软删")
	}
}

func TestUpdateTypeNotFound(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.svc.UpdateType(env.ctx, rootRC(), "missing", I18nTypeSavePayload{})
	assertBusiness(t, err, 1, MessageI18nTypeGone)
	if err := env.svc.DeleteType(env.ctx, "missing"); err == nil {
		t.Fatal("删除不存在类型应失败")
	}
}

func TestFrontendLabelsDefaultDictCodeAndAliasOrder(t *testing.T) {
	env := newTestEnv(t)
	env.dicts.frontendLabels = []domain.FrontendLabelRecord{
		{Code: "acat.read.admin.system.base-config.files.file.businessType", LabelValue: "业务类型"},
		{Code: "plain.code", LabelValue: "普通"},
	}

	labels, err := env.svc.FrontendLabels(env.ctx, rootRC(), "")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	keys := labels.Keys()
	want := []string{
		"acat.read.admin.system.base-config.files.file.businessType",
		"file.businessType",
		"plain.code",
	}
	if len(keys) != len(want) {
		t.Fatalf("键数量 = %d，期望 %d（%v）", len(keys), len(want), keys)
	}
	for index, key := range want {
		if keys[index] != key {
			t.Fatalf("键顺序异常: %v", keys)
		}
	}
	// 空 dictCode 回退 i18n_admin_label（由 httpapi 层传入空串时 service 兜底）。
	if value, _ := labels.Get("plain.code"); value != "普通" {
		t.Fatalf("值异常: %q", value)
	}
}

func TestFrontendLabelsCustomDictCodeAndEmptyResult(t *testing.T) {
	env := newTestEnv(t)
	labels, err := env.svc.FrontendLabels(env.ctx, rootRC(), "i18n_app_label")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if labels.Len() != 0 {
		t.Fatalf("无数据应返回空 Map: %v", labels.Keys())
	}
	encoded, err := labels.MarshalJSON()
	if err != nil || string(encoded) != "{}" {
		t.Fatalf("空 Map 应序列化为 {}，实际 %s (%v)", encoded, err)
	}
}
