package notification

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/result"
)

// Handler 是通知域 HTTP 处理器。
//
// 各端把它挂到自己的路由前缀下（如 acat 的 /api/read/app/user/notifications、
// devops 的 /api/v1/notifications）；登录态解析统一走 acat-go-common middleware
// 写入的会话（LoginID），由调用方在路由层套自己的认证中间件。
type Handler struct {
	svc     *Service
	keyword string // 服务名（日志用）
}

// NewHandler 构造 Handler。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc, keyword: "notification"} }

// markReadBody 是 PATCH 请求体（{isRead:boolean}）。
type markReadBody struct {
	IsRead *bool `json:"isRead"`
}

// HandleList 处理 GET 列表（多态：countOnly=true & status=unread 返回未读数）。
func (h *Handler) HandleList(w http.ResponseWriter, req *http.Request) {
	userID := loginIDFrom(req)
	if userID == "" {
		middleware.WriteError(req.Context(), w, apperr.Unauthorized("未登录或登录已过期"))
		return
	}
	q := req.URL.Query()

	if q.Get("countOnly") == "true" && q.Get("status") == "unread" {
		count, err := h.svc.UnreadCount(req.Context(), userID)
		if err != nil {
			middleware.WriteError(req.Context(), w, apperr.Internal(err, "查询未读数失败"))
			return
		}
		middleware.WriteResult(w, result.OK(count))
		return
	}

	pageIndex := 1
	if v := q.Get("pageIndex"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			pageIndex = n
		}
	}
	pageSize := DefaultPageSize
	if v := q.Get("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			pageSize = n
		}
	}
	page, err := h.svc.List(req.Context(), userID, pageIndex, pageSize)
	if err != nil {
		middleware.WriteError(req.Context(), w, apperr.Internal(err, "查询通知失败"))
		return
	}
	middleware.WriteResult(w, result.OK(page))
}

// HandleMarkRead 处理 PATCH /{id}（标记单条已读）。
//
// notificationID 由调用方从路径参数取出后传入（ServeMux PathValue 或前缀裁剪）。
func (h *Handler) HandleMarkRead(w http.ResponseWriter, req *http.Request, notificationID string) {
	userID := loginIDFrom(req)
	if userID == "" {
		middleware.WriteError(req.Context(), w, apperr.Unauthorized("未登录或登录已过期"))
		return
	}
	if notificationID == "" {
		middleware.WriteError(req.Context(), w, apperr.BadRequest("缺少通知 id"))
		return
	}
	var body markReadBody
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.IsRead == nil {
		middleware.WriteError(req.Context(), w, apperr.BadRequest("请求体非法"))
		return
	}
	if err := h.svc.MarkRead(req.Context(), userID, notificationID, *body.IsRead); err != nil {
		middleware.WriteError(req.Context(), w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

// HandleMarkAllRead 处理 PATCH（全部已读）。
func (h *Handler) HandleMarkAllRead(w http.ResponseWriter, req *http.Request) {
	userID := loginIDFrom(req)
	if userID == "" {
		middleware.WriteError(req.Context(), w, apperr.Unauthorized("未登录或登录已过期"))
		return
	}
	var body markReadBody
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.IsRead == nil {
		middleware.WriteError(req.Context(), w, apperr.BadRequest("请求体非法"))
		return
	}
	if err := h.svc.MarkAllRead(req.Context(), userID, *body.IsRead); err != nil {
		middleware.WriteError(req.Context(), w, err)
		return
	}
	middleware.WriteResult(w, result.OK[any](nil))
}

// loginIDFrom 从请求上下文取登录 id（middleware.Auth 写入的会话）。
func loginIDFrom(req *http.Request) string {
	session := middleware.SessionFrom(req.Context())
	if session == nil || session.LoginID == nil {
		return ""
	}
	if value, ok := session.LoginID.(string); ok {
		return value
	}
	return ""
}
