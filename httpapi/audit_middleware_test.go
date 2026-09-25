package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/acat-fun/acat-go-admin-system/domain"
	"github.com/acat-fun/acat-go-admin-system/repo"
	"github.com/acat-fun/acat-go-admin-system/service"
	"github.com/acat-fun/acat-go-admin-system/storage"
	"github.com/acat-fun/acat-go-common/config"
	"github.com/acat-fun/acat-go-common/satoken"
)

// failingAuditStore 模拟 MongoDB 不可用（查询/写入都失败）。
type failingAuditStore struct {
	repo.MemoryAuditLogStore
	err error
}

func (s *failingAuditStore) List(context.Context, domain.AuditLogQuery) (domain.AuditLogPage, error) {
	return domain.AuditLogPage{}, s.err
}

func (s *failingAuditStore) DeleteBefore(context.Context, time.Time) (int64, error) {
	return 0, s.err
}

func (s *failingAuditStore) Insert(context.Context, domain.AuditLog) error { return s.err }

// TestEveryWriteRouteHasAuditDetail 防止新增写操作路由时漏登记 detail
// 。
func TestEveryWriteRouteHasAuditDetail(t *testing.T) {
	server := newTestServer(t)
	writeMethods := map[string]bool{
		http.MethodPost: true, http.MethodPut: true,
		http.MethodPatch: true, http.MethodDelete: true,
	}
	if len(server.api.routes) == 0 {
		t.Fatal("未注册任何路由")
	}
	for _, pattern := range server.api.routes {
		method, _, found := strings.Cut(pattern, " ")
		if !found || !writeMethods[method] {
			continue
		}
		if auditDetail(pattern) == "" {
			t.Errorf("写操作路由缺少审计 detail 登记: %s", pattern)
		}
	}
	// 反向校验：登记表不应包含未注册的路由（避免改名后残留）。
	registered := map[string]bool{}
	for _, pattern := range server.api.routes {
		registered[pattern] = true
	}
	for pattern := range auditRouteDetails {
		if !registered[pattern] {
			t.Errorf("审计登记表中的路由未注册（可能已改名）: %s", pattern)
		}
	}
}

// TestWriteRequestWritesAuditRowThroughMux 端到端：写操作经真实路由写审计行，GET 不写。
func TestWriteRequestWritesAuditRowThroughMux(t *testing.T) {
	server := newTestServer(t)
	token := server.login(t, domain.RootLoginID, nil)

	// 无权限用户：写请求会被 handler 拒绝（403），但审计中间件仍在权限判定之前写入
	// 。
	recorder := server.do(t, http.MethodPost, PathFiles, token, []byte(`{"fileType":"other"}`))
	page, err := server.audits.List(context.Background(), domain.AuditLogQuery{PageIndex: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("写请求应写入 1 行审计日志（HTTP %d）: %+v", recorder.Code, page.List)
	}
	entry := page.List[0]
	if entry.Type != domain.AuditLogTypeCreate {
		t.Fatalf("type 应为 CREATE: %q", entry.Type)
	}
	if domain.DerefString(entry.Detail) != "AdminFileController.upload" {
		t.Fatalf("detail 应为 AdminFileController.upload: %q", domain.DerefString(entry.Detail))
	}
	if domain.DerefString(entry.Action) != "POST /api/admin/system/files" {
		t.Fatalf("action 异常: %q", domain.DerefString(entry.Action))
	}

	// GET 不写审计。
	before := page.Total
	server.do(t, http.MethodGet, PathFiles, token, nil)
	page, err = server.audits.List(context.Background(), domain.AuditLogQuery{PageIndex: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != before {
		t.Fatalf("GET 不应写审计日志: before=%d after=%d", before, page.Total)
	}
}

// TestUnauthenticatedWriteIsNotAudited 未登录请求被认证中间件拦下，不写审计
// （鉴权在方法执行前拒绝）。
func TestUnauthenticatedWriteIsNotAudited(t *testing.T) {
	server := newTestServer(t)
	recorder := server.do(t, http.MethodPost, PathFiles, "", []byte(`{}`))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，实际 %d", recorder.Code)
	}
	page, err := server.audits.List(context.Background(), domain.AuditLogQuery{PageIndex: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("未登录请求不应写审计: %+v", page.List)
	}
}

// TestAuditListMongoFailure Mongo 故障返回 500
// （不是公共库默认的 503）。
func TestAuditListMongoFailure(t *testing.T) {
	server := newTestServerWithAuditStore(t, &failingAuditStore{err: errors.New("mongo 不可用")})
	token := server.login(t, domain.RootLoginID, nil)

	recorder := server.do(t, http.MethodGet, PathAuditLogs, token, nil)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("Mongo 故障应 500，实际 %d (%s)", recorder.Code, recorder.Body.String())
	}
	code, message, _ := decodeResult(t, recorder)
	if code != 500 || message != "服务器内部错误" {
		t.Fatalf("500 响应体应与 一致: code=%d message=%q", code, message)
	}
}

// TestAuditCleanMongoFailureReturns500 清理接口同样按 500 兜底。
func TestAuditCleanMongoFailureReturns500(t *testing.T) {
	server := newTestServerWithAuditStore(t, &failingAuditStore{err: errors.New("mongo 不可用")})
	token := server.login(t, domain.RootLoginID, nil)

	recorder := server.do(t, http.MethodDelete, PathAuditLogs+"?days=30", token, nil)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("Mongo 故障应 500，实际 %d (%s)", recorder.Code, recorder.Body.String())
	}
}

// TestAuditWriteFailureDoesNotBreakBusinessRequest Mongo 故障时写操作照常返回业务结果。
func TestAuditWriteFailureDoesNotBreakBusinessRequest(t *testing.T) {
	server := newTestServerWithAuditStore(t, &failingAuditStore{err: errors.New("mongo 不可用")})
	token := server.login(t, domain.RootLoginID, nil)

	// root 有权限，POST /files 会因缺少 multipart 走业务 500，
	// 关键是不因审计写入失败而变成 503 或中断。
	recorder := server.do(t, http.MethodPost, PathFiles, token, []byte(`{}`))
	if recorder.Code == http.StatusServiceUnavailable {
		t.Fatalf("审计写入失败不应导致 503: %d %s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("响应不是 JSON: %s", recorder.Body.String())
	}
	if payload["code"] == float64(503) {
		t.Fatalf("审计写入失败不应把业务响应改成 503: %v", payload)
	}
}

// newTestServerWithAuditStore 用指定审计存储构造测试服务。
func newTestServerWithAuditStore(t *testing.T, audits repo.AuditLogStore) *testServer {
	t.Helper()
	logic := satoken.NewLogic(satoken.Config{
		TokenName:  "acat-admin-token",
		LoginType:  "login",
		Timeout:    satoken.DefaultTimeoutSeconds,
		IsShare:    true,
		TokenStyle: "uuid",
	}, satoken.NewMemoryStore(), nil)

	dicts := &stubDictRepo{frontendLabels: []domain.FrontendLabelRecord{
		{Code: "acat.read.admin.system.base-config/files.file.businessType", LabelValue: "业务类型"},
	}}
	types := &stubTypeRepo{options: []domain.I18nTypeRecord{
		{ID: "t1", Code: "zh-CN", Name: "中文", SortOrder: 1, IsEnabled: 1},
	}}
	svc, err := service.New(service.Options{
		Tx:        passthroughTx{},
		Dicts:     dicts,
		Labels:    dicts,
		Pages:     &stubPageRepo{},
		Modules:   &stubModuleRepo{},
		I18nTypes: types,
		Files:     &stubFileRepo{},
		Audits:    audits,
		Objects:   storage.NewMemory("acat-local"),
	})
	if err != nil {
		t.Fatalf("构造 Service 失败: %v", err)
	}
	api, err := New(Options{
		Service: svc,
		Satoken: logic,
		Config:  config.SaTokenConfig{CookieName: "acat-admin-token", CookiePath: "/"},
	})
	if err != nil {
		t.Fatalf("构造 API 失败: %v", err)
	}
	mux := http.NewServeMux()
	api.Register(mux)
	server := &testServer{handler: mux, logic: logic, api: api, svc: svc}
	if memory, ok := audits.(*repo.MemoryAuditLogStore); ok {
		server.audits = memory
	}
	return server
}
