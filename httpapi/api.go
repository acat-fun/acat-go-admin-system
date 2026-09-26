// Package httpapi 装配 admin-system 的 HTTP 路由与处理器。
//
// 关键约定：
//   - 业务失败一律 HTTP 200 + body.code≠0（含前端模块乐观锁 40901）；
//   - 未登录 401、无权限 403（apperr.Forbidden("无操作权限")）、请求格式错误 400、参数校验失败 422；
//   - 认证中间件 middleware.Auth（HttpOnly Cookie + satoken 头）；
//   - /api/admin/system/i18n/public/** 放行；
//   - 其余 /api/admin/** 必须先登录，含未注册路径：未登录 401、已登录才 404；
//   - 类级 + 方法级权限码都要满足（Sa-Token 1.44 先判类再判方法，AND 语义）。
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/acat-fun/acat-go-admin-system/audit"
	"github.com/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-admin-system/service"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/config"
	"github.com/acat-fun/acat-go-common/health"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/result"
	"github.com/acat-fun/acat-go-common/satoken"
)

// 路由基路径。
const (
	PathDicts              = "/api/admin/system/dicts"
	PathDictData           = "/api/admin/system/dicts/{dictId}/data"
	PathI18n               = "/api/admin/system/i18n"
	PathI18nTypes          = "/api/admin/system/i18n/types"
	PathI18nPublicTypes    = "/api/admin/system/i18n/public/types"
	PathI18nPublicLabels   = "/api/admin/system/i18n/public/frontend-labels"
	PathI18nFrontendLabels = "/api/admin/system/i18n/frontend-labels"
	PathPages              = "/api/admin/system/pages"
	PathPageModuleCodes    = "/api/admin/system/pages/enabled-frontend-module-codes"
	PathFrontendModules    = "/api/admin/system/frontend-modules"
	PathAuditLogs          = "/api/admin/system/audit-logs"
	PathFiles              = "/api/admin/system/files"
	PathFilesByID          = "/api/admin/system/files/{id}"
	PathFilesServeByID     = "/api/admin/system/files/s/{id}"
	// PathAdminPrefix 是整个 /api/admin/** 的兜底模式：
	// 认证拦截对未注册路径同样生效，未登录一律 401。
	PathAdminPrefix = "/api/admin/"
	// PathI18nPublicPrefix 是免登录路径（"/api/admin/system/i18n/public/**") 的
	// 子树前缀：该子树整段免登录（未注册路径也不要求登录）。
	PathI18nPublicPrefix = "/api/admin/system/i18n/public/"
)

// API 是 HTTP 处理器集合。
type API struct {
	svc     *service.Service
	satoken *satoken.Logic
	checker *logic.Checker
	cfg     config.SaTokenConfig
	logger  *slog.Logger
	ready   func() error
	// disableFallback 见 Options.DisableAdminFallback。
	disableFallback bool
	// audit 是审计日志写入器，逐路由插在认证之后。
	audit *audit.Recorder
	// routes 记录已注册的路由模式（供审计 detail 覆盖测试使用）。
	routes []string
}

// Options 配置 API。
type Options struct {
	Service *service.Service
	Satoken *satoken.Logic
	Config  config.SaTokenConfig
	Logger  *slog.Logger
	// DisableAdminFallback 关闭 /api/admin/** 兜底模式。
	// 与共享 auth 域（acat-go-admin-system/auth/httpapi）同时挂载时必须有一侧关闭：
	// 两个 API 都会注册 "/api/admin/"，Go ServeMux 对重复模式直接 panic。
	DisableAdminFallback bool
	// Audit 覆盖审计写入器；nil 时使用 Service 注入的审计存储（生产装配路径）。
	Audit *audit.Recorder
	// ReadyCheck 是就绪探针额外依赖检查（数据库等），可为 nil。
	ReadyCheck func() error
}

// New 构造 API。
func New(opts Options) (*API, error) {
	if opts.Service == nil || opts.Satoken == nil {
		return nil, fmt.Errorf("httpapi: Service 与 satoken.Logic 必须注入")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	recorder := opts.Audit
	if recorder == nil {
		recorder = audit.New(audit.Options{Store: opts.Service.AuditStore(), Logger: logger})
	}
	return &API{
		disableFallback: opts.DisableAdminFallback,
		svc:             opts.Service,
		satoken:         opts.Satoken,
		checker:         logic.NewChecker(opts.Satoken),
		cfg:             opts.Config,
		logger:          logger,
		ready:           opts.ReadyCheck,
		audit:           recorder,
	}, nil
}

// Register 注册路由；公开路由与受保护路由分别挂中间件。
func (a *API) Register(mux *http.ServeMux) {
	// 公开：/api/admin/system/i18n/public/**
	// 。
	a.route(mux, "GET "+PathI18nPublicTypes, passthroughAuth, a.handlePublicTypeOptions)
	a.route(mux, "GET "+PathI18nPublicLabels, passthroughAuth, a.handlePublicFrontendLabels)

	auth := middleware.Auth(middleware.AuthConfig{
		Logic:      a.satoken,
		CookieName: a.cfg.CookieName,
	})

	registerDictRoutes(mux, auth, a)
	registerI18nRoutes(mux, auth, a)
	registerPageRoutes(mux, auth, a)
	registerFrontendModuleRoutes(mux, auth, a)
	registerAuditLogRoutes(mux, auth, a)
	registerFileRoutes(mux, auth, a)

	// 兜底：整个 /api/admin/** 必须先登录，
	// 但 /api/admin/system/i18n/public/** 整段免登录。
	// Go 1.22+ ServeMux 更具体的模式优先，因此这两个模式只兜住未注册路径。
	mux.Handle(PathI18nPublicPrefix, http.HandlerFunc(a.handleAdminNotFound))
	if !a.disableFallback {
		mux.Handle(PathAdminPrefix, auth(http.HandlerFunc(a.handleAdminNotFound)))
	}
}

// passthroughAuth 是公开路由的占位认证中间件（不做鉴权，仅统一路由注册形态）。
func passthroughAuth(next http.Handler) http.Handler { return next }

// handleAdminNotFound 处理未注册的 /api/admin/** 路径：
// 未登录时由 auth 中间件先返回 401，只有已登录请求才会拿到 404。
func (a *API) handleAdminNotFound(w http.ResponseWriter, req *http.Request) {
	middleware.WriteError(req.Context(), w, apperr.NotFound("接口不存在: %s", req.URL.Path))
}

type authMiddleware func(http.Handler) http.Handler

// Health 返回就绪探针检查项。
func (a *API) Health() map[string]health.CheckFunc {
	if a.ready == nil {
		return nil
	}
	return map[string]health.CheckFunc{"dependencies": func(context.Context) error { return a.ready() }}
}

// Name 实现 server.Service，返回服务名。
func (a *API) Name() string { return "admin-system" }

// Close 实现 server.Service；资源由 main 统一释放。
func (a *API) Close() error { return nil }

// Logger 暴露内部 logger，便于服务装配统一输出。
func (a *API) Logger() *slog.Logger { return a.logger }

// writeOK 输出成功响应。
func writeOK(w http.ResponseWriter, payload any) { middleware.WriteResult(w, result.OK(payload)) }

// writeJSON 输出统一 Result 结构（裸流接口的 404/500 手写 JSON 除外）。
func writeJSON(w http.ResponseWriter, payload any) {
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Default().Error("写出响应失败", "error", err)
	}
}

// isBusinessError 区分业务失败（HTTP 200 + code≠0）与基础设施异常（5xx）。
func isBusinessError(err error) (*apperr.Business, bool) {
	return apperr.IsBusiness(err)
}

// trimOrEmpty 去除首尾空白。
func trimOrEmpty(value string) string { return strings.TrimSpace(value) }
