// Package audit 实现管理端接口的审计写入。
//
// 职责划分：
//
//	Recorder.Wrap           逐路由包装，先写审计再执行业务处理器
//	requestParams           构造 requestParams 摘要（params.go）
//	Recorder.record         写操作与登录两类分支的落库逻辑
//	clientIP                解析客户端 IP
//
// 已知行为边界：
//
//  1. URI 范围由 InScope 判定：两个固定前缀 ∪ 本服务自身的 /api/admin/system/；
//  2. requestParams 用「请求体/路径变量/查询参数」重建参数摘要；
//  3. 审计写入是同步执行，写失败只记 warning，不影响业务响应。
package audit

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/logic"
	"47.108.230.93/acat-fun/acat-go-admin-system/repo"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/satoken"
)

// javaAuditPrefixes 是固定的审计范围前缀（后端网关暴露的管理端路径）。
var javaAuditPrefixes = []string{"/api/read/admin/", "/api/admin/user/"}

// SelfPrefix 是本服务自身控制器的路由前缀。
//
// 审计范围是 javaAuditPrefixes 与 SelfPrefix 的并集，
// 因此本服务自身 /api/admin/system/ 下的写操作与登录同样会被记录。
const SelfPrefix = "/api/admin/system/"

// InScope 判断请求路径是否属于审计范围：
// javaAuditPrefixes 任一前缀命中，或命中本服务自身的 SelfPrefix。
func InScope(path string) bool {
	for _, prefix := range javaAuditPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return strings.HasPrefix(path, SelfPrefix)
}

// RouteMeta 是路由的审计元数据。
type RouteMeta struct {
	// Detail 是写入审计 detail 字段的路由标识，形如 `ClassName.methodName`
	// （例：DictAdminController.createDict）。登录分支不写 detail。
	Detail string
}

// Options 配置 Recorder。
type Options struct {
	// Store 审计日志存储（MongoAuditLogStore；测试可注入内存实现或故障桩）。
	Store repo.AuditLogStore
	// Logger 写失败时的告警日志。
	Logger *slog.Logger
	// Now 时间源，默认 time.Now（测试可注入固定时间）。
	Now func() time.Time
	// Location 时间换算时区，nil 表示 UTC。
	Location *time.Location
}

// Recorder 是审计日志写入器。
type Recorder struct {
	store    repo.AuditLogStore
	logger   *slog.Logger
	now      func() time.Time
	location *time.Location
}

// New 构造 Recorder。
func New(opts Options) *Recorder {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	location := opts.Location
	if location == nil {
		location = time.UTC
	}
	return &Recorder{store: opts.Store, logger: logger, now: now, location: location}
}

// Wrap 返回带审计写入的 handler。
//
// 审计写入发生在业务方法执行**之前**，
// 写失败只记 warning，不改变主流程与响应。
func (r *Recorder) Wrap(next http.Handler, meta RouteMeta) http.Handler {
	if r == nil || r.store == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.record(req, meta)
		next.ServeHTTP(w, req)
	})
}

// record 按路由元数据完成范围判定、身份识别与审计写入。
func (r *Recorder) record(req *http.Request, meta RouteMeta) {
	path := requestPath(req)
	if !InScope(path) {
		return
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	isWrite := method == http.MethodPost || method == http.MethodPut ||
		method == http.MethodDelete || method == http.MethodPatch
	isLogin := strings.Contains(path, "login")
	if !isLogin && !isWrite {
		return
	}

	userID, username, userType := identity(req, path)
	params := requestParams(req)
	created := domain.FormatAuditDateTime(r.now(), r.location)

	entry := domain.AuditLog{
		Type:          domain.AuditLogTypeFromHTTPMethod(method),
		UserID:        &userID,
		Username:      &username,
		UserType:      &userType,
		RequestParams: stringOrNil(params),
		IP:            stringOrNil(clientIP(req)),
		UserAgent:     stringOrNil(req.Header.Get("User-Agent")),
		CreatedAt:     &created,
	}
	if isLogin {
		// detail 保持 null。
		action := username + domain.AuditLogLoginActionSuffix
		entry.Type = domain.AuditLogTypeLogin
		entry.Action = &action
		entry.RequestURI = stringPtr(domain.AuditLogLoginRequestURI)
		entry.RequestMethod = stringPtr(domain.AuditLogLoginRequestMethod)
	} else {
		action := method + " " + path
		entry.Action = &action
		entry.Detail = stringOrNil(meta.Detail)
		entry.RequestURI = stringPtr(path)
		entry.RequestMethod = stringPtr(method)
	}

	if err := r.store.Insert(req.Context(), entry); err != nil {
		r.logger.Warn("审计日志写入失败",
			"error", err,
			"request_method", method,
			"request_uri", path,
			"detail", meta.Detail,
		)
	}
}

// requestPath 返回与 HttpServletRequest.getRequestURI() 同形的路径：
// 原始转义形式、不含查询串。
func requestPath(req *http.Request) string {
	if escaped := req.URL.EscapedPath(); escaped != "" {
		return escaped
	}
	return req.URL.Path
}

// identity 判定请求的审计身份：
//
//	未登录：userId="0"、username="anonymous"、userType="APP"；
//	已登录：userId=loginId、username=会话 username（空则 loginId）、
//	        userType=URI 含 "/admin/" ? "WORKER" : "APP"。
func identity(req *http.Request, path string) (userID, username, userType string) {
	userID = domain.AuditLogAnonymousUserID
	username = domain.AuditLogAnonymousUsername
	userType = domain.AuditLogUserTypeApp

	session := middleware.SessionFrom(req.Context())
	loginID := logic.LoginID(session)
	if session == nil || loginID == "" {
		return userID, username, userType
	}
	userID = loginID
	username = loginID
	if value := strings.TrimSpace(session.String(satoken.DataKeyUsername)); value != "" {
		username = value
	}
	if strings.Contains(path, "/admin/") {
		userType = domain.AuditLogUserTypeWorker
	}
	return userID, username, userType
}

// clientIP 解析客户端 IP（X-Forwarded-For → X-Real-IP → RemoteAddr）。
func clientIP(req *http.Request) string {
	if value := strings.TrimSpace(req.Header.Get("X-Forwarded-For")); value != "" {
		return value
	}
	if value := strings.TrimSpace(req.Header.Get("X-Real-IP")); value != "" {
		return value
	}
	remote := strings.TrimSpace(req.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil {
		return host
	}
	return remote
}

// stringOrNil 把空串转为 nil。
func stringOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// stringPtr 返回字符串指针（区分 null 与空串）。
func stringPtr(value string) *string { return &value }
