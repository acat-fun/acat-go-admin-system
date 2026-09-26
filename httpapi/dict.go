package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/acat-fun/acat-go-admin-system/domain"
	"github.com/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/result"
)

// registerDictRoutes 注册字典与字典数据项路由。
func registerDictRoutes(mux *http.ServeMux, auth authMiddleware, a *API) {
	a.route(mux, "GET "+PathDicts, auth, a.handleListDicts)
	a.route(mux, "POST "+PathDicts, auth, a.handleCreateDict)
	a.route(mux, "PUT "+PathDicts+"/{id}", auth, a.handleUpdateDict)
	a.route(mux, "DELETE "+PathDicts+"/{id}", auth, a.handleDeleteDict)

	a.route(mux, "GET "+PathDictData, auth, a.handleListDictData)
	a.route(mux, "POST "+PathDictData, auth, a.handleCreateOrBatchDictData)
	a.route(mux, "PUT "+PathDictData+"/{id}", auth, a.handleUpdateDictData)
	a.route(mux, "DELETE "+PathDictData+"/{id}", auth, a.handleDeleteDictData)
}

// handleListDicts 处理 GET /dicts：
// format=select → List<SelectVO>；all=true → List<DictVO>；否则分页 PageData<DictVO>。
func (a *API) handleListDicts(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemDicts) {
		return
	}
	rc := a.requestContext(req)
	if queryString(req, "format") == "select" {
		data, err := a.svc.ListDictsForSelect(req.Context(), rc)
		if err != nil {
			writeServiceError(req, w, err)
			return
		}
		writeOK(w, domain.SelectVOs(data))
		return
	}
	if queryBool(req, "all") {
		data, err := a.svc.ListAllDicts(req.Context(), rc)
		if err != nil {
			writeServiceError(req, w, err)
			return
		}
		writeOK(w, data)
		return
	}
	pageIndex, pageSize := queryPage(req)
	filter := domain.DictFilter{
		Name:      queryString(req, "name"),
		IsEnabled: queryIntPtr(req, "isEnabled"),
		IsTree:    queryIntPtr(req, "isTree"),
		Scope:     queryIntPtr(req, "scope"),
	}
	data, err := a.svc.ListDicts(req.Context(), rc, filter, pageIndex, pageSize)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleCreateDict 处理 POST /dicts。
func (a *API) handleCreateDict(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemDictsCreate) {
		return
	}
	payload, ok := decodeBody[domain.DictSavePayload](w, req)
	if !ok {
		return
	}
	data, err := a.svc.CreateDict(req.Context(), a.requestContext(req), payload)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleUpdateDict 处理 PUT /dicts/{id}。
func (a *API) handleUpdateDict(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemDictsEdit) {
		return
	}
	payload, ok := decodeBody[domain.DictSavePayload](w, req)
	if !ok {
		return
	}
	data, err := a.svc.UpdateDict(req.Context(), a.requestContext(req), req.PathValue("id"), payload)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleDeleteDict 处理 DELETE /dicts/{id}。
func (a *API) handleDeleteDict(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemDictsDelete) {
		return
	}
	if err := a.svc.DeleteDict(req.Context(), req.PathValue("id")); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

// handleListDictData 处理 GET /dicts/{dictId}/data（format=select / all=true / 分页 三态返回）。
func (a *API) handleListDictData(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemDicts) {
		return
	}
	rc := a.requestContext(req)
	dictID := req.PathValue("dictId")
	if queryString(req, "format") == "select" {
		data, err := a.svc.ListDataItemsForSelect(req.Context(), rc, dictID)
		if err != nil {
			writeServiceError(req, w, err)
			return
		}
		writeOK(w, data)
		return
	}
	if queryBool(req, "all") {
		data, err := a.svc.ListAllDataItems(req.Context(), rc, dictID)
		if err != nil {
			writeServiceError(req, w, err)
			return
		}
		writeOK(w, data)
		return
	}
	pageIndex, pageSize := queryPage(req)
	data, err := a.svc.ListDataItems(req.Context(), rc, dictID, pageIndex, pageSize, queryString(req, "name"))
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleCreateOrBatchDictData 处理 POST /dicts/{dictId}/data：
// 数组 → 批量保存（data=null）；对象 → 单条创建（返回实体）；权限为 create OR edit。
func (a *API) handleCreateOrBatchDictData(w http.ResponseWriter, req *http.Request) {
	if !a.requireAnyPermission(w, req, logic.SystemDictsCreate, logic.SystemDictsEdit) {
		return
	}
	raw, ok := decodeRawBody(w, req)
	if !ok {
		return
	}
	rc := a.requestContext(req)
	dictID := req.PathValue("dictId")
	if isJSONArray(raw) {
		var payloads []domain.DictDataSavePayload
		if err := json.Unmarshal(raw, &payloads); err != nil {
			middleware.WriteError(req.Context(), w,
				apperr.BadRequest("请求格式错误"))
			return
		}
		if err := a.svc.BatchSaveDataItems(req.Context(), rc, dictID, payloads); err != nil {
			writeServiceError(req, w, err)
			return
		}
		middleware.WriteResult(w, result.OK[any](nil))
		return
	}
	var payload domain.DictDataSavePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		middleware.WriteError(req.Context(), w, apperr.BadRequest("请求格式错误"))
		return
	}
	data, err := a.svc.CreateDataItem(req.Context(), rc, dictID, payload)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleUpdateDictData 处理 PUT /dicts/{dictId}/data/{id}。
func (a *API) handleUpdateDictData(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemDictsEdit) {
		return
	}
	payload, ok := decodeBody[domain.DictDataSavePayload](w, req)
	if !ok {
		return
	}
	data, err := a.svc.UpdateDataItem(req.Context(), a.requestContext(req),
		req.PathValue("dictId"), req.PathValue("id"), payload)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleDeleteDictData 处理 DELETE /dicts/{dictId}/data/{id}；cascade 决定是否级联删除子数据项。
func (a *API) handleDeleteDictData(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemDictsDelete) {
		return
	}
	if err := a.svc.DeleteDataItem(req.Context(), req.PathValue("dictId"), req.PathValue("id"),
		queryBool(req, "cascade")); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

// decodeRawBody 读取原始 JSON 请求体（用于数组/对象分派）。
func decodeRawBody(w http.ResponseWriter, req *http.Request) (json.RawMessage, bool) {
	if req.Body == nil {
		middleware.WriteError(req.Context(), w, apperr.BadRequest("请求格式错误"))
		return nil, false
	}
	var raw json.RawMessage
	if err := json.NewDecoder(req.Body).Decode(&raw); err != nil {
		middleware.WriteError(req.Context(), w, apperr.BadRequest("请求格式错误"))
		return nil, false
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		middleware.WriteError(req.Context(), w, apperr.BadRequest("请求格式错误"))
		return nil, false
	}
	return raw, true
}

// isJSONArray 判断原始请求体是否为 JSON 数组。
func isJSONArray(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '['
}
