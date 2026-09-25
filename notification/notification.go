// Package notification 提供站内通知域（t_acat_notification 表）的通用实现。
//
// 对外契约（acat 管理端消息铃铛）：
//   - GET  /notifications?countOnly=true&status=unread → data 为未读数（Integer）；
//   - GET  /notifications?pageIndex&pageSize           → data 为分页列表；
//   - PATCH /notifications/{id} body {isRead:true}      → 标记单条已读；
//   - PATCH /notifications    body {isRead:true}        → 全部已读。
//
// 各端把 handler 挂到自己的路由前缀下即可获得一致的
// 铃铛后端能力；存储直接使用 acat_user.t_acat_notification 表。
package notification

import (
	"context"
	"fmt"
	"time"

	"github.com/acat-fun/acat-go-common/apperr"
)

// 表名（acat_user 库，全限定名由 repo 层拼库前缀）。
const TableNotification = "t_acat_notification"

// DefaultPageSize 是分页缺省页大小。
const DefaultPageSize = 20

// Item 是通知条目（列表元素契约，键名与前端 NotificationItem 对齐）。
type Item struct {
	ID        string `json:"id"`
	BookID    string `json:"bookId"`
	ChapterID string `json:"chapterId"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	IsRead    bool   `json:"isRead"`
	CreatedAt string `json:"createdAt"`
}

// Page 是通知分页结果（headNodeTotal 恒为 null，键序与既有契约一致）。
type Page struct {
	Total         int64  `json:"total"`
	HeadNodeTotal *int64 `json:"headNodeTotal"`
	PageIndex     int    `json:"pageIndex"`
	PageSize      int    `json:"pageSize"`
	List          []Item `json:"list"`
}

// Repo 是通知数据访问接口。
type Repo interface {
	// ListByUser 分页查询用户通知（按 created_at 倒序）。
	ListByUser(ctx context.Context, userID string, offset, limit int) ([]Item, int64, error)
	// UnreadCount 返回用户未读数。
	UnreadCount(ctx context.Context, userID string) (int64, error)
	// MarkRead 标记单条已读（仅本人通知；返回是否命中）。
	MarkRead(ctx context.Context, userID, notificationID string) (int64, error)
	// MarkAllRead 标记用户全部未读为已读（返回影响行数）。
	MarkAllRead(ctx context.Context, userID string) (int64, error)
}

// Service 是通知域服务。
type Service struct {
	repo Repo
	now  func() time.Time
}

// Options 配置 Service。
type Options struct {
	Repo Repo
	// Now 时间源（默认 time.Now），测试可注入。
	Now func() time.Time
}

// New 构造 Service。
func New(opts Options) (*Service, error) {
	if opts.Repo == nil {
		return nil, fmt.Errorf("notification: Repo 必须注入")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Service{repo: opts.Repo, now: now}, nil
}

// List 分页返回用户通知。
func (s *Service) List(ctx context.Context, userID string, pageIndex, pageSize int) (Page, error) {
	if pageIndex <= 0 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > 100 {
		pageSize = 100
	}
	offset := (pageIndex - 1) * pageSize
	items, total, err := s.repo.ListByUser(ctx, userID, offset, pageSize)
	if err != nil {
		return Page{}, err
	}
	if items == nil {
		items = []Item{}
	}
	return Page{
		Total:         total,
		HeadNodeTotal: nil,
		PageIndex:     pageIndex,
		PageSize:      pageSize,
		List:          items,
	}, nil
}

// UnreadCount 返回用户未读数。
func (s *Service) UnreadCount(ctx context.Context, userID string) (int64, error) {
	return s.repo.UnreadCount(ctx, userID)
}

// MarkRead 标记单条已读；body.isRead 非 true 时返回「不支持的操作」业务失败。
func (s *Service) MarkRead(ctx context.Context, userID, notificationID string, isRead bool) error {
	if !isRead {
		return apperr.NewBusiness("不支持的操作")
	}
	affected, err := s.repo.MarkRead(ctx, userID, notificationID)
	if err != nil {
		return err
	}
	if affected == 0 {
		// 幂等：已是已读或非本人通知均按成功处理（与既有契约一致）。
		return nil
	}
	return nil
}

// MarkAllRead 全部已读；body.isRead 非 true 时返回「不支持的操作」业务失败。
func (s *Service) MarkAllRead(ctx context.Context, userID string, isRead bool) error {
	if !isRead {
		return apperr.NewBusiness("不支持的操作")
	}
	_, err := s.repo.MarkAllRead(ctx, userID)
	return err
}
