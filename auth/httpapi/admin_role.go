package httpapi

import (
	"net/http"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
	"github.com/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/result"
)

// registerRoleRoutes 注册角色管理路由。
func (a *API) registerRoleRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("GET "+PathRoles, auth(http.HandlerFunc(a.handleListRoles)))
	mux.Handle("POST "+PathRoles, auth(http.HandlerFunc(a.handleCreateRole)))
	mux.Handle("PUT "+PathRoles+subByID, auth(http.HandlerFunc(a.handleUpdateRole)))
	mux.Handle("DELETE "+PathRoles+subByID, auth(http.HandlerFunc(a.handleDeleteRole)))
	mux.Handle("PATCH "+PathRoles+subByID, auth(http.HandlerFunc(a.handleUpdateRoleStatus)))
	mux.Handle("GET "+PathRoles+subPermissions, auth(http.HandlerFunc(a.handleListRolePermissions)))
	mux.Handle("PUT "+PathRoles+subPermissions, auth(http.HandlerFunc(a.handleAssignRolePermissions)))
}

// registerPermissionRoutes 注册权限管理路由。
func (a *API) registerPermissionRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("GET "+PathPermissions, auth(http.HandlerFunc(a.handleListPermissions)))
	mux.Handle("POST "+PathPermissions, auth(http.HandlerFunc(a.handleCreatePermission)))
	mux.Handle("PUT "+PathPermissions+subByID, auth(http.HandlerFunc(a.handleUpdatePermission)))
	mux.Handle("DELETE "+PathPermissions+subByID, auth(http.HandlerFunc(a.handleDeletePermission)))
}

// handleListRoles 处理 GET /roles：
// format=select → 下拉选项；all=true → 全量 AdminRoleEntity；否则分页 PageData<WorkerRoleVO>。
func (a *API) handleListRoles(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemPermissions) {
		return
	}
	if req.URL.Query().Get("format") == "select" {
		data, err := a.svc.ListRolesForSelect(req.Context())
		if err != nil {
			writeServiceError(req, w, err)
			return
		}
		middleware.WriteResult(w, result.OK(data))
		return
	}
	if queryBool(req, "all") {
		data, err := a.svc.ListRolesAll(req.Context())
		if err != nil {
			writeServiceError(req, w, err)
			return
		}
		middleware.WriteResult(w, result.OK(data))
		return
	}
	pageIndex, pageSize := queryPage(req)
	data, err := a.svc.ListRolesPage(req.Context(), pageIndex, pageSize)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK(data))
}

func (a *API) handleCreateRole(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemPermissionsCreateRole) {
		return
	}
	dto, ok := decodeBody[domain.RoleSaveDTO](w, req)
	if !ok {
		return
	}
	data, err := a.svc.CreateRole(req.Context(), a.actor(req), dto)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK(data))
}

func (a *API) handleUpdateRole(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemPermissionsEditRole) {
		return
	}
	dto, ok := decodeBody[domain.RoleSaveDTO](w, req)
	if !ok {
		return
	}
	data, err := a.svc.UpdateRole(req.Context(), a.actor(req), req.PathValue("id"), dto)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK(data))
}

func (a *API) handleDeleteRole(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemPermissionsDeleteRole) {
		return
	}
	if err := a.svc.DeleteRole(req.Context(), a.actor(req), req.PathValue("id")); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

func (a *API) handleUpdateRoleStatus(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemPermissionsToggleRole) {
		return
	}
	payload, ok := decodeBody[domain.StatusParam](w, req)
	if !ok {
		return
	}
	if err := a.svc.UpdateRoleStatus(req.Context(), req.PathValue("id"), payload.Status); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

func (a *API) handleListRolePermissions(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemPermissions) {
		return
	}
	ids, err := a.svc.ListRolePermissionIDs(req.Context(), req.PathValue("id"))
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	if ids == nil {
		ids = []string{}
	}
	middleware.WriteResult(w, result.OK(ids))
}

func (a *API) handleAssignRolePermissions(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemPermissionsAssignPermission) {
		return
	}
	payload, ok := decodeBody[domain.PermissionAssignParam](w, req)
	if !ok {
		return
	}
	if err := a.svc.AssignRolePermissions(req.Context(), req.PathValue("id"), payload.PermissionIDs); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

// handleListPermissions 处理 GET /permissions：
// format=tree → List<PermissionVO>（扁平结构）；否则 → List<AdminPermissionEntity>。
func (a *API) handleListPermissions(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemPermissions) {
		return
	}
	if req.URL.Query().Get("format") == "tree" {
		data, err := a.svc.ListPermissionsAsVO(req.Context())
		if err != nil {
			writeServiceError(req, w, err)
			return
		}
		middleware.WriteResult(w, result.OK(data))
		return
	}
	data, err := a.svc.ListPermissions(req.Context())
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK(data))
}

func (a *API) handleCreatePermission(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemPermissionsCreate) {
		return
	}
	dto, ok := decodeBody[domain.PermissionSaveDTO](w, req)
	if !ok {
		return
	}
	data, err := a.svc.CreatePermission(req.Context(), dto)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK(data))
}

func (a *API) handleUpdatePermission(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemPermissionsEdit) {
		return
	}
	dto, ok := decodeBody[domain.PermissionSaveDTO](w, req)
	if !ok {
		return
	}
	data, err := a.svc.UpdatePermission(req.Context(), req.PathValue("id"), dto)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK(data))
}

func (a *API) handleDeletePermission(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemPermissionsDelete) {
		return
	}
	if err := a.svc.DeletePermission(req.Context(), req.PathValue("id")); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}
