package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"47.108.230.93/acat-fun/acat-go-admin-system/logic"
	"47.108.230.93/acat-fun/acat-go-admin-system/service"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/result"
)

// registerAuditLogRoutes 注册审计日志路由。
func registerAuditLogRoutes(mux *http.ServeMux, auth authMiddleware, a *API) {
	a.route(mux, "GET "+PathAuditLogs, auth, a.handleListAuditLogs)
	a.route(mux, "DELETE "+PathAuditLogs, auth, a.handleCleanAuditLogs)
}

// handleListAuditLogs 复刻 list：pageSize 默认 20（本服务唯一的非 10 默认值）。
func (a *API) handleListAuditLogs(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemAuditLogs) {
		return
	}
	pageIndex, pageSize, err := auditPageParams(req)
	if err != nil {
		writeAuditStoreError(req, w, err)
		return
	}
	data, err := a.svc.ListAuditLogs(req.Context(), pageIndex, pageSize,
		queryString(req, "type"), queryString(req, "userType"))
	if err != nil {
		writeAuditStoreError(req, w, err)
		return
	}
	writeOK(w, data)
}

// handleCleanAuditLogs 复刻 clean：days 必填，返回**删除后剩余总数**。
func (a *API) handleCleanAuditLogs(w http.ResponseWriter, req *http.Request) {
	if !a.requirePermission(w, req, logic.SystemAuditLogs) {
		return
	}
	if !hasQuery(req, "days") {
		writeServiceError(req, w, internalError(service.MessageInternal))
		return
	}
	days, err := strictQueryInt(req, "days", 0)
	if err != nil {
		writeAuditStoreError(req, w, err)
		return
	}
	remaining, err := a.svc.CleanAuditLogs(req.Context(), days)
	if err != nil {
		writeAuditStoreError(req, w, err)
		return
	}
	middleware.WriteResult(w, result.OK(remaining))
}

// auditPageParams 复刻审计列表的分页参数语义（严格对齐 Spring + Spring Data）：
//
//   - 缺失或空串 → @RequestParam(defaultValue=…)：pageIndex=1、pageSize=20；
//   - 非整数或超出 int 范围 → 类型绑定失败 → HTTP 500；
//   - pageSize < 1 → `PageRequest.of` 抛 IllegalArgumentException → HTTP 500；
//   - pageIndex < 1 合法，服务层按 `Math.max(pageIndex-1, 0)` 收敛为第 1 页；
//   - pageSize 不设上限（Spring Data 无上限。
//
// 说明：本服务其它列表接口沿用公共库 `result.NormalizePage`（pageSize 收敛到 1..100），
// 审计接口按
func auditPageParams(req *http.Request) (int, int, error) {
	pageIndex, err := strictQueryInt(req, "pageIndex", result.DefaultPageIndex)
	if err != nil {
		return 0, 0, err
	}
	pageSize, err := strictQueryInt(req, "pageSize", service.DefaultAuditPageSize)
	if err != nil {
		return 0, 0, err
	}
	if pageSize < 1 {
		return 0, 0, fmt.Errorf("审计日志 pageSize 必须大于 0: %d", pageSize)
	}
	return pageIndex, pageSize, nil
}

// strictQueryInt 读取整型查询参数：缺失/空串取默认值，非法值或超范围报错。
func strictQueryInt(req *http.Request, name string, fallback int) (int, error) {
	raw := strings.TrimSpace(req.URL.Query().Get(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("查询参数 %s 不是合法整数: %q", name, raw)
	}
	return int(value), nil
}

// writeAuditStoreError 输出审计接口失败响应。
//
// ServiceUnavailableException，会落到 `GlobalExceptionHandler.handleException`
// 兜底分支 → **HTTP 500** `{"code":500,"message":"服务器内部错误"}`
// （GlobalExceptionHandler.java:109-113；503 分支只对应显式抛出的
// ServiceUnavailableException，见 GlobalExceptionHandler.java:79-83）。
//
// 因此审计接口不走公共库默认的「未知异常 → 503」口径（writeServiceError 的兜底分支），
// 而是把基础设施异常统一映射为 500，；业务失败与语义错误仍按各自口径输出。
func writeAuditStoreError(req *http.Request, w http.ResponseWriter, err error) {
	if business, ok := apperr.IsBusiness(err); ok {
		middleware.WriteResult(w, result.FailCode(business.Code, business.Message))
		return
	}
	if _, ok := apperr.As(err); ok {
		middleware.WriteError(req.Context(), w, err)
		return
	}
	// 记录原始 cause，便于排查 Mongo 故障。
	slog.Default().Error("审计日志存储异常", "error", err, "request_uri", req.URL.Path)
	middleware.WriteError(req.Context(), w, internalError(service.MessageInternal))
}
