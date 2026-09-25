package notification

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/acat-fun/acat-go-common/db"
)

// MySQLRepo 是 Repo 的 MySQL 实现（acat_user 库 t_acat_notification）。
type MySQLRepo struct {
	db   *db.Handle
	now  func() time.Time
	qual string // 全限定表名，默认 acat_user.t_acat_notification
}

// RepoOptions 配置 MySQLRepo。
type RepoOptions struct {
	// Database 是库名限定（默认 "acat_user"，传空则用裸表名）。
	Database string
	// Now 时间源（默认 time.Now）。
	Now func() time.Time
}

// NewMySQLRepo 构造 MySQL 实现。
func NewMySQLRepo(handle *db.Handle, opts RepoOptions) *MySQLRepo {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	qual := TableNotification
	if opts.Database != "" {
		qual = opts.Database + "." + TableNotification
	}
	return &MySQLRepo{db: handle, now: now, qual: qual}
}

const itemColumns = "id, book_id, chapter_id, type, title, content, is_read, created_at"

// ListByUser 实现 Repo。
func (r *MySQLRepo) ListByUser(ctx context.Context, userID string, offset, limit int) ([]Item, int64, error) {
	var total int64
	if err := r.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(1) FROM %s WHERE user_id = ? AND is_deleted = 0", r.qual),
		userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计通知失败: %w", err)
	}
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf("SELECT %s FROM %s WHERE user_id = ? AND is_deleted = 0 ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?", itemColumns, r.qual),
		userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("查询通知失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]Item, 0, limit)
	for rows.Next() {
		var (
			item      Item
			bookID    sql.NullString
			chapterID sql.NullString
			createdAt time.Time
		)
		if err := rows.Scan(&item.ID, &bookID, &chapterID, &item.Type, &item.Title, &item.Content,
			&item.IsRead, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("扫描通知失败: %w", err)
		}
		item.BookID = bookID.String
		item.ChapterID = chapterID.String
		item.CreatedAt = createdAt.Format("2006-01-02T15:04:05")
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("遍历通知失败: %w", err)
	}
	return items, total, nil
}

// UnreadCount 实现 Repo。
func (r *MySQLRepo) UnreadCount(ctx context.Context, userID string) (int64, error) {
	var count int64
	if err := r.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(1) FROM %s WHERE user_id = ? AND is_deleted = 0 AND is_read = 0", r.qual),
		userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("统计未读失败: %w", err)
	}
	return count, nil
}

// MarkRead 实现 Repo。
func (r *MySQLRepo) MarkRead(ctx context.Context, userID, notificationID string) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		fmt.Sprintf("UPDATE %s SET is_read = 1, update_by = ?, updated_at = ?, version = version + 1 WHERE id = ? AND user_id = ? AND is_deleted = 0 AND is_read = 0", r.qual),
		userID, r.now(), notificationID, userID)
	if err != nil {
		return 0, fmt.Errorf("标记已读失败: %w", err)
	}
	affected, _ := res.RowsAffected()
	return affected, nil
}

// MarkAllRead 实现 Repo。
func (r *MySQLRepo) MarkAllRead(ctx context.Context, userID string) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		fmt.Sprintf("UPDATE %s SET is_read = 1, update_by = ?, updated_at = ?, version = version + 1 WHERE user_id = ? AND is_deleted = 0 AND is_read = 0", r.qual),
		userID, r.now(), userID)
	if err != nil {
		return 0, fmt.Errorf("全部已读失败: %w", err)
	}
	affected, _ := res.RowsAffected()
	return affected, nil
}
