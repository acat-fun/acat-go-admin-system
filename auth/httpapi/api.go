// Package httpapi 装配 admin-user 的 HTTP 路由与处理器。
//
// 契约来源：（acat-admin-user 章节）。
// 关键约定：
//   - 业务失败一律 HTTP 200 + body.code≠0；
//   - 未登录 401、无权限 403（由中间件/处理器显式构造）；
//   - 仅 /api/admin/user/auth/login 放行，其余 /api/admin/** 必须登录（含未注册路径：
//     未登录 401，已登录才 404
//   - 管理接口（users/workers/roles/permissions）登录后再做权限码判定（logic.Checker）。
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/acat-fun/acat-go-admin-system/auth/service"
	"github.com/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/config"
	"github.com/acat-fun/acat-go-common/health"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/result"
	"github.com/acat-fun/acat-go-common/satoken"
)

// 路由常量：与 @RequestMapping 路径逐字对齐。
const (
	PathAuthLogin     = "/api/admin/user/auth/login"
	PathAuthUserInfo  = "/api/admin/user/auth/user-info"
	PathAuthBootstrap = "/api/admin/user/auth/bootstrap"
	PathAuthLogout    = "/api/admin/user/auth/logout"
	// PathAuthChangePassword 本人改密（校验原密码 → 更新密码 → 失效登录会话）。
	PathAuthChangePassword = "/api/admin/user/auth/change-password"
	PathMyPermissions      = "/api/admin/user/permissions/my"
	// PathAdminPrefix 是整个 /api/admin/** 的兜底模式
	// addPathPatterns("/api/admin/**") 对未注册路径同样生效，未登录一律 401，
	// 而不是 Go ServeMux 默认的 404 纯文本。
	PathAdminPrefix = "/api/admin/"
)

// loginRequest。
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

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
}

// Options 配置 API。
type Options struct {
	Service *service.Service
	Satoken *satoken.Logic
	Config  config.SaTokenConfig
	Logger  *slog.Logger
	// DisableAdminFallback 关闭 /api/admin/** 兜底模式。
	// 与共享 system 域（acat-go-admin-system/httpapi）同时挂载时必须有一侧关闭：
	// 两个 API 都会注册 "/api/admin/"，Go ServeMux 对重复模式直接 panic。
	DisableAdminFallback bool
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
	return &API{
		disableFallback: opts.DisableAdminFallback,
		svc:             opts.Service,
		satoken:         opts.Satoken,
		checker:         logic.NewChecker(opts.Satoken),
		cfg:             opts.Config,
		logger:          logger,
		ready:           opts.ReadyCheck,
	}, nil
}

// Register 注册路由；公开路由与受保护路由分别挂中间件。
func (a *API) Register(mux *http.ServeMux) {
	// 公开：登录。
	mux.Handle("POST "+PathAuthLogin, http.HandlerFunc(a.handleLogin))

	// 受保护：其余 /api/admin/** 需要登录。
	auth := middleware.Auth(middleware.AuthConfig{
		Logic:      a.satoken,
		CookieName: a.cfg.CookieName,
	})
	mux.Handle("GET "+PathAuthUserInfo, auth(http.HandlerFunc(a.handleUserInfo)))
	mux.Handle("GET "+PathAuthBootstrap, auth(http.HandlerFunc(a.handleBootstrap)))
	mux.Handle("POST "+PathAuthLogout, auth(http.HandlerFunc(a.handleLogout)))
	mux.Handle("POST "+PathAuthChangePassword, auth(http.HandlerFunc(a.handleChangePassword)))
	mux.Handle("GET "+PathMyPermissions, auth(http.HandlerFunc(a.handleMyPermissions)))

	// 管理接口（users/workers/roles/permissions）：登录 + 处理器内权限判定。
	a.registerUserRoutes(mux, auth)
	a.registerWorkerRoutes(mux, auth)
	a.registerRoleRoutes(mux, auth)
	a.registerPermissionRoutes(mux, auth)

	// 兜底：整个 /api/admin/** 必须先登录。
	// Go 1.22+ ServeMux 更具体的模式优先，因此该模式只兜住未注册路径：
	// 未登录 → auth 中间件 401；已登录 → handleAdminNotFound 404。
	if !a.disableFallback {
		mux.Handle(PathAdminPrefix, auth(http.HandlerFunc(a.handleAdminNotFound)))
	}
}

// handleAdminNotFound 处理已登录但未注册的 /api/admin/** 路径；
// 未登录请求在 auth 中间件处已返回 401，不会走到这里。
func (a *API) handleAdminNotFound(w http.ResponseWriter, req *http.Request) {
	middleware.WriteError(req.Context(), w, apperr.NotFound("接口不存在: %s", req.URL.Path))
}

// Health 返回就绪探针检查项。
func (a *API) Health() map[string]health.CheckFunc {
	if a.ready == nil {
		return nil
	}
	return map[string]health.CheckFunc{"dependencies": func(context.Context) error { return a.ready() }}
}

func (a *API) handleLogin(w http.ResponseWriter, req *http.Request) {
	var payload loginRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		middleware.WriteError(req.Context(), w, apperr.BadRequest("请求格式错误"))
		return
	}
	resultValue, err := a.svc.Login(req.Context(), payload.Username, payload.Password)
	if err != nil {
		if business, ok := isBusinessError(err); ok {
			// 业务失败：HTTP 200 + code≠0。
			middleware.WriteResult(w, result.FailCode(business.Code, business.Message))
			return
		}
		middleware.WriteError(req.Context(), w, apperr.Unavailable("登录失败: %v", err))
		return
	}
	a.setAuthCookie(w, resultValue.Token)
	middleware.WriteResult(w, result.OK(resultValue.Bootstrap))
}

func (a *API) handleUserInfo(w http.ResponseWriter, req *http.Request) {
	a.writeBootstrap(w, req)
}

func (a *API) handleBootstrap(w http.ResponseWriter, req *http.Request) {
	a.writeBootstrap(w, req)
}

func (a *API) writeBootstrap(w http.ResponseWriter, req *http.Request) {
	loginID := loginIDFrom(req)
	bootstrap, err := a.svc.Bootstrap(req.Context(), loginID)
	if err != nil {
		if business, ok := isBusinessError(err); ok {
			middleware.WriteResult(w, result.FailCode(business.Code, business.Message))
			return
		}
		middleware.WriteError(req.Context(), w, apperr.Unavailable("读取启动数据失败: %v", err))
		return
	}
	middleware.WriteResult(w, result.OK(bootstrap))
}

func (a *API) handleMyPermissions(w http.ResponseWriter, req *http.Request) {
	codes, err := a.svc.Permissions(req.Context(), loginIDFrom(req))
	if err != nil {
		middleware.WriteError(req.Context(), w, apperr.Unavailable("读取权限失败: %v", err))
		return
	}
	if codes == nil {
		codes = []string{}
	}
	middleware.WriteResult(w, result.OK(codes))
}

// changePasswordRequest 本人改密请求体。
type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// handleChangePassword 修改本人密码：成功后清 Cookie，前端回到登录页重新登录。
func (a *API) handleChangePassword(w http.ResponseWriter, req *http.Request) {
	var payload changePasswordRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		middleware.WriteError(req.Context(), w, apperr.BadRequest("请求格式错误"))
		return
	}
	if err := a.svc.ChangePassword(req.Context(), loginIDFrom(req), payload.OldPassword, payload.NewPassword); err != nil {
		if business, ok := isBusinessError(err); ok {
			middleware.WriteResult(w, result.FailCode(business.Code, business.Message))
			return
		}
		middleware.WriteError(req.Context(), w, apperr.Unavailable("修改密码失败: %v", err))
		return
	}
	a.clearAuthCookie(w)
	middleware.WriteResult(w, result.OK[any](nil))
}

func (a *API) handleLogout(w http.ResponseWriter, req *http.Request) {
	token := middleware.TokenFrom(req.Context())
	if err := a.svc.Logout(req.Context(), token); err != nil {
		middleware.WriteError(req.Context(), w, apperr.Unavailable("%v", err))
		return
	}
	a.clearAuthCookie(w)
	middleware.WriteResult(w, result.OK[any](nil))
}

// setAuthCookie 下发 HttpOnly 会话 Cookie，属性与前端 Cookie 模式约定一致
// （同源 HttpOnly + SameSite=Lax，Token 不落 localStorage）。
func (a *API) setAuthCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     a.cfg.CookieName,
		Value:    token,
		Path:     a.cfg.CookiePath,
		Domain:   a.cfg.CookieDomain,
		MaxAge:   a.cfg.CookieMaxAge,
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: sameSite(a.cfg.CookieSameSite),
	})
}

func (a *API) clearAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     a.cfg.CookieName,
		Value:    "",
		Path:     a.cfg.CookiePath,
		Domain:   a.cfg.CookieDomain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: sameSite(a.cfg.CookieSameSite),
	})
}

func sameSite(value string) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

func loginIDFrom(req *http.Request) string {
	session := middleware.SessionFrom(req.Context())
	if session == nil {
		return ""
	}
	if v, ok := session.LoginID.(string); ok {
		return v
	}
	if session.LoginID != nil {
		return fmt.Sprintf("%v", session.LoginID)
	}
	return ""
}

// isBusinessError 区分业务失败（HTTP 200 + code≠0）与基础设施异常（5xx）。
// 返回 (business, true) 表示按 Result.fail(code, message) 输出。
func isBusinessError(err error) (*apperr.Business, bool) {
	return apperr.IsBusiness(err)
}

// Name 实现 server.Service，返回服务名。
func (a *API) Name() string { return "admin-user" }

// Close 实现 server.Service；资源由 main 统一释放。
func (a *API) Close() error { return nil }

// Logger 暴露内部 logger，便于服务装配统一输出。
func (a *API) Logger() *slog.Logger { return a.logger }
