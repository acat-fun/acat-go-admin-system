package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
	"github.com/acat-fun/acat-go-admin-system/auth/repo"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/satoken"
)

// fakeWorkerRepo 是 WorkerRepo 的内存实现。
type fakeWorkerRepo struct {
	byUsername map[string]*domain.Worker
	byID       map[string]*domain.Worker
	roles      map[string][]string
	urlCodes   map[string][]string
	permCodes  map[string][]string
	allPerms   []string
	allURLs    []string
	failAll    error
}

func newFakeWorkerRepo() *fakeWorkerRepo {
	return &fakeWorkerRepo{
		byUsername: map[string]*domain.Worker{},
		byID:       map[string]*domain.Worker{},
		roles:      map[string][]string{},
		urlCodes:   map[string][]string{},
		permCodes:  map[string][]string{},
	}
}

func (f *fakeWorkerRepo) FindByUsername(_ context.Context, username string) (*domain.Worker, error) {
	worker, ok := f.byUsername[username]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return worker, nil
}

func (f *fakeWorkerRepo) FindByID(_ context.Context, id string) (*domain.Worker, error) {
	worker, ok := f.byID[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return worker, nil
}

func (f *fakeWorkerRepo) RoleCodes(_ context.Context, userID string) ([]string, error) {
	return f.roles[userID], nil
}

func (f *fakeWorkerRepo) URLAndButtonCodes(_ context.Context, userID string) ([]string, error) {
	return f.permCodes[userID], nil
}

func (f *fakeWorkerRepo) URLCodes(_ context.Context, userID string) ([]string, error) {
	return f.urlCodes[userID], nil
}

func (f *fakeWorkerRepo) AllPermissionCodes(context.Context) ([]string, error) {
	if f.failAll != nil {
		return nil, f.failAll
	}
	return f.allPerms, nil
}

func (f *fakeWorkerRepo) AllURLCodes(context.Context) ([]string, error) {
	if f.failAll != nil {
		return nil, f.failAll
	}
	return f.allURLs, nil
}

// fakePageRepo 是 PageRepo 的内存实现。
type fakePageRepo struct {
	pages []domain.Page
	err   error
}

func (f *fakePageRepo) ListByScope(context.Context, int, string) ([]domain.Page, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.pages, nil
}

// fakeModuleRepo 是 FrontendModuleRepo 的内存实现。
type fakeModuleRepo struct {
	modules []domain.FrontendModule
	calls   [][]string
}

func (f *fakeModuleRepo) ListEnabledByCodes(_ context.Context, codes []string) ([]domain.FrontendModule, error) {
	f.calls = append(f.calls, codes)
	if len(codes) == 0 {
		return []domain.FrontendModule{}, nil
	}
	wanted := map[string]struct{}{}
	for _, code := range codes {
		wanted[code] = struct{}{}
	}
	out := make([]domain.FrontendModule, 0, len(f.modules))
	for _, module := range f.modules {
		if _, ok := wanted[module.ModuleCode]; ok {
			out = append(out, module)
		}
	}
	return out, nil
}

type fixture struct {
	svc     *Service
	workers *fakeWorkerRepo
	pages   *fakePageRepo
	modules *fakeModuleRepo
	logic   *satoken.Logic
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	workers := newFakeWorkerRepo()
	pages := &fakePageRepo{}
	modules := &fakeModuleRepo{}
	logic := satoken.NewLogic(satoken.Config{
		TokenName: DefaultTokenNameForTest,
		Timeout:   3600,
		Now:       func() time.Time { return time.Unix(1730000000, 0) },
	}, satoken.NewMemoryStore(), nil)
	svc, err := New(Options{Workers: workers, Pages: pages, Modules: modules, Satoken: logic, Tx: passthroughTx{}})
	if err != nil {
		t.Fatalf("构造 Service 失败: %v", err)
	}
	return &fixture{svc: svc, workers: workers, pages: pages, modules: modules, logic: logic}
}

const DefaultTokenNameForTest = "acat-admin-token"

func hashPassword(t *testing.T, raw string) string {
	t.Helper()
	hashed, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成 bcrypt 失败: %v", err)
	}
	return string(hashed)
}

func TestLoginSuccessWritesSessionAndCookieData(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	root := &domain.Worker{ID: domain.RootLoginID, Username: "admin", Password: hashPassword(t, "admin-pass"), Status: 1}
	f.workers.byUsername["admin"] = root
	f.workers.byID[domain.RootLoginID] = root

	f.workers.roles[domain.RootLoginID] = []string{domain.RootRoleID}
	f.workers.allPerms = []string{"acat:admin:system:dicts", "acat:admin:system:users:workers"}
	f.workers.allURLs = []string{"acat:admin:system", "acat:admin:system:dicts"}
	f.pages.pages = []domain.Page{
		{ID: "p1", Code: "acat:admin:system", Name: "系统管理", Type: 0, SortOrder: 1, Scope: 0, IsEnabled: 1, FrontendModuleCode: "system"},
		{ID: "p2", Code: "acat:admin:system:dicts", Name: "字典管理", Type: 2, ParentID: "p1", SortOrder: 2, Scope: 0, IsEnabled: 1, FrontendModuleCode: "system"},
		{ID: "p3", Code: "acat:admin:system:disabled", Name: "停用页面", Type: 2, ParentID: "p1", SortOrder: 3, Scope: 0, IsEnabled: 0, FrontendModuleCode: "system"},
	}
	f.modules.modules = []domain.FrontendModule{{
		ModuleCode: "system", Name: "系统管理", ReleaseVersion: "1.0.0", ContractVersion: 1,
		ManifestPath: "/modules/system/manifest.json", Status: 1,
	}}

	result, err := f.svc.Login(ctx, "admin", "admin-pass")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if result.Token == "" {
		t.Fatalf("token 为空")
	}

	// token -> loginId 映射写入共享 Redis。
	tokenKey := f.logic.TokenKey(result.Token)
	store := f.logic.Store()
	value, err := store.Get(ctx, tokenKey)
	if err != nil {
		t.Fatalf("token 映射未写入: %v", err)
	}
	if value != domain.RootLoginID {
		t.Errorf("token 映射 value = %q, 期望 %q", value, domain.RootLoginID)
	}

	// 会话快照：permissions/roles/username 三键。
	session, err := f.logic.GetSession(ctx, domain.RootLoginID)
	if err != nil || session == nil {
		t.Fatalf("会话未写入: %v", err)
	}
	if got := session.StringList(satoken.DataKeyPermissions); len(got) != 2 {
		t.Errorf("permissions 快照 = %v", got)
	}
	if got := session.String(satoken.DataKeyUsername); got != "admin" {
		t.Errorf("username 快照 = %q", got)
	}

	// 响应结构：停用页面被过滤，树按 parentId 组装。
	bootstrap := result.Bootstrap
	if len(bootstrap.Pages) != 1 {
		t.Fatalf("页面树根节点数 = %d, 期望 1", len(bootstrap.Pages))
	}
	rootNode := bootstrap.Pages[0]
	if rootNode.Code != "acat:admin:system" || len(rootNode.Children) != 1 {
		t.Errorf("页面树结构异常: %+v", rootNode)
	}
	if rootNode.Children[0].Code != "acat:admin:system:dicts" {
		t.Errorf("子节点 = %s", rootNode.Children[0].Code)
	}
	if rootNode.Children[0].Children != nil {
		t.Errorf("type=2 页面不应挂载 children: %+v", rootNode.Children[0].Children)
	}
	// urls 只保留可见页面的 code。
	if len(bootstrap.URLs) != 2 {
		t.Errorf("urls = %v", bootstrap.URLs)
	}
	if len(bootstrap.FrontendModules) != 1 || bootstrap.FrontendModules[0].ModuleCode != "system" {
		t.Errorf("frontendModules = %+v", bootstrap.FrontendModules)
	}
	if bootstrap.Session.UserID != domain.RootLoginID || bootstrap.Session.Username != "admin" {
		t.Errorf("session = %+v", bootstrap.Session)
	}
	if bootstrap.Session.Email != nil || bootstrap.Session.Bio != nil {
		t.Errorf("空值字段应为 null: %+v", bootstrap.Session)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	f := newFixture(t)
	worker := &domain.Worker{ID: "u1", Username: "someone", Password: hashPassword(t, "right"), Status: 1}
	f.workers.byUsername["someone"] = worker
	f.workers.byID["u1"] = worker

	_, err := f.svc.Login(context.Background(), "someone", "wrong")
	business, ok := apperr.IsBusiness(err)
	if !ok {
		t.Fatalf("应为业务失败，实际 %v", err)
	}
	if business.Message != MessageInvalidCredential || business.Code != 1 {
		t.Errorf("业务错误 = %+v", business)
	}
}

func TestLoginRejectsDisabledAndUnknownUser(t *testing.T) {
	f := newFixture(t)
	disabled := &domain.Worker{ID: "u2", Username: "blocked", Password: hashPassword(t, "pw"), Status: 0}
	f.workers.byUsername["blocked"] = disabled
	f.workers.byID["u2"] = disabled

	for _, tc := range []struct{ name, user, pass string }{
		{"禁用账号", "blocked", "pw"},
		{"不存在账号", "ghost", "pw"},
		{"空用户名", "", "pw"},
		{"空密码", "blocked", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.svc.Login(context.Background(), tc.user, tc.pass)
			business, ok := apperr.IsBusiness(err)
			if !ok || business.Message != MessageInvalidCredential {
				t.Fatalf("应返回统一提示，实际 %v", err)
			}
		})
	}
}

func TestNonRootUserSeesOnlyGrantedPagesWithAncestors(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	worker := &domain.Worker{ID: "u3", Username: "operator", Password: hashPassword(t, "pw"), Status: 1}
	f.workers.byUsername["operator"] = worker
	f.workers.byID["u3"] = worker
	f.workers.roles["u3"] = []string{"system-admin"}
	f.workers.urlCodes["u3"] = []string{"acat:admin:system:dicts"}
	f.workers.permCodes["u3"] = []string{"acat:admin:system:dicts", "acat:admin:system:dicts:create"}
	f.pages.pages = []domain.Page{
		{ID: "root1", Code: "acat:admin:system", Name: "系统管理", Type: 0, SortOrder: 1, Scope: 0, IsEnabled: 1, FrontendModuleCode: "system"},
		{ID: "group1", Code: "acat:admin:system:base", Name: "基础配置", Type: 1, ParentID: "root1", SortOrder: 1, Scope: 0, IsEnabled: 1, FrontendModuleCode: "system"},
		{ID: "dict", Code: "acat:admin:system:dicts", Name: "字典管理", Type: 2, ParentID: "group1", SortOrder: 1, Scope: 0, IsEnabled: 1, FrontendModuleCode: "system", RouteKey: "system.config.dicts"},
		{ID: "other", Code: "acat:admin:system:files", Name: "文件管理", Type: 2, ParentID: "root1", SortOrder: 2, Scope: 0, IsEnabled: 1, FrontendModuleCode: "system"},
	}
	f.modules.modules = []domain.FrontendModule{{ModuleCode: "system", ReleaseVersion: "1.0.0", ContractVersion: 1, ManifestPath: "/m.json", Status: 1}}

	result, err := f.svc.Login(ctx, "operator", "pw")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if len(result.Bootstrap.Pages) != 1 {
		t.Fatalf("根节点数 = %d", len(result.Bootstrap.Pages))
	}
	rootNode := result.Bootstrap.Pages[0]
	if rootNode.Code != "acat:admin:system" || len(rootNode.Children) != 1 {
		t.Fatalf("祖先节点未补全: %+v", rootNode)
	}
	group := rootNode.Children[0]
	if group.Code != "acat:admin:system:base" || len(group.Children) != 1 {
		t.Fatalf("分组结构异常: %+v", group)
	}
	if group.Children[0].Code != "acat:admin:system:dicts" {
		t.Errorf("叶子节点 = %s", group.Children[0].Code)
	}
	if got := group.Children[0].RouteKey; got == nil || *got != "system.config.dicts" {
		t.Errorf("routeKey = %v", got)
	}
	// permissions 保留按钮权限（不过滤），urls 只保留页面权限。
	if len(result.Bootstrap.Permissions) != 2 {
		t.Errorf("permissions = %v", result.Bootstrap.Permissions)
	}
	if len(result.Bootstrap.URLs) != 1 || result.Bootstrap.URLs[0] != "acat:admin:system:dicts" {
		t.Errorf("urls = %v", result.Bootstrap.URLs)
	}
}

func TestPagesFilteredByDisabledFrontendModule(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	worker := &domain.Worker{ID: domain.RootLoginID, Username: "admin", Password: hashPassword(t, "pw"), Status: 1}
	f.workers.byUsername["admin"] = worker
	f.workers.byID[domain.RootLoginID] = worker

	f.workers.roles[domain.RootLoginID] = []string{domain.RootRoleID}
	f.workers.allURLs = []string{"acat:admin:system", "acat:read:admin:cat-read", "acat:read:admin:dashboard"}
	f.workers.allPerms = []string{"acat:admin:system"}
	f.pages.pages = []domain.Page{
		{ID: "sys", Code: "acat:admin:system", Name: "系统管理", Type: 0, Scope: 0, IsEnabled: 1, FrontendModuleCode: "system", SortOrder: 1},
		{ID: "content", Code: "acat:read:admin:cat-read", Name: "内容运营", Type: 0, Scope: 0, IsEnabled: 1, FrontendModuleCode: "content", SortOrder: 2},
		{ID: "shell", Code: "acat:read:admin:dashboard", Name: "仪表盘", Type: 0, Scope: 0, IsEnabled: 1, FrontendModuleCode: "shell", SortOrder: 0},
	}
	// 仅 system 模块启用，content 未启用。
	f.modules.modules = []domain.FrontendModule{{ModuleCode: "system", ReleaseVersion: "1.0.0", ContractVersion: 1, ManifestPath: "/s.json", Status: 1}}

	result, err := f.svc.Login(ctx, "admin", "pw")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if len(result.Bootstrap.Pages) != 2 {
		t.Fatalf("可见页面数 = %d, 期望 2（shell + system）", len(result.Bootstrap.Pages))
	}
	codes := []string{result.Bootstrap.Pages[0].Code, result.Bootstrap.Pages[1].Code}
	want := map[string]bool{"acat:read:admin:dashboard": true, "acat:admin:system": true}
	for _, code := range codes {
		if !want[code] {
			t.Errorf("出现未启用模块的页面: %v", codes)
		}
	}
	if len(result.Bootstrap.URLs) != 2 {
		t.Errorf("urls 未按可见页面过滤: %v", result.Bootstrap.URLs)
	}
}

func TestPermissionsReadsSessionForNonRoot(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.logic.Login(ctx, "u9"); err != nil {
		t.Fatalf("预置会话失败: %v", err)
	}
	session, err := f.logic.GetSession(ctx, "u9")
	if err != nil || session == nil {
		t.Fatalf("读取会话失败: %v", err)
	}
	session.Set(satoken.DataKeyPermissions, []string{"acat:admin:system:files"})
	if err := f.logic.SaveSession(ctx, session); err != nil {
		t.Fatalf("保存会话失败: %v", err)
	}

	codes, err := f.svc.Permissions(ctx, "u9")
	if err != nil {
		t.Fatalf("读取权限失败: %v", err)
	}
	if len(codes) != 1 || codes[0] != "acat:admin:system:files" {
		t.Errorf("权限码 = %v", codes)
	}

	// root（会话含 root 角色）实时查库返回全量权限码。
	if _, err := f.logic.Login(ctx, domain.RootLoginID); err != nil {
		t.Fatalf("预置 root 会话失败: %v", err)
	}
	rootSession, err := f.logic.GetSession(ctx, domain.RootLoginID)
	if err != nil || rootSession == nil {
		t.Fatalf("读取 root 会话失败: %v", err)
	}
	rootSession.Set(satoken.DataKeyRoles, []string{domain.RootRoleID})
	if err := f.logic.SaveSession(ctx, rootSession); err != nil {
		t.Fatalf("保存 root 会话失败: %v", err)
	}
	f.workers.allPerms = []string{"acat:admin:system:dicts"}
	rootCodes, err := f.svc.Permissions(ctx, domain.RootLoginID)
	if err != nil {
		t.Fatalf("root 读取权限失败: %v", err)
	}
	if len(rootCodes) != 1 || rootCodes[0] != "acat:admin:system:dicts" {
		t.Errorf("root 权限码 = %v", rootCodes)
	}
}

func TestBootstrapUnknownUserReturnsBusinessError(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Bootstrap(context.Background(), "missing")
	business, ok := apperr.IsBusiness(err)
	if !ok || business.Message != MessageUserNotFound {
		t.Fatalf("应返回用户不存在业务错误，实际 %v", err)
	}
}

func TestLogoutRemovesSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	worker := &domain.Worker{ID: domain.RootLoginID, Username: "admin", Password: hashPassword(t, "pw"), Status: 1}
	f.workers.byUsername["admin"] = worker
	f.workers.byID[domain.RootLoginID] = worker

	f.workers.roles[domain.RootLoginID] = []string{domain.RootRoleID}

	result, err := f.svc.Login(ctx, "admin", "pw")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if err := f.svc.Logout(ctx, result.Token); err != nil {
		t.Fatalf("登出失败: %v", err)
	}
	if _, err := f.logic.CheckLogin(ctx, result.Token); err == nil {
		t.Errorf("登出后 token 应失效")
	}
}

func TestBuildPageTreeEmptyInput(t *testing.T) {
	tree := BuildPageTree(nil)
	if tree == nil || len(tree) != 0 {
		t.Errorf("空输入应返回空切片，实际 %v", tree)
	}
}

// TestBootstrapPageNodesCarryAuditTimestampsAndNullFallbackManifest 锁定双读比对发现的两处契约：
//  1. 每个页面节点含 createdAt/updatedAt（ISO "yyyy-MM-ddTHH:mm:ss"，无值输出 null）；
//  2. frontendModules[].fallbackManifestUrl 库中为 NULL 时输出 null（不是 ""）。
func TestBootstrapPageNodesCarryAuditTimestampsAndNullFallbackManifest(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	root := &domain.Worker{ID: domain.RootLoginID, Username: "admin", Password: hashPassword(t, "pw"), Status: 1}
	f.workers.byUsername["admin"] = root
	f.workers.byID[domain.RootLoginID] = root

	f.workers.roles[domain.RootLoginID] = []string{domain.RootRoleID}
	f.workers.allURLs = []string{"acat:admin:system", "acat:admin:system:dicts"}
	f.workers.allPerms = []string{"acat:admin:system"}
	created := time.Date(2026, 8, 1, 9, 0, 57, 0, time.UTC)
	updated := time.Date(2026, 8, 1, 9, 1, 57, 0, time.UTC)
	f.pages.pages = []domain.Page{
		{ID: "p1", Code: "acat:admin:system", Name: "系统管理", Type: 0, Scope: 0, IsEnabled: 1,
			FrontendModuleCode: "system", SortOrder: 1, CreatedAt: created, UpdatedAt: updated},
		// 子节点时间零值 → JSON null。
		{ID: "p2", Code: "acat:admin:system:dicts", Name: "字典管理", Type: 2, ParentID: "p1",
			Scope: 0, IsEnabled: 1, FrontendModuleCode: "shell", SortOrder: 2},
	}
	// fallback_manifest_path 为 NULL（指针 nil）。
	f.modules.modules = []domain.FrontendModule{{
		ModuleCode: "system", ReleaseVersion: "1.0.0", ContractVersion: 1,
		ManifestPath: "/admin-remotes/system/1.0.0/mf-manifest.json", Status: 1,
	}}

	result, err := f.svc.Login(ctx, "admin", "pw")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if len(result.Bootstrap.Pages) != 1 || len(result.Bootstrap.Pages[0].Children) != 1 {
		t.Fatalf("页面树结构异常: %+v", result.Bootstrap.Pages)
	}
	rootNode := result.Bootstrap.Pages[0]
	if rootNode.CreatedAt == nil || *rootNode.CreatedAt != "2026-08-01T09:00:57" {
		t.Errorf("根节点 createdAt = %v, 期望 2026-08-01T09:00:57", rootNode.CreatedAt)
	}
	if rootNode.UpdatedAt == nil || *rootNode.UpdatedAt != "2026-08-01T09:01:57" {
		t.Errorf("根节点 updatedAt = %v, 期望 2026-08-01T09:01:57", rootNode.UpdatedAt)
	}
	child := rootNode.Children[0]
	if child.CreatedAt != nil || child.UpdatedAt != nil {
		t.Errorf("零值时间应输出 null: createdAt=%v updatedAt=%v", child.CreatedAt, child.UpdatedAt)
	}

	raw, err := json.Marshal(result.Bootstrap)
	if err != nil {
		t.Fatalf("序列化 bootstrap 失败: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		`"createdAt":"2026-08-01T09:00:57"`,
		`"updatedAt":"2026-08-01T09:01:57"`,
		`"createdAt":null`,
		`"updatedAt":null`,
		`"fallbackManifestUrl":null`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("bootstrap JSON 缺少 %s: %s", want, body)
		}
	}
	if strings.Contains(body, `"fallbackManifestUrl":""`) {
		t.Errorf("fallbackManifestUrl 不应输出空串: %s", body)
	}
}
