package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-admin-system/service"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/result"
)

// requestContext。
func (a *API) requestContext(req *http.Request) service.RequestContext {
	return service.RequestContext{
		I18nCode: strings.TrimSpace(req.Header.Get("Accept-Language")),
		Actor:    logic.ActorFrom(req.Context(), a.checker),
	}
}

// requirePermission 判定单个权限码：root 放行，其余按会话权限码判定，不通过为 HTTP 403。
func (a *API) requirePermission(w http.ResponseWriter, req *http.Request, code string) bool {
	if err := a.actor(req).RequirePermission(code); err != nil {
		middleware.WriteError(req.Context(), w, err)
		return false
	}
	return true
}

// requireAllPermissions 判定多个权限码（AND 语义）。
//
// 类级 + 方法级双注解组合。
func (a *API) requireAllPermissions(w http.ResponseWriter, req *http.Request, codes ...string) bool {
	actor := a.actor(req)
	for _, code := range codes {
		if err := actor.RequirePermission(code); err != nil {
			middleware.WriteError(req.Context(), w, err)
			return false
		}
	}
	return true
}

// requireAnyPermission 判定权限码集合（OR 语义，对应 @SaCheckPermission(mode = SaMode.OR)）。
func (a *API) requireAnyPermission(w http.ResponseWriter, req *http.Request, codes ...string) bool {
	if err := a.actor(req).RequireAnyPermission(codes...); err != nil {
		middleware.WriteError(req.Context(), w, err)
		return false
	}
	return true
}

// actor 从请求上下文构造当前操作者（会话由 middleware.Auth 写入）。
func (a *API) actor(req *http.Request) *logic.Actor {
	return logic.ActorFrom(req.Context(), a.checker)
}

// writeServiceError 输出 service 层错误：
//   - 业务失败（apperr.Business）→ HTTP 200 + body.code≠0（含 40901）；
//   - 语义错误（apperr.Forbidden/BadRequest/Internal 等）→ 保留其 HTTP 状态码；
//   - 其它基础设施异常 → 503（保留原始错误信息，不吞异常）。
func writeServiceError(req *http.Request, w http.ResponseWriter, err error) {
	if business, ok := apperr.IsBusiness(err); ok {
		middleware.WriteResult(w, result.FailCode(business.Code, business.Message))
		return
	}
	if _, ok := apperr.As(err); ok {
		middleware.WriteError(req.Context(), w, err)
		return
	}
	middleware.WriteError(req.Context(), w, apperr.Unavailable("%v", err))
}

// decodeBody 解析 JSON 请求体；失败返回 400。
func decodeBody[T any](w http.ResponseWriter, req *http.Request) (T, bool) {
	var payload T
	if req.Body == nil {
		middleware.WriteError(req.Context(), w, apperr.BadRequest("请求格式错误"))
		return payload, false
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		middleware.WriteError(req.Context(), w, apperr.BadRequest("请求格式错误"))
		return payload, false
	}
	return payload, true
}

// queryPage 读取 pageIndex/pageSize 并按公共库口径归一化（默认 1/10，上限 100）。
func queryPage(req *http.Request) (int, int) {
	pageIndex := queryInt(req, "pageIndex", result.DefaultPageIndex)
	pageSize := queryInt(req, "pageSize", result.DefaultPageSize)
	return result.NormalizePage(pageIndex, pageSize)
}

// queryPageWith 读取分页参数，允许自定义默认页大小（审计日志为 20）。
func queryPageWith(req *http.Request, defaultPageSize int) (int, int) {
	pageIndex := queryInt(req, "pageIndex", result.DefaultPageIndex)
	pageSize := queryInt(req, "pageSize", defaultPageSize)
	return result.NormalizePage(pageIndex, pageSize)
}

// queryInt 读取整型查询参数，缺失或非法时取默认值（Spring 对非法值会 400，此处按缺省处理）。
func queryInt(req *http.Request, name string, fallback int) int {
	raw := strings.TrimSpace(req.URL.Query().Get(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

// queryIntPtr 读取可空整型查询参数。
func queryIntPtr(req *http.Request, name string) *int {
	raw := strings.TrimSpace(req.URL.Query().Get(name))
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &value
}

// queryBool 按 Spring StringToBooleanConverter 口径解析布尔查询参数（true/on/yes/1）。
func queryBool(req *http.Request, name string) bool {
	switch strings.ToLower(strings.TrimSpace(req.URL.Query().Get(name))) {
	case "true", "on", "yes", "1":
		return true
	default:
		return false
	}
}

// queryString 读取字符串查询参数（返回原始值，便于区分空串与缺失）。
func queryString(req *http.Request, name string) string {
	return req.URL.Query().Get(name)
}

// hasQuery 判断查询参数是否存在（对应 Spring @RequestParam(required=false) 的存在性）。
func hasQuery(req *http.Request, name string) bool {
	return req.URL.Query().Has(name)
}
