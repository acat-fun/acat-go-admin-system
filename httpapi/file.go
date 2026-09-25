package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/logic"
	"47.108.230.93/acat-fun/acat-go-admin-system/service"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/result"
)

// maxUploadBytes 是上传体积上限（Spring Boot 默认 multipart max-file-size = 1MB）。
const maxUploadBytes = 1 << 20

// registerFileRoutes 注册文件路由。
func registerFileRoutes(mux *http.ServeMux, auth authMiddleware, a *API) {
	a.route(mux, "POST "+PathFiles, auth, a.handleUploadFile)
	a.route(mux, "GET "+PathFiles, auth, a.handleListFiles)
	a.route(mux, "GET "+PathFilesByID, auth, a.handleGetFile)
	a.route(mux, "DELETE "+PathFilesByID, auth, a.handleDeleteFile)
	a.route(mux, "GET "+PathFilesServeByID, auth, a.handleServeFile)
}

// handleListFiles 复刻 GET /files（类级权限）。
func (a *API) handleListFiles(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemFiles) {
		return
	}
	pageIndex, pageSize := queryPage(req)
	data, err := a.svc.ListFiles(req.Context(), pageIndex, pageSize, queryString(req, "fileType"))
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleUploadFile 复刻 POST /files（类级 + files:upload）。
func (a *API) handleUploadFile(w http.ResponseWriter, req *http.Request) {
	if !a.requireAllPermissions(w, req, logic.SystemFiles, logic.SystemFilesUpload) {
		return
	}
	if err := req.ParseMultipartForm(maxUploadBytes); err != nil {
		writeServiceError(req, w, internalError(service.MessageInternal))
		return
	}
	file, header, err := req.FormFile("file")
	if err != nil {
		writeServiceError(req, w, internalError(service.MessageInternal))
		return
	}
	defer func() { _ = file.Close() }()
	body, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		writeServiceError(req, w, internalError(service.MessageInternal))
		return
	}
	if len(body) > maxUploadBytes {
		writeServiceError(req, w, internalError(service.MessageInternal))
		return
	}
	contentType := header.Header.Get("Content-Type")
	data, err := a.svc.UploadFile(req.Context(), a.requestContext(req),
		req.FormValue("fileType"), header.Filename, contentType, body)
	if err != nil {
		writeServiceError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleDeleteFile 复刻 DELETE /files/{id}（类级 + files:delete）。
func (a *API) handleDeleteFile(w http.ResponseWriter, req *http.Request) {
	if !a.requireAllPermissions(w, req, logic.SystemFiles, logic.SystemFilesDelete) {
		return
	}
	if err := a.svc.DeleteFile(req.Context(), req.PathValue("id"), queryBool(req, "removeFromStorage")); err != nil {
		writeServiceError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

// handleGetFile 复刻 GET /files/{id}：裸二进制流，**不是 Result 结构**。
//
// 404 时手写 JSON，字段是 `msg`（不是 `message`）。
func (a *API) handleGetFile(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemFiles) {
		return
	}
	a.writeFileStream(w, req, queryBool(req, "download"))
}

// handleServeFile 复刻 GET /files/s/{id}：内联展示 + 一年缓存；404 时**空 body**。
func (a *API) handleServeFile(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemFiles) {
		return
	}
	file, err := a.svc.GetFile(req.Context(), req.PathValue("id"))
	if err != nil {
		if _, ok := apperr.IsBusiness(err); ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeServiceError(req, w, err)
		return
	}
	body, err := a.svc.OpenObject(req.Context(), *file)
	if err != nil {
		writeHandwrittenError(w, http.StatusInternalServerError, "500", service.MessageFileStreamFailed)
		return
	}
	writeStreamHeaders(w, *file, false)
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	writeStreamBody(w, *file, body)
}

// writeFileStream 处理 GET /files/{id}（可选 download=true）。
func (a *API) writeFileStream(w http.ResponseWriter, req *http.Request, download bool) {
	file, err := a.svc.GetFile(req.Context(), req.PathValue("id"))
	if err != nil {
		if business, ok := apperr.IsBusiness(err); ok {
			writeHandwrittenError(w, http.StatusNotFound, strconv.Itoa(404), business.Message)
			return
		}
		writeServiceError(req, w, err)
		return
	}
	body, err := a.svc.OpenObject(req.Context(), *file)
	if err != nil {
		writeHandwrittenError(w, http.StatusInternalServerError, "500", service.MessageFileStreamFailed)
		return
	}
	writeStreamHeaders(w, *file, download)
	writeStreamBody(w, *file, body)
}

// writeStreamHeaders。
// Content-Type 取实体 MIME（空则 application/octet-stream）、Content-Length 取实体 size、
// download=true 时附加 RFC 5987 风格的 Content-Disposition。
func writeStreamHeaders(w http.ResponseWriter, file domain.File, download bool) {
	contentType := file.Type
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	if download {
		filename := file.Name
		if filename == "" {
			filename = "file"
		}
		encoded := url.QueryEscape(filename)
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+encoded)
	}
}

// writeStreamBody 写出裸流。
//
// 否则交给 net/http 自行计算（避免响应被截断），差异记录在 README。
func writeStreamBody(w http.ResponseWriter, file domain.File, body []byte) {
	if int64(len(body)) == file.Size {
		w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// writeHandwrittenError 输出手写 JSON：字段名是 msg，不是 message。
//
// 实际消息（文件不存在 / 获取文件流失败）不含特殊字符，输出。
func writeHandwrittenError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(status)
	encoded := encodeJSONString(message)
	_, _ = w.Write([]byte(fmt.Sprintf(`{"code":%s,"msg":%s}`, code, encoded)))
}

// encodeJSONString 返回带引号的 JSON 字符串字面量（失败时回退空串）。
func encodeJSONString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(encoded)
}

// internalError 构造 500。
func internalError(message string) error {
	return apperr.Internal(errors.New(message), "%s", message)
}
