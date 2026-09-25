package audit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/repo"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/satoken"
)

// fixedNow 是测试注入的固定时间（含毫秒，覆盖 BSON Date 精度）。
var fixedNow = time.Date(2026, 9, 14, 1, 2, 3, 456_000_000, time.UTC)

// 与 httpapi 路由常量同形的模式（本包不依赖 httpapi，避免循环引用）。
const (
	PathDictsPattern    = "/api/admin/system/dicts/{id}"
	PathDictDataPattern = "/api/admin/system/dicts/{dictId}/data"
)

// auditEnv 是被测环境：真实 Sa-Token 会话 + 内存审计存储 + 中间件。
type auditEnv struct {
	recorder *Recorder
	store    *repo.MemoryAuditLogStore
	logic    *satoken.Logic
}

func newAuditEnv(t *testing.T, store repo.AuditLogStore) *auditEnv {
	t.Helper()
	if store == nil {
		store = repo.NewMemoryAuditLogStore()
	}
	logic := satoken.NewLogic(satoken.Config{
		TokenName:  "satoken",
		LoginType:  "login",
		Timeout:    satoken.DefaultTimeoutSeconds,
		IsShare:    true,
		TokenStyle: "uuid",
	}, satoken.NewMemoryStore(), nil)
	return &auditEnv{
		recorder: New(Options{
			Store:    store,
			Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
			Now:      func() time.Time { return fixedNow },
			Location: time.UTC,
		}),
		store: store.(*repo.MemoryAuditLogStore),
		logic: logic,
	}
}

// serve 在进程内跑完整链路（auth → audit → handler，与生产装配顺序一致）。
func (e *auditEnv) serve(t *testing.T, req *http.Request, token string, meta RouteMeta, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	if token != "" {
		req.Header.Set("satoken", token)
	}
	auth := middleware.Auth(middleware.AuthConfig{Logic: e.logic, CookieName: "satoken"})
	recorder := httptest.NewRecorder()
	auth(e.recorder.Wrap(handler, meta)).ServeHTTP(recorder, req)
	return recorder
}

// servePublic 模拟公开路由（无认证中间件。
func (e *auditEnv) servePublic(req *http.Request, meta RouteMeta, handler http.HandlerFunc) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	e.recorder.Wrap(handler, meta).ServeHTTP(recorder, req)
	return recorder
}

// login 建立会话并返回 token（username 写入会话。
func (e *auditEnv) login(t *testing.T, loginID, username string) string {
	t.Helper()
	ctx := context.Background()
	token, err := e.logic.Login(ctx, loginID)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	session, err := e.logic.GetSession(ctx, loginID)
	if err != nil || session == nil {
		t.Fatalf("读取会话失败: %v", err)
	}
	session.Set(satoken.DataKeyUsername, username)
	if err := e.logic.SaveSession(ctx, session); err != nil {
		t.Fatalf("保存会话失败: %v", err)
	}
	return token
}

// rows 返回当前全部审计行。
func (e *auditEnv) rows(t *testing.T) []domain.AuditLog {
	t.Helper()
	page, err := e.store.List(context.Background(), domain.AuditLogQuery{PageIndex: 1, PageSize: 100})
	if err != nil {
		t.Fatalf("查询审计行失败: %v", err)
	}
	return page.List
}

func okHandler(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

// TestInScope 锁定审计范围（白名单 ∪ 本服务前缀）。
func TestInScope(t *testing.T) {
	cases := map[string]bool{
		"/api/admin/system/dicts":      true,                                                                                                   // 本服务
		"/api/admin/system/audit-logs": true,                                                                                                   // 本服务
		"/api/read/admin/book/books":   true, "/api/admin/user/auth/login": true, "/api/read/app/book/books": false, "/api/admin/users": false, // 前缀不完全匹配
		"/api/admin/system": false, // 无结尾斜杠
		"/healthz":          false,
	}
	for path, want := range cases {
		if got := InScope(path); got != want {
			t.Errorf("InScope(%q) = %v, want %v", path, got, want)
		}
	}
}

// TestWriteOperationIsRecorded 写操作（POST/PUT/PATCH/DELETE）记录审计。
func TestWriteOperationIsRecorded(t *testing.T) {
	env := newAuditEnv(t, nil)
	token := env.login(t, "0", "root")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/system/dicts", strings.NewReader(`{"code":"book_tag"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("User-Agent", "curl/8.14.1")
	recorder := env.serve(t, req, token, RouteMeta{Detail: "DictAdminController.createDict"}, okHandler)
	if recorder.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d", recorder.Code)
	}

	rows := env.rows(t)
	if len(rows) != 1 {
		t.Fatalf("应写入 1 行审计日志，实际 %d", len(rows))
	}
	entry := rows[0]
	if entry.Type != domain.AuditLogTypeCreate {
		t.Fatalf("type 应为 CREATE: %q", entry.Type)
	}
	if domain.DerefString(entry.UserID) != "0" || domain.DerefString(entry.Username) != "root" ||
		domain.DerefString(entry.UserType) != domain.AuditLogUserTypeWorker {
		t.Fatalf("身份字段异常: %+v", entry)
	}
	if domain.DerefString(entry.Action) != "POST /api/admin/system/dicts" ||
		domain.DerefString(entry.Detail) != "DictAdminController.createDict" {
		t.Fatalf("action/detail 异常: %+v", entry)
	}
	if domain.DerefString(entry.RequestURI) != "/api/admin/system/dicts" ||
		domain.DerefString(entry.RequestMethod) != "POST" {
		t.Fatalf("请求字段异常: %+v", entry)
	}
	if domain.DerefString(entry.IP) != "203.0.113.9" || domain.DerefString(entry.UserAgent) != "curl/8.14.1" {
		t.Fatalf("ip/userAgent 异常: %+v", entry)
	}
	if domain.DerefString(entry.RequestParams) != `{"code":"book_tag"}` {
		t.Fatalf("requestParams 应取请求体 JSON: %q", domain.DerefString(entry.RequestParams))
	}
	if domain.DerefString(entry.CreatedAt) != "2026-09-14T01:02:03.456" {
		t.Fatalf("createdAt 异常: %q", domain.DerefString(entry.CreatedAt))
	}
	if entry.CreateBy != nil || entry.UpdateBy != nil || entry.UpdatedAt != nil {
		t.Fatalf("未赋值字段应为 null: %+v", entry)
	}
}

// TestGetRequestIsNotRecorded GET 不在审计范围内。
func TestGetRequestIsNotRecorded(t *testing.T) {
	env := newAuditEnv(t, nil)
	token := env.login(t, "0", "root")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/system/dicts", nil)
	env.serve(t, req, token, RouteMeta{}, okHandler)

	if rows := env.rows(t); len(rows) != 0 {
		t.Fatalf("GET 不应写审计日志: %+v", rows)
	}
}

// TestLoginIsRecordedWithFixedFields 登录分支写 LOGIN、固定 request
func TestLoginIsRecordedWithFixedFields(t *testing.T) {
	env := newAuditEnv(t, nil)
	token := env.login(t, "e2e-dualread-0001", "e2e_dualread")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/user/auth/login", strings.NewReader(`{"username":"e2e_dualread"}`))
	req.Header.Set("Content-Type", "application/json")
	env.serve(t, req, token, RouteMeta{Detail: "AuthController.login"}, okHandler)

	rows := env.rows(t)
	if len(rows) != 1 {
		t.Fatalf("登录应写入 1 行审计日志: %+v", rows)
	}
	entry := rows[0]
	if entry.Type != domain.AuditLogTypeLogin {
		t.Fatalf("type 应为 LOGIN: %q", entry.Type)
	}
	if domain.DerefString(entry.Action) != "e2e_dualread 登录系统" {
		t.Fatalf("action 异常: %q", domain.DerefString(entry.Action))
	}
	if domain.DerefString(entry.RequestURI) != "/api/auth/login" ||
		domain.DerefString(entry.RequestMethod) != "POST" {
		t.Fatalf("登录日志 requestUri/requestMethod 应固定: %+v", entry)
	}
	if entry.Detail != nil {
		t.Fatalf("登录日志 detail 应为 null: %v", *entry.Detail)
	}
}

// TestAnonymousIdentityDefaults 匿名身份默认值
func TestAnonymousIdentityDefaults(t *testing.T) {
	env := newAuditEnv(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/system/i18n/public/collect", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	env.servePublic(req, RouteMeta{Detail: "I18nAdminController.collect"}, okHandler)

	rows := env.rows(t)
	if len(rows) != 1 {
		t.Fatalf("应写入 1 行: %+v", rows)
	}
	entry := rows[0]
	if domain.DerefString(entry.UserID) != "0" ||
		domain.DerefString(entry.Username) != "anonymous" ||
		domain.DerefString(entry.UserType) != domain.AuditLogUserTypeApp {
		t.Fatalf("匿名身份字段异常: %+v", entry)
	}
}

// TestOutOfScopeRequestIsNotRecorded /api/read/app/** 不在审计范围
func TestOutOfScopeRequestIsNotRecorded(t *testing.T) {
	env := newAuditEnv(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/read/app/book/books", strings.NewReader(`{}`))
	env.servePublic(req, RouteMeta{Detail: "BookController.create"}, okHandler)
	if rows := env.rows(t); len(rows) != 0 {
		t.Fatalf("白名单外路径不应写审计日志: %+v", rows)
	}
}

// TestWhitelistPathIsRecorded 白名单路径记录审计
func TestWhitelistPathIsRecorded(t *testing.T) {
	env := newAuditEnv(t, nil)
	token := env.login(t, "0", "root")
	req := httptest.NewRequest(http.MethodDelete, "/api/read/admin/book/books/1", nil)
	env.serve(t, req, token, RouteMeta{Detail: "BookAdminController.delete"}, okHandler)

	rows := env.rows(t)
	if len(rows) != 1 || rows[0].Type != domain.AuditLogTypeDelete {
		t.Fatalf("白名单路径应写 DELETE 审计行: %+v", rows)
	}
	if domain.DerefString(rows[0].UserType) != domain.AuditLogUserTypeWorker {
		t.Fatalf("含 /admin/ 的路径 userType 应为 WORKER: %+v", rows[0])
	}
}

// failingStore 让写入失败，用于验证「审计失败不影响主流程」。
type failingStore struct {
	repo.MemoryAuditLogStore
	err error
}

func (s *failingStore) Insert(context.Context, domain.AuditLog) error { return s.err }

// TestInsertFailureDoesNotBreakRequest 审计写入失败不影响请求
func TestInsertFailureDoesNotBreakRequest(t *testing.T) {
	store := repo.NewMemoryAuditLogStore()
	recorder := New(Options{
		Store:  &failingStore{err: errors.New("mongo 不可用")},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    func() time.Time { return fixedNow },
	})
	handler := recorder.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"code":0}`))
	}), RouteMeta{Detail: "DictAdminController.createDict"})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/system/dicts", strings.NewReader(`{}`))
	recorderHTTP := httptest.NewRecorder()
	handler.ServeHTTP(recorderHTTP, req)

	if recorderHTTP.Code != http.StatusCreated || recorderHTTP.Body.String() != `{"code":0}` {
		t.Fatalf("审计写入失败不应影响响应: %d %s", recorderHTTP.Code, recorderHTTP.Body.String())
	}
	if rows, _ := store.List(context.Background(), domain.AuditLogQuery{PageIndex: 1, PageSize: 10}); len(rows.List) != 0 {
		t.Fatalf("故障存储不应有数据: %+v", rows.List)
	}
}

// TestBodyIsRestoredForHandler 审计读取请求体后必须还原，业务 handler 仍能读到完整内容。
func TestBodyIsRestoredForHandler(t *testing.T) {
	env := newAuditEnv(t, nil)
	token := env.login(t, "0", "root")
	payload := `{"code":"book_tag","name":"书籍标签"}`

	var seen string
	handler := func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		seen = string(body)
		w.WriteHeader(http.StatusOK)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/system/dicts", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	env.serve(t, req, token, RouteMeta{Detail: "DictAdminController.createDict"}, handler)

	if seen != payload {
		t.Fatalf("handler 读到的请求体不完整: %q", seen)
	}
}

// TestParamsFromQueryAndPathVariables 无请求体时按单参/多参形态组装摘要。
func TestParamsFromQueryAndPathVariables(t *testing.T) {
	env := newAuditEnv(t, nil)
	token := env.login(t, "0", "root")

	// 单参数（仅路径变量）→ JSON 标量。
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/system/dicts/d1", nil)
	req.Pattern = PathDictsPattern
	req.SetPathValue("id", "d1")
	env.serve(t, req, token, RouteMeta{Detail: "DictAdminController.deleteDict"}, okHandler)
	rows := env.rows(t)
	if len(rows) != 1 || domain.DerefString(rows[0].RequestParams) != `"d1"` {
		t.Fatalf("单路径参数摘要异常: %+v", rows)
	}

	// 多参数（路径变量 + 查询参数）→ JSON 数组，布尔/整型按 绑定类型还原。
	req = httptest.NewRequest(http.MethodDelete, "/api/admin/system/dicts/d1/data/i2?cascade=true&days=3", nil)
	req.Pattern = PathDictDataPattern + "/{id}"
	req.SetPathValue("dictId", "d1")
	req.SetPathValue("id", "i2")
	env.serve(t, req, token, RouteMeta{Detail: "DictDataAdminController.deleteDataItem"}, okHandler)
	rows = env.rows(t)
	if len(rows) != 2 {
		t.Fatalf("应累计 2 行: %+v", rows)
	}
	var multi *domain.AuditLog
	for index := range rows {
		if domain.DerefString(rows[index].Detail) == "DictDataAdminController.deleteDataItem" {
			multi = &rows[index]
		}
	}
	if multi == nil {
		t.Fatalf("未找到字典数据项删除审计行: %+v", rows)
	}
	if params := domain.DerefString(multi.RequestParams); params != `["d1","i2",true,3]` {
		t.Fatalf("多参数摘要异常: %q", params)
	}
}

// TestParamsTruncatedAt2000
func TestParamsTruncatedAt2000(t *testing.T) {
	env := newAuditEnv(t, nil)
	token := env.login(t, "0", "root")
	payload := `{"code":"` + strings.Repeat("x", 2500) + `"}`

	req := httptest.NewRequest(http.MethodPost, "/api/admin/system/dicts", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	env.serve(t, req, token, RouteMeta{Detail: "DictAdminController.createDict"}, okHandler)

	rows := env.rows(t)
	params := domain.DerefString(rows[0].RequestParams)
	if len([]rune(params)) != domain.AuditLogRequestParamsMaxLength+3 {
		t.Fatalf("截断后长度异常: %d", len([]rune(params)))
	}
	if !strings.HasSuffix(params, domain.AuditLogRequestParamsTruncatedSuffix) {
		t.Fatalf("截断后应有 ... 后缀: %q", params[len(params)-10:])
	}
}

// TestMultipartParamsDoNotLeakFileBytes 上传请求只记占位符，不落文件内容。
func TestMultipartParamsDoNotLeakFileBytes(t *testing.T) {
	env := newAuditEnv(t, nil)
	token := env.login(t, "0", "root")

	body := &strings.Builder{}
	body.WriteString("--BOUNDARY\r\n")
	body.WriteString("Content-Disposition: form-data; name=\"fileType\"\r\n\r\nother\r\n")
	body.WriteString("--BOUNDARY\r\n")
	body.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"a.txt\"\r\n")
	body.WriteString("Content-Type: text/plain\r\n\r\nSECRET-CONTENT\r\n")
	body.WriteString("--BOUNDARY--\r\n")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/system/files", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=BOUNDARY")
	env.serve(t, req, token, RouteMeta{Detail: "AdminFileController.upload"}, okHandler)

	rows := env.rows(t)
	params := domain.DerefString(rows[0].RequestParams)
	if params != "[multipart/form-data]" {
		t.Fatalf("multipart 参数摘要异常: %q", params)
	}
	if strings.Contains(params, "SECRET-CONTENT") {
		t.Fatal("审计摘要不应包含文件内容")
	}
}

// TestClientIPFallbacks X-Forwarded-For → X-Real-IP → RemoteAddr。
func TestClientIPFallbacks(t *testing.T) {
	cases := []struct {
		name       string
		forwarded  string
		realIP     string
		remoteAddr string
		want       string
	}{
		{name: "X-Forwarded-For 优先", forwarded: "203.0.113.1", realIP: "203.0.113.2", remoteAddr: "127.0.0.1:1234", want: "203.0.113.1"},
		{name: "X-Real-IP 次之", realIP: "203.0.113.2", remoteAddr: "127.0.0.1:1234", want: "203.0.113.2"},
		{name: "RemoteAddr 去端口", remoteAddr: "127.0.0.1:1234", want: "127.0.0.1"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/admin/system/dicts", nil)
			req.RemoteAddr = item.remoteAddr
			if item.forwarded != "" {
				req.Header.Set("X-Forwarded-For", item.forwarded)
			}
			if item.realIP != "" {
				req.Header.Set("X-Real-IP", item.realIP)
			}
			if got := clientIP(req); got != item.want {
				t.Fatalf("clientIP = %q, want %q", got, item.want)
			}
		})
	}
}

// TestRequestPathEscapedForm 审计记录的路径用转义形式。
func TestRequestPathEscapedForm(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/admin/system/dicts/%E4%B8%AD%E6%96%87/1?x=1", nil)
	if got := requestPath(req); got != "/api/admin/system/dicts/%E4%B8%AD%E6%96%87/1" {
		t.Fatalf("requestPath = %q", got)
	}
	plain := httptest.NewRequest(http.MethodPost, "/api/admin/system/dicts/d1", nil)
	if got := requestPath(plain); got != "/api/admin/system/dicts/d1" {
		t.Fatalf("requestPath = %q", got)
	}
}

// TestWriteFailureLogsWarning 校验写失败时确实打了 warning。
func TestWriteFailureLogsWarning(t *testing.T) {
	var logged strings.Builder
	recorder := New(Options{
		Store:  &failingStore{err: errors.New("写入失败")},
		Logger: slog.New(slog.NewTextHandler(&logged, nil)),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/system/dicts", strings.NewReader(`{}`))
	recorder.record(req, RouteMeta{Detail: "DictAdminController.createDict"})
	if !strings.Contains(logged.String(), "审计日志写入失败") {
		t.Fatalf("应记录 warning 日志: %q", logged.String())
	}
}
