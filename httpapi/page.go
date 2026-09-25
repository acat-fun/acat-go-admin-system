package httpapi

import (
	"net/http"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/result"
)

// registerPageRoutes 注册页面路由。
//
// GET /pages 有两种形态：仅当 query 参数 mine 恰好为 "true" 时才走
// 我的菜单树（无权限码、仅登录），否则走配置页面列表（permissions OR pages）。
// ServeMux 不支持参数条件映射，因此在处理器内分派，且更具体的条件优先。
func registerPageRoutes(mux *http.ServeMux, auth authMiddleware, a *API) {
	a.route(mux, "GET "+PathPages, auth, a.handleListPages)
	a.route(mux, "GET "+PathPageModuleCodes, auth, a.handleEnabledFrontendModuleCodes)
	a.route(mux, "POST "+PathPages, auth, a.handleCreatePage)
	a.route(mux, "PUT "+PathPages+"/{id}", auth, a.handleUpdatePage)
	a.route(mux, "DELETE "+PathPages+"/{id}", auth, a.handleDeletePage)
}

// handleListPages 分派 GET /pages：mine=true → 我的菜单树；否则 → 配置页面树。
func (a *API) handleListPages(w http.ResponseWriter, req *http.Request) {
	if queryString(req, "mine") == "true" {
		data, err := a.svc.ListAccessiblePages(req.Context(), a.requestContext(req))
		if err != nil {
			writeServiceError(req, w, err)
			return
		}
		writeOK(w, data)
		return
	}
	if !a.requireAnyPermission(w, req, logic.SystemPermissions, logic.SystemPages) {
		return
	}
	data, err := a.svc.ListPages(req.Context(), a.requestContext(req))
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleEnabledFrontendModuleCodes 处理 GET /pages/enabled-frontend-module-codes。
func (a *API) handleEnabledFrontendModuleCodes(w http.ResponseWriter, req *http.Request) {
	if !a.requireAnyPermission(w, req, logic.SystemPermissions, logic.SystemPages) {
		return
	}
	data, err := a.svc.ListEnabledFrontendModuleCodes(req.Context())
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	if data == nil {
		data = []string{}
	}
	writeOK(w, data)
}

// handleCreatePage 处理 POST /pages。
func (a *API) handleCreatePage(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemPagesCreate) {
		return
	}
	payload, ok := decodeBody[domain.PageSavePayload](w, req)
	if !ok {
		return
	}
	data, err := a.svc.CreatePage(req.Context(), a.requestContext(req), payload)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleUpdatePage 处理 PUT /pages/{id}。
func (a *API) handleUpdatePage(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemPagesEdit) {
		return
	}
	payload, ok := decodeBody[domain.PageSavePayload](w, req)
	if !ok {
		return
	}
	data, err := a.svc.UpdatePage(req.Context(), a.requestContext(req), req.PathValue("id"), payload)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleDeletePage 处理 DELETE /pages/{id}。
func (a *API) handleDeletePage(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemPagesDelete) {
		return
	}
	if err := a.svc.DeletePage(req.Context(), a.requestContext(req), req.PathValue("id")); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}
