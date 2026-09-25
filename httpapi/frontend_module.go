package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/result"
)

// registerFrontendModuleRoutes 注册前端模块路由。
//
// 权限为「类级 + 方法级」双注解：Sa-Token 1.44 先校验类注解再校验方法注解（AND），
// 因此写接口同时需要页面码与方法码。
func registerFrontendModuleRoutes(mux *http.ServeMux, auth authMiddleware, a *API) {
	a.route(mux, "GET "+PathFrontendModules, auth, a.handleListFrontendModules)
	a.route(mux, "POST "+PathFrontendModules, auth, a.handleCreateFrontendModule)
	a.route(mux, "PUT "+PathFrontendModules+"/{id}", auth, a.handleUpdateFrontendModule)
	a.route(mux, "PUT "+PathFrontendModules+"/{id}/publication", auth, a.handlePublishFrontendModule)
}

// handleListFrontendModules 处理 GET /frontend-modules（moduleCode 空 → 全量，否则按模块码过滤）。
func (a *API) handleListFrontendModules(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemFrontendModules) {
		return
	}
	data, err := a.svc.ListFrontendModules(req.Context(), queryString(req, "moduleCode"))
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleCreateFrontendModule 处理 POST /frontend-modules。
func (a *API) handleCreateFrontendModule(w http.ResponseWriter, req *http.Request) {
	if !a.requireAllPermissions(w, req, logic.SystemFrontendModules, logic.SystemFrontendModulesCreate) {
		return
	}
	payload, ok := decodeBody[domain.FrontendModuleSaveParam](w, req)
	if !ok {
		return
	}
	if errors := validateFrontendModuleSavePayload(payload); len(errors) > 0 {
		writeValidationError(req, w, errors)
		return
	}
	data, err := a.svc.CreateFrontendModule(req.Context(), payload)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleUpdateFrontendModule 处理 PUT /frontend-modules/{id}。
func (a *API) handleUpdateFrontendModule(w http.ResponseWriter, req *http.Request) {
	if !a.requireAllPermissions(w, req, logic.SystemFrontendModules, logic.SystemFrontendModulesEdit) {
		return
	}
	payload, ok := decodeBody[domain.FrontendModuleSaveParam](w, req)
	if !ok {
		return
	}
	if errors := validateFrontendModuleSavePayload(payload); len(errors) > 0 {
		writeValidationError(req, w, errors)
		return
	}
	data, err := a.svc.UpdateFrontendModule(req.Context(), req.PathValue("id"), payload)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handlePublishFrontendModule 处理 PUT /frontend-modules/{id}/publication（乐观锁冲突业务码 40901）。
func (a *API) handlePublishFrontendModule(w http.ResponseWriter, req *http.Request) {
	if !a.requireAllPermissions(w, req, logic.SystemFrontendModules, logic.SystemFrontendModulesPublish) {
		return
	}
	payload, ok := decodeBody[domain.FrontendModulePublicationParam](w, req)
	if !ok {
		return
	}
	if errors := validateFrontendModulePublicationPayload(payload); len(errors) > 0 {
		writeValidationError(req, w, errors)
		return
	}
	data, err := a.svc.PublishFrontendModule(req.Context(), req.PathValue("id"), payload)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// writeValidationError 输出参数校验失败响应（HTTP 422 + "字段: 文案[; ...]"）。
func writeValidationError(req *http.Request, w http.ResponseWriter, messages []string) {
	body := result.FailCode(422, strings.Join(messages, "; "))
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(apperr.StatusUnprocessable)
	writeJSON(w, body)
}

// validateFrontendModuleSavePayload 校验前端模块保存载荷（必填与非空约束）。
//
// 文案为英文默认约束提示（与容器无 LANG 时的提示一致）；
// 字段顺序即 record 组件声明顺序。
func validateFrontendModuleSavePayload(payload domain.FrontendModuleSaveParam) []string {
	messages := make([]string, 0, 4)
	messages = appendNotBlank(messages, "moduleCode", payload.ModuleCode)
	if payload.ModuleCode != nil && !frontendModuleCodeRegexp.MatchString(*payload.ModuleCode) {
		messages = append(messages, fmt.Sprintf("moduleCode: must match %q", frontendModuleCodeRegexp.String()))
	}
	messages = appendNotBlank(messages, "name", payload.Name)
	messages = appendMaxSize(messages, "name", payload.Name, 100)
	messages = appendNotBlank(messages, "releaseVersion", payload.ReleaseVersion)
	messages = appendMaxSize(messages, "releaseVersion", payload.ReleaseVersion, 64)
	if payload.ContractVersion == nil {
		messages = append(messages, "contractVersion: must not be null")
	} else {
		if *payload.ContractVersion < 1 {
			messages = append(messages, "contractVersion: must be greater than or equal to 1")
		}
		if *payload.ContractVersion > 1 {
			messages = append(messages, "contractVersion: must be less than or equal to 1")
		}
	}
	messages = appendNotBlank(messages, "manifestPath", payload.ManifestPath)
	messages = appendMaxSize(messages, "manifestPath", payload.ManifestPath, 255)
	messages = appendMaxSize(messages, "fallbackVersion", payload.FallbackVersion, 64)
	messages = appendMaxSize(messages, "fallbackManifestPath", payload.FallbackManifestPath, 255)
	if payload.SortOrder == nil {
		messages = append(messages, "sortOrder: must not be null")
	}
	return messages
}

// validateFrontendModulePublicationPayload 校验前端模块发布载荷（必填与非空约束）。
func validateFrontendModulePublicationPayload(payload domain.FrontendModulePublicationParam) []string {
	messages := make([]string, 0, 4)
	messages = appendNotBlank(messages, "releaseVersion", payload.ReleaseVersion)
	messages = appendMaxSize(messages, "releaseVersion", payload.ReleaseVersion, 64)
	if payload.ContractVersion == nil {
		messages = append(messages, "contractVersion: must not be null")
	} else {
		if *payload.ContractVersion < 1 {
			messages = append(messages, "contractVersion: must be greater than or equal to 1")
		}
		if *payload.ContractVersion > 1 {
			messages = append(messages, "contractVersion: must be less than or equal to 1")
		}
	}
	messages = appendNotBlank(messages, "manifestPath", payload.ManifestPath)
	messages = appendMaxSize(messages, "manifestPath", payload.ManifestPath, 255)
	messages = appendMaxSize(messages, "fallbackVersion", payload.FallbackVersion, 64)
	messages = appendMaxSize(messages, "fallbackManifestPath", payload.FallbackManifestPath, 255)
	if payload.Status == nil {
		messages = append(messages, "status: must not be null")
	} else {
		if *payload.Status < 1 {
			messages = append(messages, "status: must be greater than or equal to 1")
		}
		if *payload.Status > 2 {
			messages = append(messages, "status: must be less than or equal to 2")
		}
	}
	if payload.ExpectedVersion == nil {
		messages = append(messages, "expectedVersion: must not be null")
	} else if *payload.ExpectedVersion < 0 {
		messages = append(messages, "expectedVersion: must be greater than or equal to 0")
	}
	return messages
}

func appendNotBlank(messages []string, field string, value *string) []string {
	if value == nil {
		return append(messages, field+": must not be blank")
	}
	if strings.TrimSpace(*value) == "" {
		return append(messages, field+": must not be blank")
	}
	return messages
}

func appendMaxSize(messages []string, field string, value *string, max int) []string {
	if value == nil {
		return messages
	}
	if len([]rune(*value)) > max {
		return append(messages, fmt.Sprintf("%s: size must be between 0 and %d", field, max))
	}
	return messages
}

// frontendModuleCodeRegexp （模块码格式约束）。
var frontendModuleCodeRegexp = domain.FrontendModuleCodePattern
