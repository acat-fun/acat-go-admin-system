package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acat-fun/acat-go-admin-system/domain"
	"github.com/acat-fun/acat-go-admin-system/repo"
	"github.com/acat-fun/acat-go-admin-system/service"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/result"
	"github.com/acat-fun/acat-go-common/satoken"
)

// passthroughTx 让不关心事务的测试直接执行用例函数；真实提交/回滚由 tx_test.go 覆盖。
type passthroughTx struct{}

func (passthroughTx) Within(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// stubs 通过内嵌 repo 接口实现"只覆写被测方法"的极简桩：
// 未覆写的方法一旦被调用会 panic（nil 接口），从而暴露测试与实现的偏离。
type stubDictRepo struct {
	repo.DictRepo
	repo.LabelRepo
	frontendLabels []domain.FrontendLabelRecord
}

func (s *stubDictRepo) ListFrontendLabels(context.Context, string, string) ([]domain.FrontendLabelRecord, error) {
	return s.frontendLabels, nil
}

type stubModuleRepo struct {
	repo.FrontendModuleRepo
}

type stubPageRepo struct {
	repo.PageRepo
}

type stubTypeRepo struct {
	repo.I18nTypeRepo
	options []domain.I18nTypeRecord
}

func (s *stubTypeRepo) ListEnabledTypes(context.Context) ([]domain.I18nTypeRecord, error) {
	return s.options, nil
}

func (s *stubTypeRepo) ListTypesPaged(context.Context, string, *int, int, int) ([]domain.I18nTypeRecord, int64, error) {
	return s.options, int64(len(s.options)), nil
}

type stubFileRepo struct {
	repo.FileRepo
}

func (s *stubFileRepo) FindFileByID(context.Context, string) (*domain.FileRecord, error) {
	return nil, repo.ErrNotFound
}

func (s *stubFileRepo) ListFiles(context.Context, string, int, int) ([]domain.FileRecord, int64, error) {
	return []domain.FileRecord{}, 0, nil
}

// testServer 构造带真实路由与 Sa-Token 会话的测试服务。
type testServer struct {
	handler http.Handler
	logic   *satoken.Logic
	api     *API
	svc     *service.Service
	audits  *repo.MemoryAuditLogStore
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	// 默认用内存审计存储（真实实现见 repo.MongoAuditLogStore 与 mongo_integration 测试）。
	return newTestServerWithAuditStore(t, repo.NewMemoryAuditLogStore())
}

// login 写入真实 Sa-Token 会话并返回 token。
func (s *testServer) login(t *testing.T, loginID string, permissions []string) string {
	t.Helper()
	ctx := context.Background()
	token, err := s.logic.Login(ctx, loginID)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	session, err := s.logic.GetSession(ctx, loginID)
	if err != nil || session == nil {
		t.Fatalf("读取会话失败: %v", err)
	}
	session.Set(satoken.DataKeyPermissions, permissions)
	// root 登录 id 的会话补 root 角色（生产路径由 admin-user buildBootstrap 写入）。
	if loginID == domain.RootLoginID {
		session.Set(satoken.DataKeyRoles, []string{domain.RootRoleID})
	}
	if err := s.logic.SaveSession(ctx, session); err != nil {
		t.Fatalf("保存会话失败: %v", err)
	}
	return token
}

func (s *testServer) do(t *testing.T, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(satokenHeader, token)
	}
	recorder := httptest.NewRecorder()
	s.handler.ServeHTTP(recorder, req)
	return recorder
}

const satokenHeader = "satoken"

// decodeResult 解析统一 Result 结构。
func decodeResult(t *testing.T, recorder *httptest.ResponseRecorder) (int, string, json.RawMessage) {
	t.Helper()
	var payload struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("响应不是合法 Result JSON: %s (%v)", recorder.Body.String(), err)
	}
	return payload.Code, payload.Message, payload.Data
}

func TestUnauthenticatedReturns401(t *testing.T) {
	server := newTestServer(t)
	protected := []struct{ method, path string }{
		{http.MethodGet, PathDicts},
		{http.MethodPost, PathDicts},
		{http.MethodGet, PathI18nTypes},
		{http.MethodGet, PathI18nFrontendLabels},
		{http.MethodGet, PathPages},
		{http.MethodGet, PathPageModuleCodes},
		{http.MethodGet, PathFrontendModules},
		{http.MethodGet, PathAuditLogs},
		{http.MethodDelete, PathAuditLogs},
		{http.MethodGet, PathFiles},
		{http.MethodGet, "/api/admin/system/files/abc"},
		{http.MethodGet, "/api/admin/system/files/s/abc"},
	}
	for _, item := range protected {
		recorder := server.do(t, item.method, item.path, "", nil)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s 未登录应 401，实际 %d", item.method, item.path, recorder.Code)
			continue
		}
		code, message, _ := decodeResult(t, recorder)
		// 401 文案 逐字一致
		// （公共库 middleware.MessageNotLoggedIn）。
		if code != 401 || message != middleware.MessageNotLoggedIn {
			t.Errorf("%s %s 401 响应体异常: code=%d message=%q", item.method, item.path, code, message)
		}
	}
}

// TestUnregisteredAdminPathRequiresLogin 未注册的 /api/admin/** 路径：未登录 401、已登录 404。
// 拦截器覆盖整个前缀，
// 未注册路径不会先落到 404。
func TestUnregisteredAdminPathRequiresLogin(t *testing.T) {
	server := newTestServer(t)
	const path = "/api/admin/system/not-registered"

	recorder := server.do(t, http.MethodGet, path, "", nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("未登录访问未注册路径应 401，实际 %d（body=%s）", recorder.Code, recorder.Body.String())
	}
	if code, message, _ := decodeResult(t, recorder); code != 401 || message != middleware.MessageNotLoggedIn {
		t.Errorf("未登录响应体 = code:%d message:%q", code, message)
	}

	token := server.login(t, "1", nil)
	recorder = server.do(t, http.MethodGet, path, token, nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("登录后访问未注册路径应 404，实际 %d（body=%s）", recorder.Code, recorder.Body.String())
	}
	if code, _, _ := decodeResult(t, recorder); code != 404 {
		t.Errorf("登录后响应体 code = %d, 期望 404", code)
	}
}

// TestUnregisteredPublicI18nPathSkipsAuth /api/admin/system/i18n/public/** 整段免登录
// ，未注册路径给 404 而不是 401。
func TestUnregisteredPublicI18nPathSkipsAuth(t *testing.T) {
	server := newTestServer(t)
	recorder := server.do(t, http.MethodGet, PathI18nPublicPrefix+"not-registered", "", nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("public 子树未注册路径应 404（免登录），实际 %d（body=%s）", recorder.Code, recorder.Body.String())
	}
}

// TestResultEnvelope 信封顶层键序与 success 取值：
// code→message→data→success，success 恒等于 code==0。
func TestResultEnvelope(t *testing.T) {
	server := newTestServer(t)
	cases := []struct {
		name        string
		path        string
		wantSuccess bool
	}{
		{name: "成功（免登录 public 接口）", path: PathI18nPublicTypes, wantSuccess: true},
		{name: "失败（未登录 401）", path: PathDicts, wantSuccess: false},
	}
	for _, testCase := range cases {
		recorder := server.do(t, http.MethodGet, testCase.path, "", nil)
		raw := recorder.Body.Bytes()
		var payload struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    any    `json:"data"`
			Success bool   `json:"success"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("%s 解析响应失败: %v (%s)", testCase.name, err, raw)
		}
		if payload.Success != testCase.wantSuccess || payload.Success != (payload.Code == 0) {
			t.Errorf("%s success = %v（code=%d）, 期望 success=%v 且恒等于 code==0",
				testCase.name, payload.Success, payload.Code, testCase.wantSuccess)
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

func TestPublicI18nRoutesSkipAuth(t *testing.T) {
	server := newTestServer(t)

	// /i18n/public/types：免登录，返回 SelectVO 列表。
	recorder := server.do(t, http.MethodGet, PathI18nPublicTypes, "", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("公开接口应 200，实际 %d", recorder.Code)
	}
	code, _, data := decodeResult(t, recorder)
	if code != 0 {
		t.Fatalf("公开接口应 code=0，实际 %d", code)
	}
	var options []domain.SelectVO
	if err := json.Unmarshal(data, &options); err != nil || len(options) != 1 || options[0].Value != "zh-CN" {
		t.Fatalf("下拉数据异常: %s (%v)", data, err)
	}

	// /i18n/public/frontend-labels：免登录，返回有序 Map（含历史别名）。
	recorder = server.do(t, http.MethodGet, PathI18nPublicLabels, "", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("公开接口应 200，实际 %d", recorder.Code)
	}
	if _, _, data := decodeResult(t, recorder); !strings.Contains(string(data), "file.businessType") {
		t.Fatalf("前端文案应包含历史别名: %s", data)
	}

	// 同域的受保护 i18n 接口仍需登录。
	if recorder := server.do(t, http.MethodGet, PathI18nTypes, "", nil); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("受保护 i18n 接口应 401，实际 %d", recorder.Code)
	}
	if recorder := server.do(t, http.MethodGet, PathI18nFrontendLabels, "", nil); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("受保护 frontend-labels 应 401，实际 %d", recorder.Code)
	}
}

func TestPermissionDeniedReturns403(t *testing.T) {
	server := newTestServer(t)
	token := server.login(t, "u-viewer", []string{"acat:read:admin:dashboard"})

	recorder := server.do(t, http.MethodGet, PathDicts, token, nil)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("无权限应 403，实际 %d", recorder.Code)
	}
	code, message, _ := decodeResult(t, recorder)
	if code != 403 || message != "无操作权限" {
		t.Fatalf("403 响应体异常: code=%d message=%q", code, message)
	}
}

func TestRootBypassesPermissionChecks(t *testing.T) {
	server := newTestServer(t)
	// loginID=="0" 即 root，即使会话没有任何权限码也应放行。
	token := server.login(t, domain.RootLoginID, nil)
	recorder := server.do(t, http.MethodGet, PathFiles, token, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("root 应放行，实际 %d (%s)", recorder.Code, recorder.Body.String())
	}
}

func TestFrontendModulesRequiresClassAndMethodPermission(t *testing.T) {
	server := newTestServer(t)

	// 只持有类级页面码 → 写接口仍应 403（Sa-Token 先判类再判方法，AND 语义）。
	classOnly := server.login(t, "u-class-only", []string{"acat:admin:system:frontend-modules"})
	recorder := server.do(t, http.MethodPost, PathFrontendModules, classOnly,
		[]byte(`{"moduleCode":"content","name":"内容","releaseVersion":"1.0.0","contractVersion":1,"manifestPath":"/admin-remotes/content/1.0.0/mf-manifest.json","sortOrder":1}`))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("缺少方法级权限应 403，实际 %d (%s)", recorder.Code, recorder.Body.String())
	}

	// 只持有方法码 → 也应 403。
	methodOnly := server.login(t, "u-method-only", []string{"acat:admin:system:frontend-modules:create"})
	recorder = server.do(t, http.MethodPost, PathFrontendModules, methodOnly,
		[]byte(`{"moduleCode":"content","name":"内容","releaseVersion":"1.0.0","contractVersion":1,"manifestPath":"/admin-remotes/content/1.0.0/mf-manifest.json","sortOrder":1}`))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("缺少类级权限应 403，实际 %d (%s)", recorder.Code, recorder.Body.String())
	}
}

func TestMalformedBodyReturns400(t *testing.T) {
	server := newTestServer(t)
	token := server.login(t, domain.RootLoginID, nil)

	recorder := server.do(t, http.MethodPost, PathDicts, token, []byte(`{not json`))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("请求体非法应 400，实际 %d", recorder.Code)
	}
	code, message, _ := decodeResult(t, recorder)
	if code != 400 || message != "请求格式错误" {
		t.Fatalf("400 响应体异常: code=%d message=%q", code, message)
	}
}

func TestFrontendModuleValidationReturns422(t *testing.T) {
	server := newTestServer(t)
	token := server.login(t, domain.RootLoginID, nil)

	recorder := server.do(t, http.MethodPost, PathFrontendModules, token,
		[]byte(`{"moduleCode":"Bad Code","name":"","releaseVersion":"1.0.0","contractVersion":2,"manifestPath":"","sortOrder":1}`))
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("参数校验失败应 422，实际 %d (%s)", recorder.Code, recorder.Body.String())
	}
	code, message, _ := decodeResult(t, recorder)
	if code != 422 {
		t.Fatalf("业务码应为 422，实际 %d", code)
	}
	for _, want := range []string{"moduleCode: must match", "name: must not be blank", "contractVersion: must be less than or equal to 1", "manifestPath: must not be blank"} {
		if !strings.Contains(message, want) {
			t.Errorf("校验消息缺少 %q：%s", want, message)
		}
	}
}

func TestPublishValidationRequiresExpectedVersion(t *testing.T) {
	server := newTestServer(t)
	token := server.login(t, domain.RootLoginID, nil)

	recorder := server.do(t, http.MethodPut, PathFrontendModules+"/m1/publication", token,
		[]byte(`{"releaseVersion":"1.0.0","contractVersion":1,"manifestPath":"/admin-remotes/content/1.0.0/mf-manifest.json","status":1}`))
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("缺少 expectedVersion 应 422，实际 %d (%s)", recorder.Code, recorder.Body.String())
	}
	_, message, _ := decodeResult(t, recorder)
	if !strings.Contains(message, "expectedVersion: must not be null") {
		t.Fatalf("校验消息异常: %s", message)
	}
}

func TestFileStream404UsesMsgField(t *testing.T) {
	server := newTestServer(t)
	token := server.login(t, domain.RootLoginID, nil)

	recorder := server.do(t, http.MethodGet, "/api/admin/system/files/not-exist", token, nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("文件不存在应 404，实际 %d", recorder.Code)
	}
	body := strings.TrimSpace(recorder.Body.String())
	if body != `{"code":404,"msg":"文件不存在"}` {
		t.Fatalf("404 响应体应为 手写格式，实际 %s", body)
	}
	if strings.Contains(body, "message") {
		t.Fatalf("404 不应出现 message 字段: %s", body)
	}
}

func TestServeFile404HasEmptyBody(t *testing.T) {
	server := newTestServer(t)
	token := server.login(t, domain.RootLoginID, nil)

	recorder := server.do(t, http.MethodGet, "/api/admin/system/files/s/not-exist", token, nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("应 404，实际 %d", recorder.Code)
	}
	if body := recorder.Body.String(); body != "" {
		t.Fatalf("serve 的 404 为空 body，实际 %q", body)
	}
}

func TestAuditLogListDefaultPageSize20(t *testing.T) {
	server := newTestServer(t)
	token := server.login(t, domain.RootLoginID, nil)

	recorder := server.do(t, http.MethodGet, PathAuditLogs, token, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d (%s)", recorder.Code, recorder.Body.String())
	}
	_, _, data := decodeResult(t, recorder)
	var page result.PageData[domain.AuditLog]
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatalf("分页结构异常: %s (%v)", data, err)
	}
	if page.PageSize != service.DefaultAuditPageSize {
		t.Fatalf("审计日志默认 pageSize 应为 20，实际 %d", page.PageSize)
	}
}

func TestAuditLogCleanWithoutDaysFails(t *testing.T) {
	server := newTestServer(t)
	token := server.login(t, domain.RootLoginID, nil)

	recorder := server.do(t, http.MethodDelete, PathAuditLogs, token, nil)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("缺少 days 应 500，实际 %d", recorder.Code)
	}
}

func TestAuditLogCleanReturnsRemaining(t *testing.T) {
	server := newTestServer(t)
	token := server.login(t, domain.RootLoginID, nil)

	created := "2026-09-01T10:00:00"
	if err := server.audits.Insert(context.Background(), domain.AuditLog{Type: "LOGIN", CreatedAt: &created}); err != nil {
		t.Fatalf("写入审计日志失败: %v", err)
	}
	recorder := server.do(t, http.MethodDelete, PathAuditLogs+"?days=1", token, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d (%s)", recorder.Code, recorder.Body.String())
	}
	code, _, data := decodeResult(t, recorder)
	// 旧行（2026-09-01）被删除；本次 DELETE 自身也会被审计（写操作），其 createdAt=now 不在删除范围内，
	// 因此「删除后剩余总数」= 1。
	if code != 0 || strings.TrimSpace(string(data)) != "1" {
		t.Fatalf("clean 应返回删除后剩余总数 1，实际 code=%d data=%s", code, data)
	}
	page, err := server.audits.List(context.Background(), domain.AuditLogQuery{PageIndex: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != 1 || page.List[0].Type != domain.AuditLogTypeDelete {
		t.Fatalf("剩余记录应为本次清理写下的 DELETE 审计行: %+v", page.List)
	}
	if detail := page.List[0].Detail; detail == nil || *detail != "AuditLogAdminController.clean" {
		t.Fatalf("审计行 detail 应为 AuditLogAdminController.clean: %+v", page.List[0])
	}
}

func TestMineTrueSkipsPermissionCheck(t *testing.T) {
	server := newTestServer(t)
	// 普通账号：mine=true 只需要登录（无权限码），会走 stubPageRepo（未覆写方法会 panic），
	// 因此这里只用无权限的会话断言"未因权限被拒"——返回 403 之外的错误都说明未走权限分支。
	token := server.login(t, "u-no-perm", nil)

	recorder := server.do(t, http.MethodGet, PathPages+"?mine=true", token, nil)
	if recorder.Code == http.StatusForbidden {
		t.Fatalf("mine=true 不应做权限码校验")
	}

	// 普通 GET /pages 需要 permissions 或 pages 权限。
	recorder = server.do(t, http.MethodGet, PathPages, token, nil)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("普通 GET /pages 无权限应 403，实际 %d", recorder.Code)
	}
}

func TestAPIInterfaceContract(t *testing.T) {
	server := newTestServer(t)
	if server.api.Name() != "admin-system" {
		t.Fatalf("服务名 = %q", server.api.Name())
	}
	if server.api.Logger() == nil {
		t.Fatal("Logger 不应为 nil")
	}
	if err := server.api.Close(); err != nil {
		t.Fatalf("Close 应为 nil: %v", err)
	}
	if server.api.Health() != nil {
		t.Fatal("未注入 ReadyCheck 时 Health 应为 nil")
	}
}

func TestHealthProbe(t *testing.T) {
	server := newTestServer(t)
	checked := false
	api, err := New(Options{
		Service: server.svc,
		Satoken: server.logic,
		ReadyCheck: func() error {
			checked = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	checks := api.Health()
	if len(checks) != 1 {
		t.Fatalf("应有一个依赖检查: %v", checks)
	}
	for _, check := range checks {
		if err := check(context.Background()); err != nil {
			t.Fatalf("依赖检查失败: %v", err)
		}
	}
	if !checked {
		t.Fatal("ReadyCheck 未被调用")
	}
}

func TestQueryHelpers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?pageIndex=3&pageSize=5&all=true&scope=0&name=", nil)
	if index, size := queryPage(req); index != 3 || size != 5 {
		t.Fatalf("queryPage = %d/%d", index, size)
	}
	if !queryBool(req, "all") {
		t.Fatal("all 应解析为 true")
	}
	if scope := queryIntPtr(req, "scope"); scope == nil || *scope != 0 {
		t.Fatalf("scope = %v", scope)
	}
	if !hasQuery(req, "name") || queryString(req, "name") != "" {
		t.Fatalf("name 应存在且为空串")
	}
	if index, size := queryPageWith(req, service.DefaultAuditPageSize); index != 3 || size != 5 {
		t.Fatalf("queryPageWith = %d/%d", index, size)
	}
	// 非法值回落默认。
	bad := httptest.NewRequest(http.MethodGet, "/x?pageIndex=abc", nil)
	if index, _ := queryPage(bad); index != result.DefaultPageIndex {
		t.Fatalf("非法页码应回落默认: %d", index)
	}
}

func TestRequestContextReadsLanguageHeader(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Accept-Language", "en")
	rc := server.api.requestContext(req)
	if rc.Language() != "en" {
		t.Fatalf("语言 = %q", rc.Language())
	}
	if rc.IsRoot() {
		t.Fatal("无会话不应判为 root")
	}
}
