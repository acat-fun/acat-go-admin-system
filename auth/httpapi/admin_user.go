package httpapi

import (
	"net/http"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
	"github.com/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/result"
)

// registerUserRoutes 注册用户管理路由。
func (a *API) registerUserRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("GET "+PathReaders, auth(http.HandlerFunc(a.handleListReaders)))
	mux.Handle("POST "+PathReaders, auth(http.HandlerFunc(a.handleCreateReader)))
	mux.Handle("PUT "+PathReaders+subByID, auth(http.HandlerFunc(a.handleUpdateReader)))
	mux.Handle("PATCH "+PathReaders+subByID, auth(http.HandlerFunc(a.handleUpdateReaderStatus)))
	mux.Handle("PATCH "+PathReaders+subMute, auth(http.HandlerFunc(a.handleUpdateReaderMute)))
	mux.Handle("DELETE "+PathReaders+subByID, auth(http.HandlerFunc(a.handleDeleteReader)))
	mux.Handle("PUT "+PathReaders+subRoles, auth(http.HandlerFunc(a.handleAssignReaderRoles)))
}

// registerWorkerRoutes 注册工作人员管理路由。
func (a *API) registerWorkerRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("GET "+PathWorkers, auth(http.HandlerFunc(a.handleListWorkers)))
	mux.Handle("POST "+PathWorkers, auth(http.HandlerFunc(a.handleCreateWorker)))
	mux.Handle("PUT "+PathWorkers+subByID, auth(http.HandlerFunc(a.handleUpdateWorker)))
	mux.Handle("PATCH "+PathWorkers+subByID, auth(http.HandlerFunc(a.handleUpdateWorkerStatus)))
	mux.Handle("DELETE "+PathWorkers+subByID, auth(http.HandlerFunc(a.handleDeleteWorker)))
	mux.Handle("PUT "+PathWorkers+subRoles, auth(http.HandlerFunc(a.handleAssignWorkerRoles)))
}

func (a *API) handleListReaders(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemUsersReaders) {
		return
	}
	pageIndex, pageSize := queryPage(req)
	data, err := a.svc.ListReaders(req.Context(), pageIndex, pageSize, req.URL.Query().Get("keyword"))
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK(data))
}

func (a *API) handleCreateReader(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemUsersReadersAdd) {
		return
	}
	dto, ok := decodeBody[domain.ReaderCreateDTO](w, req)
	if !ok {
		return
	}
	data, err := a.svc.CreateReader(req.Context(), dto)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK(data))
}

func (a *API) handleUpdateReader(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemUsersReadersEdit) {
		return
	}
	dto, ok := decodeBody[domain.ReaderUpdateDTO](w, req)
	if !ok {
		return
	}
	data, err := a.svc.UpdateReader(req.Context(), req.PathValue("id"), dto)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK(data))
}

func (a *API) handleUpdateReaderStatus(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemUsersReadersToggle) {
		return
	}
	payload, ok := decodeBody[statusBody](w, req)
	if !ok {
		return
	}
	if payload.Status == nil {
		middleware.WriteError(req.Context(), w, apperr.BadRequest("请求格式错误"))
		return
	}
	if err := a.svc.UpdateReaderStatus(req.Context(), req.PathValue("id"), *payload.Status); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

func (a *API) handleUpdateReaderMute(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemUsersReadersToggle) {
		return
	}
	payload, ok := decodeBody[muteBody](w, req)
	if !ok {
		return
	}
	if payload.Muted == nil {
		middleware.WriteError(req.Context(), w, apperr.BadRequest("请求格式错误"))
		return
	}
	if err := a.svc.UpdateReaderMute(req.Context(), req.PathValue("id"), *payload.Muted); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

func (a *API) handleDeleteReader(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemUsersReadersDelete) {
		return
	}
	if err := a.svc.DeleteReader(req.Context(), req.PathValue("id")); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

func (a *API) handleAssignReaderRoles(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemUsersReadersAssignRole) {
		return
	}
	payload, ok := decodeBody[domain.RoleAssignParam](w, req)
	if !ok {
		return
	}
	if err := a.svc.AssignReaderRoles(req.Context(), req.PathValue("id"), payload.RoleIDs); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

func (a *API) handleListWorkers(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemUsersWorkers) {
		return
	}
	pageIndex, pageSize := queryPage(req)
	data, err := a.svc.ListWorkers(req.Context(), pageIndex, pageSize, req.URL.Query().Get("keyword"))
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK(data))
}

func (a *API) handleCreateWorker(w http.ResponseWriter, req *http.Request) {
	dto, ok := decodeBody[domain.WorkerCreateDTO](w, req)
	if !ok {
		return
	}
	// workers:add 权限在 service 内判定。
	data, err := a.svc.CreateWorker(req.Context(), a.actor(req), dto)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK(data))
}

func (a *API) handleUpdateWorker(w http.ResponseWriter, req *http.Request) {
	dto, ok := decodeBody[domain.WorkerUpdateDTO](w, req)
	if !ok {
		return
	}
	data, err := a.svc.UpdateWorker(req.Context(), a.actor(req), req.PathValue("id"), dto)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK(data))
}

func (a *API) handleUpdateWorkerStatus(w http.ResponseWriter, req *http.Request) {
	payload, ok := decodeBody[statusBody](w, req)
	if !ok {
		return
	}
	if payload.Status == nil {
		middleware.WriteError(req.Context(), w, apperr.BadRequest("请求格式错误"))
		return
	}
	if err := a.svc.UpdateWorkerStatus(req.Context(), a.actor(req), req.PathValue("id"), *payload.Status); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

func (a *API) handleDeleteWorker(w http.ResponseWriter, req *http.Request) {
	if err := a.svc.DeleteWorker(req.Context(), a.actor(req), req.PathValue("id")); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

func (a *API) handleAssignWorkerRoles(w http.ResponseWriter, req *http.Request) {
	payload, ok := decodeBody[domain.RoleAssignParam](w, req)
	if !ok {
		return
	}
	if err := a.svc.AssignWorkerRoles(req.Context(), a.actor(req), req.PathValue("id"), payload.RoleIDs); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

// statusBody。
type statusBody struct {
	Status *int `json:"status"`
}

// muteBody。
type muteBody struct {
	Muted *int `json:"muted"`
}
