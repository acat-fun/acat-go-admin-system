package httpapi

import (
	"net/http"

	"47.108.230.93/acat-fun/acat-go-admin-system/audit"
)

// auditRouteDetails 是 Go 路由 → Controller.methodName 的唯一映射表。
//
// `signature.getMethod().getName()` 拼出 detail；Go 中间件没有方法签名，改为按路由登记。
// 新增写操作路由时若忘记登记，TestEveryWriteRouteHasAuditDetail 会直接失败（防漂移）。
//
// 只需登记写操作（POST/PUT/PATCH/DELETE）与 login 路由：GET 不在审计范围内
// ，detail 不会被使用。
var auditRouteDetails = map[string]string{
	"POST " + PathDicts:                                "DictAdminController.createDict",
	"PUT " + PathDicts + "/{id}":                       "DictAdminController.updateDict",
	"DELETE " + PathDicts + "/{id}":                    "DictAdminController.deleteDict",
	"POST " + PathDictData:                             "DictDataAdminController.createOrBatchSaveDataItems",
	"PUT " + PathDictData + "/{id}":                    "DictDataAdminController.updateDataItem",
	"DELETE " + PathDictData + "/{id}":                 "DictDataAdminController.deleteDataItem",
	"POST " + PathI18nTypes:                            "I18nAdminController.createType",
	"PUT " + PathI18nTypes + "/{id}":                   "I18nAdminController.updateType",
	"DELETE " + PathI18nTypes + "/{id}":                "I18nAdminController.deleteType",
	"POST " + PathPages:                                "PageAdminController.create",
	"PUT " + PathPages + "/{id}":                       "PageAdminController.update",
	"DELETE " + PathPages + "/{id}":                    "PageAdminController.delete",
	"POST " + PathFrontendModules:                      "FrontendModuleAdminController.create",
	"PUT " + PathFrontendModules + "/{id}":             "FrontendModuleAdminController.update",
	"PUT " + PathFrontendModules + "/{id}/publication": "FrontendModuleAdminController.publish",
	"POST " + PathFiles:                                "AdminFileController.upload",
	"DELETE " + PathFilesByID:                          "AdminFileController.deleteById",
	"DELETE " + PathAuditLogs:                          "AuditLogAdminController.clean",
}

// auditDetail 返回路由对应的 detail（未登记返回空串，写操作由测试兜底保证非空）。
func auditDetail(pattern string) string { return auditRouteDetails[pattern] }

// route 注册路由，并把审计写入中间件插在**认证之后、handler 之前**
// 。
func (a *API) route(mux *http.ServeMux, pattern string, auth authMiddleware, handler http.HandlerFunc) {
	a.routes = append(a.routes, pattern)
	wrapped := a.audit.Wrap(http.HandlerFunc(handler), audit.RouteMeta{Detail: auditDetail(pattern)})
	mux.Handle(pattern, auth(wrapped))
}
