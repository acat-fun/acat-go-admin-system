package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
	"github.com/acat-fun/acat-go-admin-system/auth/repo"
	"github.com/acat-fun/acat-go-admin-system/auth/service"
	"github.com/acat-fun/acat-go-common/config"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/satoken"
)

const testTokenName = "acat-admin-token"

type stubWorkers struct {
	worker *domain.Worker
	perms  []string
	urls   []string
	roles  []string
}

func (s *stubWorkers) FindByUsername(_ context.Context, username string) (*domain.Worker, error) {
	if s.worker == nil || s.worker.Username != username {
		return nil, repo.ErrNotFound
	}
	return s.worker, nil
}

func (s *stubWorkers) FindByID(_ context.Context, id string) (*domain.Worker, error) {
	if s.worker == nil || s.worker.ID != id {
		return nil, repo.ErrNotFound
	}
	return s.worker, nil
}

func (s *stubWorkers) RoleCodes(context.Context, string) ([]string, error) { return s.roles, nil }
func (s *stubWorkers) URLAndButtonCodes(context.Context, string) ([]string, error) {
	return s.perms, nil
}
func (s *stubWorkers) URLCodes(context.Context, string) ([]string, error) { return s.urls, nil }
func (s *stubWorkers) AllPermissionCodes(context.Context) ([]string, error) {
	return s.perms, nil
}
func (s *stubWorkers) AllURLCodes(context.Context) ([]string, error) { return s.urls, nil }

// stubPages 是 PageRepo 的内存桩，可按测试注入页面行。
type stubPages struct {
	pages []domain.Page
}

func (s stubPages) ListByScope(context.Context, int, string) ([]domain.Page, error) {
	if s.pages == nil {
		return []domain.Page{}, nil
	}
	return s.pages, nil
}

// stubModules 是 FrontendModuleRepo 的内存桩，可按测试注入模块行。
type stubModules struct {
	modules []domain.FrontendModule
}

func (s stubModules) ListEnabledByCodes(context.Context, []string) ([]domain.FrontendModule, error) {
	if s.modules == nil {
		return []domain.FrontendModule{}, nil
	}
	return s.modules, nil
}

type fixture struct {
	handler http.Handler
	logic   *satoken.Logic
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureWith(t, stubPages{}, stubModules{})
}

func newFixtureWith(t *testing.T, pages stubPages, modules stubModules) *fixture {
	t.Helper()
	hashed, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成 bcrypt 失败: %v", err)
	}
	workers := &stubWorkers{
		worker: &domain.Worker{ID: "0", Username: "admin", Password: string(hashed), Status: 1},
		perms:  []string{"acat:admin:system:dicts"},
		urls:   []string{"acat:admin:system"},
		roles:  []string{"root"},
	}
	logic := satoken.NewLogic(satoken.Config{
		TokenName: testTokenName,
		Timeout:   3600,
		Now:       func() time.Time { return time.Unix(1730000000, 0) },
	}, satoken.NewMemoryStore(), nil)
	svc, err := service.New(service.Options{
		Tx:      passthroughTx{},
		Workers: workers, Pages: pages, Modules: modules, Satoken: logic,
	})
	if err != nil {
		t.Fatalf("构造 Service 失败: %v", err)
	}
	api, err := New(Options{
		Service: svc,
		Satoken: logic,
		Config:  config.SaTokenConfig{TokenName: testTokenName, CookieName: testTokenName, CookiePath: "/", CookieSameSite: "Lax"},
	})
	if err != nil {
		t.Fatalf("构造 API 失败: %v", err)
	}
	mux := http.NewServeMux()
	api.Register(mux)
	return &fixture{handler: mux, logic: logic}
}

func (f *fixture) login(t *testing.T) *http.Cookie {
	t.Helper()
	body := strings.NewReader(`{"username":"admin","password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, PathAuthLogin, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("登录状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == testTokenName {
			if !cookie.HttpOnly {
				t.Errorf("会话 Cookie 必须是 HttpOnly")
			}
			if cookie.SameSite != http.SameSiteLaxMode {
				t.Errorf("会话 Cookie SameSite = %v", cookie.SameSite)
			}
			return cookie
		}
	}
	t.Fatalf("登录未下发会话 Cookie: %v", rec.Result().Cookies())
	return nil
}

func TestLoginReturnsBootstrapAndCookie(t *testing.T) {
	f := newFixture(t)
	body := strings.NewReader(`{"username":"admin","password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, PathAuthLogin, body)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	var payload struct {
		Code    int              `json:"code"`
		Message string           `json:"message"`
		Data    domain.Bootstrap `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应失败: %v (%s)", err, rec.Body.String())
	}
	if payload.Code != 0 || payload.Message != "OK" {
		t.Errorf("响应包装 = %+v", payload)
	}
	if payload.Data.Session.AccountName != "admin" {
		t.Errorf("session.accountName = %s", payload.Data.Session.AccountName)
	}
	if len(payload.Data.Permissions) != 1 {
		t.Errorf("permissions = %v", payload.Data.Permissions)
	}
	// 数组字段必须是 [] 而不是 null。
	if !strings.Contains(rec.Body.String(), `"pages":[]`) {
		t.Errorf("pages 应为空数组: %s", rec.Body.String())
	}
}

func TestLoginWrongPasswordIsHTTP200WithBusinessCode(t *testing.T) {
	f := newFixture(t)
	body := strings.NewReader(`{"username":"admin","password":"nope"}`)
	req := httptest.NewRequest(http.MethodPost, PathAuthLogin, body)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("业务失败必须返回 HTTP 200，实际 %d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if payload["code"] != float64(1) || payload["message"] != "用户名或密码错误" || payload["data"] != nil || payload["success"] != false {
		t.Errorf("响应体 = %v", payload)
	}
}

// TestResultEnvelope 信封顶层键序与 success 取值：
// code→message→data→success，success 恒等于 code==0。
func TestResultEnvelope(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		name        string
		body        string
		wantCode    int
		wantSuccess bool
	}{
		{name: "登录成功", body: `{"username":"admin","password":"secret"}`, wantCode: 0, wantSuccess: true},
		{name: "密码错误", body: `{"username":"admin","password":"nope"}`, wantCode: 1, wantSuccess: false},
	}
	for _, testCase := range cases {
		req := httptest.NewRequest(http.MethodPost, PathAuthLogin, strings.NewReader(testCase.body))
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		raw := rec.Body.Bytes()
		var payload struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    any    `json:"data"`
			Success bool   `json:"success"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("%s 解析响应失败: %v (%s)", testCase.name, err, raw)
		}
		if payload.Code != testCase.wantCode || payload.Success != testCase.wantSuccess {
			t.Errorf("%s 信封 = code:%d success:%v, 期望 code:%d success:%v",
				testCase.name, payload.Code, payload.Success, testCase.wantCode, testCase.wantSuccess)
		}
		if got := strings.Join(topLevelKeys(t, raw), ","); got != "code,message,data,success" {
			t.Errorf("%s 顶层键序 = %s, 期望 code,message,data,success", testCase.name, got)
		}
	}
}

// topLevelKeys 按出现顺序返回顶层 JSON 对象的键。
func topLevelKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if _, err := decoder.Token(); err != nil { // 消费顶层 '{'
		t.Fatalf("解析响应失败: %v (%s)", err, raw)
	}
	keys := make([]string, 0, 4)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			t.Fatalf("读取顶层键失败: %v (%s)", err, raw)
		}
		key, ok := token.(string)
		if !ok {
			t.Fatalf("顶层键不是字符串: %v (%s)", token, raw)
		}
		keys = append(keys, key)
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			t.Fatalf("跳过键 %s 的值失败: %v (%s)", key, err, raw)
		}
	}
	return keys
}

func TestLoginMalformedBodyIsHTTP400(t *testing.T) {
	f := newFixture(t)
	req := httptest.NewRequest(http.MethodPost, PathAuthLogin, strings.NewReader(`{"username":`))
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, 期望 400", rec.Code)
	}
}

func TestProtectedRoutesRequireCookie(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{PathAuthUserInfo, PathAuthBootstrap, PathMyPermissions} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s 未登录状态码 = %d, 期望 401", path, rec.Code)
			continue
		}
		// 401 文案 逐字一致。
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s 解析响应失败: %v", path, err)
		}
		if payload["message"] != middleware.MessageNotLoggedIn || payload["success"] != false {
			t.Errorf("%s 401 响应体 = %v", path, payload)
		}
	}
	req := httptest.NewRequest(http.MethodPost, PathAuthLogout, nil)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("logout 未登录状态码 = %d, 期望 401", rec.Code)
	}
}

// TestUnregisteredAdminPathRequiresLogin 未注册的 /api/admin/** 路径：未登录 401、已登录 404。
// 拦截器覆盖整个前缀，
// 未注册路径不会先落到 404（Go ServeMux 默认行为）。
func TestUnregisteredAdminPathRequiresLogin(t *testing.T) {
	f := newFixture(t)
	const path = "/api/admin/system/not-registered"

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录访问未注册路径应 401，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应失败: %v (%s)", err, rec.Body.String())
	}
	if payload["code"] != float64(401) || payload["message"] != middleware.MessageNotLoggedIn {
		t.Errorf("未登录响应体 = %v", payload)
	}

	cookie := f.login(t)
	req = httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("登录后访问未注册路径应 404，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应失败: %v (%s)", err, rec.Body.String())
	}
	if payload["code"] != float64(404) {
		t.Errorf("登录后响应体 = %v", payload)
	}
}

func TestBootstrapAndUserInfoWithCookie(t *testing.T) {
	f := newFixture(t)
	cookie := f.login(t)

	for _, path := range []string{PathAuthBootstrap, PathAuthUserInfo} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s 状态码 = %d, body=%s", path, rec.Code, rec.Body.String())
		}
		var payload struct {
			Code int              `json:"code"`
			Data domain.Bootstrap `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s 解析响应失败: %v", path, err)
		}
		if payload.Code != 0 || payload.Data.Session.UserID != "0" {
			t.Errorf("%s 响应 = %+v", path, payload)
		}
	}
}

// TestBootstrapPageNodesAndNullFallbackManifestInJSON 锁定 HTTP 层契约：
// 页面节点含 createdAt/updatedAt；fallbackManifestUrl 的 NULL 输出 null 而不是 ""。
func TestBootstrapPageNodesAndNullFallbackManifestInJSON(t *testing.T) {
	created := time.Date(2026, 8, 1, 9, 0, 57, 0, time.UTC)
	updated := time.Date(2026, 8, 1, 9, 1, 57, 0, time.UTC)
	f := newFixtureWith(t,
		stubPages{pages: []domain.Page{
			{ID: "p1", Code: "acat:admin:system", Name: "系统管理", Type: 0, Scope: 0, IsEnabled: 1,
				FrontendModuleCode: "system", SortOrder: 1, CreatedAt: created, UpdatedAt: updated},
		}},
		stubModules{modules: []domain.FrontendModule{{
			ModuleCode: "system", ReleaseVersion: "1.0.0", ContractVersion: 1,
			ManifestPath: "/admin-remotes/system/1.0.0/mf-manifest.json", Status: 1,
		}}},
	)
	cookie := f.login(t)

	req := httptest.NewRequest(http.MethodGet, PathAuthBootstrap, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	for _, want := range []string{
		`"createdAt":"2026-08-01T09:00:57"`,
		`"updatedAt":"2026-08-01T09:01:57"`,
		`"fallbackManifestUrl":null`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("bootstrap 响应缺少 %s: %s", want, body)
		}
	}
	if strings.Contains(body, `"fallbackManifestUrl":""`) {
		t.Errorf("fallbackManifestUrl 不应输出空串: %s", body)
	}
}

func TestMyPermissionsWithCookie(t *testing.T) {
	f := newFixture(t)
	cookie := f.login(t)

	req := httptest.NewRequest(http.MethodGet, PathMyPermissions, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	var payload struct {
		Code int      `json:"code"`
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0] != "acat:admin:system:dicts" {
		t.Errorf("权限码 = %v", payload.Data)
	}
}

func TestLogoutClearsSessionAndCookie(t *testing.T) {
	f := newFixture(t)
	cookie := f.login(t)

	req := httptest.NewRequest(http.MethodPost, PathAuthLogout, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("登出状态码 = %d", rec.Code)
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == testTokenName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Errorf("登出应清空会话 Cookie")
	}

	// 复用旧 Cookie 访问受保护接口应 401。
	req2 := httptest.NewRequest(http.MethodGet, PathAuthBootstrap, nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	f.handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("登出后状态码 = %d, 期望 401", rec2.Code)
	}
}

func TestBearerHeaderAlsoWorks(t *testing.T) {
	f := newFixture(t)
	cookie := f.login(t)

	// 管理端为 Cookie 模式，但中间件同时兼容 satoken 头（读者端/联调场景）。
	req := httptest.NewRequest(http.MethodGet, PathMyPermissions, nil)
	req.Header.Set(middleware.TokenHeader, cookie.Value)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("satoken 头访问状态码 = %d", rec.Code)
	}
}
