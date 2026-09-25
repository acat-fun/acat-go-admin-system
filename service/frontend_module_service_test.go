package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

// TestListFrontendModulesEmitsNullFallbackManifestPath 锁定字段回退行为
// fallback_version/fallback_manifest_path 为 NULL 时 JSON 输出 null（不能是 ""）。
func TestListFrontendModulesEmitsNullFallbackManifestPath(t *testing.T) {
	env := newTestEnv(t)
	addModule(env, "m1", "system", "系统管理", "1.0.0", domain.FrontendModuleStatusEnabled, 1, 0)

	list, err := env.svc.ListFrontendModules(env.ctx, "")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("模块数 = %d", len(list))
	}
	if list[0].FallbackManifestPath != nil || list[0].FallbackVersion != nil {
		t.Errorf("NULL 回退字段应为 nil: %+v", list[0])
	}

	raw, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	body := string(raw)
	for _, want := range []string{`"fallbackManifestPath":null`, `"fallbackVersion":null`,
		`"createdAt":"2026-09-14T01:02:03"`, `"updatedAt":"2026-09-14T01:02:03"`} {
		if !strings.Contains(body, want) {
			t.Errorf("模块 JSON 缺少 %s: %s", want, body)
		}
	}
	if strings.Contains(body, `"fallbackManifestPath":""`) {
		t.Errorf("fallbackManifestPath 不应输出空串: %s", body)
	}
}

func addModule(env *testEnv, id, code, name, version string, status, sortOrder, recordVersion int) {
	env.modules.addModule(domain.FrontendModuleRecord{
		ID: id, ModuleCode: code, Name: name, ReleaseVersion: version, ContractVersion: 1,
		ManifestPath: "/admin-remotes/" + code + "/" + version + "/mf-manifest.json",
		Status:       status, SortOrder: sortOrder, Version: recordVersion,
		CreatedAt: fixedNow, UpdatedAt: fixedNow,
	})
}

func TestCreateFrontendModuleRejectsDuplicateVersion(t *testing.T) {
	env := newTestEnv(t)
	addModule(env, "m1", "system", "系统管理", "1.0.0", domain.FrontendModuleStatusEnabled, 1, 0)

	_, err := env.svc.CreateFrontendModule(env.ctx, domain.FrontendModuleSaveParam{
		ModuleCode:      strPtr("system"),
		Name:            strPtr("系统管理前端"),
		ReleaseVersion:  strPtr("1.0.0"),
		ContractVersion: intPtr(1),
		ManifestPath:    strPtr("/admin-remotes/system/1.0.0/mf-manifest.json"),
		SortOrder:       intPtr(1),
	})
	assertBusiness(t, err, 1, MessageModuleVersionExists)
}

func TestCreateFrontendModuleDraftStatus(t *testing.T) {
	env := newTestEnv(t)
	vo, err := env.svc.CreateFrontendModule(env.ctx, domain.FrontendModuleSaveParam{
		ModuleCode:      strPtr("cat-read"),
		Name:            strPtr("内容运营前端"),
		ReleaseVersion:  strPtr("1.0.0"),
		ContractVersion: intPtr(1),
		ManifestPath:    strPtr("/admin-remotes/cat-read/1.0.0/mf-manifest.json"),
		SortOrder:       intPtr(2),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if vo.Status != domain.FrontendModuleStatusDraft {
		t.Fatalf("新建模块状态应为草稿 0，实际 %d", vo.Status)
	}
	if vo.Version != 0 || vo.FallbackManifestPath != nil {
		t.Fatalf("初始字段异常: %+v", vo)
	}
}

func TestCreateFrontendModuleValidatesManifestPath(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.svc.CreateFrontendModule(env.ctx, domain.FrontendModuleSaveParam{
		ModuleCode:      strPtr("cat-read"),
		Name:            strPtr("内容"),
		ReleaseVersion:  strPtr("1.0.0"),
		ContractVersion: intPtr(1),
		ManifestPath:    strPtr("https://evil.example/mf-manifest.json"),
		SortOrder:       intPtr(1),
	})
	assertBusiness(t, err, 1, MessageModuleManifestInvalid)

	_, err = env.svc.CreateFrontendModule(env.ctx, domain.FrontendModuleSaveParam{
		ModuleCode:      strPtr("cat-read"),
		Name:            strPtr("内容"),
		ReleaseVersion:  strPtr("1.0.0"),
		ContractVersion: intPtr(2),
		ManifestPath:    strPtr("/admin-remotes/cat-read/1.0.0/mf-manifest.json"),
		SortOrder:       intPtr(1),
	})
	assertBusiness(t, err, 1, MessageModuleContractInvalid)
}

func TestCreateFrontendModuleFallbackRules(t *testing.T) {
	env := newTestEnv(t)
	addModule(env, "m1", "cat-read", "内容运营", "1.0.0", domain.FrontendModuleStatusEnabled, 1, 0)

	// 回退版本不能指向当前版本。
	_, err := env.svc.CreateFrontendModule(env.ctx, domain.FrontendModuleSaveParam{
		ModuleCode:      strPtr("cat-read"),
		Name:            strPtr("内容"),
		ReleaseVersion:  strPtr("1.1.0"),
		ContractVersion: intPtr(1),
		ManifestPath:    strPtr("/admin-remotes/cat-read/1.1.0/mf-manifest.json"),
		FallbackVersion: strPtr("1.1.0"),
		SortOrder:       intPtr(1),
	})
	assertBusiness(t, err, 1, MessageModuleFallbackSame)

	// 回退版本不存在且未提供回退 manifest。
	_, err = env.svc.CreateFrontendModule(env.ctx, domain.FrontendModuleSaveParam{
		ModuleCode:      strPtr("cat-read"),
		Name:            strPtr("内容"),
		ReleaseVersion:  strPtr("1.1.0"),
		ContractVersion: intPtr(1),
		ManifestPath:    strPtr("/admin-remotes/cat-read/1.1.0/mf-manifest.json"),
		FallbackVersion: strPtr("0.9.0"),
		SortOrder:       intPtr(1),
	})
	assertBusiness(t, err, 1, MessageModuleFallbackMissing)

	// 回退版本存在 → 自动取库中该版本的 manifest_path。
	vo, err := env.svc.CreateFrontendModule(env.ctx, domain.FrontendModuleSaveParam{
		ModuleCode:      strPtr("cat-read"),
		Name:            strPtr("内容"),
		ReleaseVersion:  strPtr("1.1.0"),
		ContractVersion: intPtr(1),
		ManifestPath:    strPtr("/admin-remotes/cat-read/1.1.0/mf-manifest.json"),
		FallbackVersion: strPtr("1.0.0"),
		SortOrder:       intPtr(1),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if vo.FallbackManifestPath == nil || *vo.FallbackManifestPath != "/admin-remotes/cat-read/1.0.0/mf-manifest.json" {
		t.Fatalf("回退 manifest 应取库中值: %+v", vo)
	}
}

func TestUpdateFrontendModuleRejectsCodeChange(t *testing.T) {
	env := newTestEnv(t)
	addModule(env, "m1", "system", "系统管理", "1.0.0", domain.FrontendModuleStatusEnabled, 1, 0)

	_, err := env.svc.UpdateFrontendModule(env.ctx, "m1", domain.FrontendModuleSaveParam{
		ModuleCode:      strPtr("cat-read"),
		Name:            strPtr("换码"),
		ReleaseVersion:  strPtr("1.0.0"),
		ContractVersion: intPtr(1),
		ManifestPath:    strPtr("/admin-remotes/cat-read/1.0.0/mf-manifest.json"),
		SortOrder:       intPtr(1),
	})
	assertBusiness(t, err, 1, MessageModuleCodeImmutable)
}

func TestUpdateFrontendModuleNotFound(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.svc.UpdateFrontendModule(env.ctx, "missing", domain.FrontendModuleSaveParam{})
	assertBusiness(t, err, 1, MessageModuleNotFound)
}

func TestPublishFrontendModuleOptimisticLock(t *testing.T) {
	env := newTestEnv(t)
	addModule(env, "m1", "cat-read", "内容运营", "1.0.0", domain.FrontendModuleStatusDraft, 1, 3)

	// 期望版本与库中不一致 → 40901（HTTP 仍为 200）。
	_, err := env.svc.PublishFrontendModule(env.ctx, "m1", domain.FrontendModulePublicationParam{
		ReleaseVersion:  strPtr("1.0.0"),
		ContractVersion: intPtr(1),
		ManifestPath:    strPtr("/admin-remotes/cat-read/1.0.0/mf-manifest.json"),
		Status:          intPtr(1),
		ExpectedVersion: intPtr(2),
	})
	assertBusiness(t, err, BusinessCodeFrontendModuleConflict, MessageModuleConflict)

	// 期望版本缺失 → 同样 40901。
	_, err = env.svc.PublishFrontendModule(env.ctx, "m1", domain.FrontendModulePublicationParam{
		ReleaseVersion:  strPtr("1.0.0"),
		ContractVersion: intPtr(1),
		ManifestPath:    strPtr("/admin-remotes/cat-read/1.0.0/mf-manifest.json"),
		Status:          intPtr(1),
	})
	assertBusiness(t, err, BusinessCodeFrontendModuleConflict, MessageModuleConflict)

	// 版本一致 → 成功发布，版本递增。
	vo, err := env.svc.PublishFrontendModule(env.ctx, "m1", domain.FrontendModulePublicationParam{
		ReleaseVersion:  strPtr("1.1.0"),
		ContractVersion: intPtr(1),
		ManifestPath:    strPtr("/admin-remotes/cat-read/1.1.0/mf-manifest.json"),
		Status:          intPtr(1),
		ExpectedVersion: intPtr(3),
	})
	if err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	if vo.Status != domain.FrontendModuleStatusEnabled || vo.Version != 4 {
		t.Fatalf("发布结果异常: %+v", vo)
	}
}

func TestPublishFrontendModuleZeroRowsAffected(t *testing.T) {
	env := newTestEnv(t)
	addModule(env, "m1", "cat-read", "内容运营", "1.0.0", domain.FrontendModuleStatusDraft, 1, 0)
	// 模拟并发更新导致的 0 行受影响。
	env.modules.forceUpdateMiss = true

	_, err := env.svc.PublishFrontendModule(env.ctx, "m1", domain.FrontendModulePublicationParam{
		ReleaseVersion:  strPtr("1.0.0"),
		ContractVersion: intPtr(1),
		ManifestPath:    strPtr("/admin-remotes/cat-read/1.0.0/mf-manifest.json"),
		Status:          intPtr(1),
		ExpectedVersion: intPtr(0),
	})
	assertBusiness(t, err, BusinessCodeFrontendModuleConflict, MessageModuleConflict)
}

func TestPublishFrontendModuleSystemProtectionAndStatusWhitelist(t *testing.T) {
	env := newTestEnv(t)
	addModule(env, "m1", "system", "系统管理", "1.0.0", domain.FrontendModuleStatusEnabled, 1, 0)

	_, err := env.svc.PublishFrontendModule(env.ctx, "m1", domain.FrontendModulePublicationParam{
		ReleaseVersion:  strPtr("1.0.0"),
		ContractVersion: intPtr(1),
		ManifestPath:    strPtr("/admin-remotes/system/1.0.0/mf-manifest.json"),
		Status:          intPtr(2),
		ExpectedVersion: intPtr(0),
	})
	assertBusiness(t, err, 1, MessageModuleSystemUndelete)

	addModule(env, "m2", "cat-read", "内容", "1.0.0", domain.FrontendModuleStatusDraft, 1, 0)
	_, err = env.svc.PublishFrontendModule(env.ctx, "m2", domain.FrontendModulePublicationParam{
		ReleaseVersion:  strPtr("1.0.0"),
		ContractVersion: intPtr(1),
		ManifestPath:    strPtr("/admin-remotes/cat-read/1.0.0/mf-manifest.json"),
		Status:          intPtr(9),
		ExpectedVersion: intPtr(0),
	})
	assertBusiness(t, err, 1, MessageModuleStatusInvalid)
}

func TestPublishFrontendModuleDisablesSiblings(t *testing.T) {
	env := newTestEnv(t)
	addModule(env, "m1", "cat-read", "内容运营", "1.0.0", domain.FrontendModuleStatusEnabled, 1, 0)
	addModule(env, "m2", "cat-read", "内容运营", "1.1.0", domain.FrontendModuleStatusDraft, 2, 0)

	if _, err := env.svc.PublishFrontendModule(env.ctx, "m2", domain.FrontendModulePublicationParam{
		ReleaseVersion:  strPtr("1.1.0"),
		ContractVersion: intPtr(1),
		ManifestPath:    strPtr("/admin-remotes/cat-read/1.1.0/mf-manifest.json"),
		Status:          intPtr(1),
		ExpectedVersion: intPtr(0),
	}); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	old, err := env.modules.FindModuleByID(env.ctx, "m1")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if old.Status != domain.FrontendModuleStatusDisabled {
		t.Fatalf("同模块其他启用版本应被停用: %+v", old)
	}
	if old.Version != 0 {
		t.Fatalf("兄弟行版本不应变化: %d", old.Version)
	}
}

func TestListFrontendModulesFiltersByCode(t *testing.T) {
	env := newTestEnv(t)
	addModule(env, "m1", "system", "系统管理", "1.0.0", domain.FrontendModuleStatusEnabled, 1, 0)
	addModule(env, "m2", "cat-read", "内容运营", "1.0.0", domain.FrontendModuleStatusEnabled, 2, 0)

	all, err := env.svc.ListFrontendModules(env.ctx, "")
	if err != nil || len(all) != 2 {
		t.Fatalf("全量查询异常: %v %v", all, err)
	}
	byCode, err := env.svc.ListFrontendModules(env.ctx, "cat-read")
	if err != nil || len(byCode) != 1 || byCode[0].ModuleCode != "cat-read" {
		t.Fatalf("按模块码查询异常: %v %v", byCode, err)
	}
}
