package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/result"
)

// 管理接口路由常量（基路径）与子路径后缀。
const (
	// PathReaders 读者用户管理。
	PathReaders = "/api/admin/user/readers"
	// PathWorkers 工作人员管理。
	PathWorkers = "/api/admin/user/workers"
	// PathRoles 角色管理。
	PathRoles = "/api/admin/user/roles"
	// PathPermissions 权限管理。
	PathPermissions = "/api/admin/user/permissions"
)

// 子路径后缀：与 @PathVariable 路径逐字对齐。
const (
	subByID        = "/{id}"
	subRoles       = "/{id}/roles"
	subMute        = "/{id}/mute"
	subPermissions = "/{id}/permissions"
)

// actor 从请求上下文构造当前操作者（会话由 middleware.Auth 写入）。
func (a *API) actor(req *http.Request) *logic.Actor {
	return logic.ActorFrom(req.Context(), a.checker)
}

// requirePermission 执行权限判定：root 放行，其余按会话权限码判定，不通过为 HTTP 403。
func (a *API) requirePermission(w http.ResponseWriter, req *http.Request, code string) bool {
	if err := a.actor(req).RequirePermission(code); err != nil {
		middleware.WriteError(req.Context(), w, err)
		return false
	}
	return true
}

// writeServiceError 输出 service 层错误：
//   - 业务失败（apperr.Business）→ HTTP 200 + body.code≠0；
//   - 语义错误（apperr.Forbidden/BadRequest 等）→ 保留其 HTTP 状态码；
//   - 基础设施异常 → 503（保留原始错误信息，不吞异常）。
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

// queryBool 按 Spring StringToBooleanConverter 口径解析布尔查询参数（true/on/yes/1）。
func queryBool(req *http.Request, name string) bool {
	switch strings.ToLower(strings.TrimSpace(req.URL.Query().Get(name))) {
	case "true", "on", "yes", "1":
		return true
	default:
		return false
	}
}
