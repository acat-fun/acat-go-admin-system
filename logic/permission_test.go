package logic

import (
	"context"
	"testing"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/satoken"
)

func newLogic() *satoken.Logic {
	return satoken.NewLogic(satoken.Config{
		TokenName:  "acat-admin-token",
		LoginType:  "login",
		Timeout:    satoken.DefaultTimeoutSeconds,
		IsShare:    true,
		TokenStyle: "uuid",
	}, satoken.NewMemoryStore(), nil)
}

// sessionFor 构造带权限快照的会话，并返回基于它的 Actor。
func sessionFor(loginID string, permissions ...string) *Actor {
	session := satoken.NewSession("session-" + loginID)
	session.LoginID = loginID
	session.Set(satoken.DataKeyPermissions, permissions)
	ctx := middleware.WithSession(context.Background(), session, "token")
	return ActorFrom(ctx, NewChecker(newLogic()))
}

// rootSessionFor 构造绑定 root 角色的超管会话 Actor（生产路径由登录服务写入 roles）。
func rootSessionFor(loginID string) *Actor {
	session := satoken.NewSession("session-root-" + loginID)
	session.LoginID = loginID
	session.Set(satoken.DataKeyRoles, []string{domain.RootRoleID})
	ctx := middleware.WithSession(context.Background(), session, "token")
	return ActorFrom(ctx, NewChecker(newLogic()))
}

func TestRootBypassesEveryPermission(t *testing.T) {
	actor := rootSessionFor(domain.RootLoginID)
	if !actor.IsRoot() {
		t.Fatal("会话角色含 root 应判为超管")
	}
	if err := actor.RequirePermission(SystemDictsCreate); err != nil {
		t.Fatalf("root 应放行: %v", err)
	}
	if err := actor.RequireAnyPermission(SystemPages, SystemPermissions); err != nil {
		t.Fatalf("root 的 OR 判定应放行: %v", err)
	}
}

func TestRootRoleAnyLoginIDBypasses(t *testing.T) {
	// 任意登录 id 只要绑定 root 角色即超管。
	actor := rootSessionFor("u-root-9")
	if !actor.IsRoot() {
		t.Fatal("任意登录 id 绑定 root 角色应判为超管")
	}
	if err := actor.RequirePermission(SystemDictsCreate); err != nil {
		t.Fatalf("root 角色应放行: %v", err)
	}
}

func TestLoginIDZeroWithoutRootRoleNotRoot(t *testing.T) {
	// 登录 id 为 "0" 但无 root 角色时不是超管（判定只看角色）。
	actor := sessionFor(domain.RootLoginID, SystemDicts)
	if actor.IsRoot() {
		t.Fatal("无 root 角色不应判为超管（即使登录 id 为 0）")
	}
	if err := actor.RequirePermission(SystemDicts); err != nil {
		t.Fatalf("持有权限码应放行: %v", err)
	}
	if err := actor.RequirePermission(SystemDictsCreate); err == nil {
		t.Fatal("未授予的权限码应拒绝")
	}
}

func TestNonRootNeedsMatchingPermission(t *testing.T) {
	actor := sessionFor("u1", SystemDicts)
	if actor.IsRoot() {
		t.Fatal("非 0 登录 id 不应判为 root")
	}
	if err := actor.RequirePermission(SystemDicts); err != nil {
		t.Fatalf("持有权限码应放行: %v", err)
	}
	err := actor.RequirePermission(SystemDictsCreate)
	if err == nil {
		t.Fatal("缺少权限码应拒绝")
	}
	appError, ok := apperr.As(err)
	if !ok || appError.HTTPStatus != 403 || appError.Message != MessageForbidden {
		t.Fatalf("应为 403 无操作权限，实际 %v", err)
	}
}

func TestRequireAnyPermissionOrSemantics(t *testing.T) {
	actor := sessionFor("u1", SystemPermissions)
	if err := actor.RequireAnyPermission(SystemPages, SystemPermissions); err != nil {
		t.Fatalf("OR 语义应放行: %v", err)
	}
	if err := actor.RequireAnyPermission(SystemPages, SystemPagesCreate); err == nil {
		t.Fatal("均不持有应拒绝")
	}
	// 空集合视为无需权限。
	if err := actor.RequireAnyPermission(); err != nil {
		t.Fatalf("空集合应放行: %v", err)
	}
}

func TestNilSessionIsForbiddenNotPanic(t *testing.T) {
	checker := NewChecker(nil)
	if err := checker.CheckPermission(nil, SystemDicts); err == nil {
		t.Fatal("空会话应拒绝")
	}
	actor := &Actor{}
	if err := actor.RequirePermission(SystemDicts); err == nil {
		t.Fatal("空 Actor 应拒绝")
	}
}

func TestLoginIDFormatting(t *testing.T) {
	session := satoken.NewSession("s")
	session.LoginID = 123
	if got := LoginID(session); got != "123" {
		t.Fatalf("非字符串 loginID 应格式化，实际 %q", got)
	}
	if got := LoginID(nil); got != "" {
		t.Fatalf("空会话应返回空串，实际 %q", got)
	}
}

func TestPermissionCodes(t *testing.T) {
	expected := map[string]string{
		"SystemDicts":                  "acat:admin:system:dicts",
		"SystemDictsCreate":            "acat:admin:system:dicts:create",
		"SystemDictsEdit":              "acat:admin:system:dicts:edit",
		"SystemDictsDelete":            "acat:admin:system:dicts:delete",
		"SystemI18nTypes":              "acat:admin:system:i18n-types",
		"SystemI18nTypesCreate":        "acat:admin:system:i18n-types:create",
		"SystemI18nTypesEdit":          "acat:admin:system:i18n-types:edit",
		"SystemI18nTypesDelete":        "acat:admin:system:i18n-types:delete",
		"SystemPages":                  "acat:admin:system:pages",
		"SystemPagesCreate":            "acat:admin:system:pages:create",
		"SystemPagesEdit":              "acat:admin:system:pages:edit",
		"SystemPagesDelete":            "acat:admin:system:pages:delete",
		"SystemPermissions":            "acat:admin:system:permissions",
		"SystemFrontendModules":        "acat:admin:system:frontend-modules",
		"SystemFrontendModulesCreate":  "acat:admin:system:frontend-modules:create",
		"SystemFrontendModulesEdit":    "acat:admin:system:frontend-modules:edit",
		"SystemFrontendModulesPublish": "acat:admin:system:frontend-modules:publish",
		"SystemAuditLogs":              "acat:admin:system:audit-logs",
		"SystemFiles":                  "acat:admin:system:files",
		"SystemFilesUpload":            "acat:admin:system:files:upload",
		"SystemFilesDelete":            "acat:admin:system:files:delete",
	}
	actual := map[string]string{
		"SystemDicts":                  SystemDicts,
		"SystemDictsCreate":            SystemDictsCreate,
		"SystemDictsEdit":              SystemDictsEdit,
		"SystemDictsDelete":            SystemDictsDelete,
		"SystemI18nTypes":              SystemI18nTypes,
		"SystemI18nTypesCreate":        SystemI18nTypesCreate,
		"SystemI18nTypesEdit":          SystemI18nTypesEdit,
		"SystemI18nTypesDelete":        SystemI18nTypesDelete,
		"SystemPages":                  SystemPages,
		"SystemPagesCreate":            SystemPagesCreate,
		"SystemPagesEdit":              SystemPagesEdit,
		"SystemPagesDelete":            SystemPagesDelete,
		"SystemPermissions":            SystemPermissions,
		"SystemFrontendModules":        SystemFrontendModules,
		"SystemFrontendModulesCreate":  SystemFrontendModulesCreate,
		"SystemFrontendModulesEdit":    SystemFrontendModulesEdit,
		"SystemFrontendModulesPublish": SystemFrontendModulesPublish,
		"SystemAuditLogs":              SystemAuditLogs,
		"SystemFiles":                  SystemFiles,
		"SystemFilesUpload":            SystemFilesUpload,
		"SystemFilesDelete":            SystemFilesDelete,
	}
	for name, want := range expected {
		if actual[name] != want {
			t.Errorf("%s = %q，期望 %q", name, actual[name], want)
		}
	}
}
