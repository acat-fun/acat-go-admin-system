package httpapi

import (
	"net/http"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/logic"
	"47.108.230.93/acat-fun/acat-go-admin-system/service"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/result"
)

// registerI18nRoutes 注册国际化路由。
//
// 公开路由已在 Register 中单独挂载：/public/types 与 /public/frontend-labels 免登录。
func registerI18nRoutes(mux *http.ServeMux, auth authMiddleware, a *API) {
	a.route(mux, "GET "+PathI18nTypes, auth, a.handleListI18nTypes)
	a.route(mux, "POST "+PathI18nTypes, auth, a.handleCreateI18nType)
	a.route(mux, "PUT "+PathI18nTypes+"/{id}", auth, a.handleUpdateI18nType)
	a.route(mux, "DELETE "+PathI18nTypes+"/{id}", auth, a.handleDeleteI18nType)
	// 仅登录、无权限码。
	a.route(mux, "GET "+PathI18nFrontendLabels, auth, a.handleFrontendLabels)
}

// handlePublicTypeOptions 处理 GET /i18n/public/types（免登录）。
func (a *API) handlePublicTypeOptions(w http.ResponseWriter, req *http.Request) {
	data, err := a.svc.ListTypeOptions(req.Context())
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handlePublicFrontendLabels 处理 GET /i18n/public/frontend-labels（免登录）。
func (a *API) handlePublicFrontendLabels(w http.ResponseWriter, req *http.Request) {
	a.writeFrontendLabels(w, req)
}

// handleFrontendLabels 处理 GET /i18n/frontend-labels（仅登录）。
func (a *API) handleFrontendLabels(w http.ResponseWriter, req *http.Request) {
	a.writeFrontendLabels(w, req)
}

func (a *API) writeFrontendLabels(w http.ResponseWriter, req *http.Request) {
	labels, err := a.svc.FrontendLabels(req.Context(), a.requestContext(req), queryString(req, "dictCode"))
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, labels)
}

// handleListI18nTypes 处理 GET /i18n/types（format=select / all=true / 分页 三态，仅登录、无权限码）。
func (a *API) handleListI18nTypes(w http.ResponseWriter, req *http.Request) {
	if queryString(req, "format") == "select" {
		data, err := a.svc.ListTypeOptions(req.Context())
		if err != nil {
			writeServiceError(req, w, err)
			return
		}
		writeOK(w, domain.SelectVOs(data))
		return
	}
	if queryBool(req, "all") {
		data, err := a.svc.ListAllTypes(req.Context())
		if err != nil {
			writeServiceError(req, w, err)
			return
		}
		writeOK(w, data)
		return
	}
	pageIndex, pageSize := queryPage(req)
	data, err := a.svc.ListTypes(req.Context(), pageIndex, pageSize,
		queryString(req, "keyword"), queryIntPtr(req, "isEnabled"))
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleCreateI18nType 处理 POST /i18n/types。
func (a *API) handleCreateI18nType(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemI18nTypesCreate) {
		return
	}
	payload, ok := decodeBody[service.I18nTypeSavePayload](w, req)
	if !ok {
		return
	}
	data, err := a.svc.CreateType(req.Context(), a.requestContext(req), payload)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleUpdateI18nType 处理 PUT /i18n/types/{id}。
func (a *API) handleUpdateI18nType(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemI18nTypesEdit) {
		return
	}
	payload, ok := decodeBody[service.I18nTypeSavePayload](w, req)
	if !ok {
		return
	}
	data, err := a.svc.UpdateType(req.Context(), a.requestContext(req), req.PathValue("id"), payload)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleDeleteI18nType 处理 DELETE /i18n/types/{id}。
func (a *API) handleDeleteI18nType(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemI18nTypesDelete) {
		return
	}
	if err := a.svc.DeleteType(req.Context(), req.PathValue("id")); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}
