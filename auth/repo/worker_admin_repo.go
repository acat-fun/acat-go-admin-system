package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
)

// AdminWorkerRepo 提供工作人员（t_acat_user_worker）管理数据访问。
//
// 与认证用的 WorkerRepo 分开：管理接口需要 email/avatar 的可空语义与 created_at/updated_at，
// 而认证链路只用 id/username/password/email/avatar/status。
type AdminWorkerRepo interface {
	// List 分页查询：keyword 对 username/email 做 LIKE，ORDER BY id ASC。
	List(ctx context.Context, keyword string, offset, limit int) ([]domain.AdminWorker, int64, error)
	// FindByUsername 按用户名查询未删除工作人员。
	FindByUsername(ctx context.Context, username string) (*domain.AdminWorker, error)
	// FindByID 按主键查询未删除工作人员。
	FindByID(ctx context.Context, id string) (*domain.AdminWorker, error)
	// Insert 新增工作人员（is_deleted=0，写入 created_at/updated_at）。
	Insert(ctx context.Context, worker *domain.AdminWorker) error
	// Update 按主键更新工作人员。
	Update(ctx context.Context, worker *domain.AdminWorker) (int64, error)
	// SoftDelete 软删除工作人员（is_deleted=1）。
	SoftDelete(ctx context.Context, id string) (int64, error)
	// RoleCodes 查询启用角色编码。
	RoleCodes(ctx context.Context, userID string) ([]string, error)
	// SoftDeleteRoles 软删除全部角色关联。
	SoftDeleteRoles(ctx context.Context, userID string) (int64, error)
	// InsertRoles 幂等插入角色关联。
	InsertRoles(ctx context.Context, userID string, roleIDs []string) (int64, error)
}

// MySQLAdminWorkerRepo 是 AdminWorkerRepo 的 MySQL 实现。
type MySQLAdminWorkerRepo struct {
	db DBTX
}

// NewAdminWorkerRepo 构造 MySQL 实现。
func NewAdminWorkerRepo(db DBTX) *MySQLAdminWorkerRepo { return &MySQLAdminWorkerRepo{db: db} }

const adminWorkerColumns = "id, username, password, email, avatar, status, created_at, updated_at"

// List 实现 AdminWorkerRepo。
func (r *MySQLAdminWorkerRepo) List(ctx context.Context, keyword string, offset, limit int) ([]domain.AdminWorker, int64, error) {
	where := " WHERE is_deleted = 0"
	args := make([]any, 0, 4)
	if keyword != "" {
		where += " AND (username LIKE ? OR email LIKE ?)"
		pattern := "%" + keyword + "%"
		args = append(args, pattern, pattern)
	}

	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM t_acat_user_worker"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计工作人员失败: %w", err)
	}

	query := "SELECT " + adminWorkerColumns + " FROM t_acat_user_worker" + where + " ORDER BY id ASC LIMIT ? OFFSET ?"
	rows, err := r.db.QueryContext(ctx, query, append(append([]any{}, args...), limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("查询工作人员失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.AdminWorker, 0, limit)
	for rows.Next() {
		worker, scanErr := scanAdminWorker(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		out = append(out, *worker)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("遍历工作人员失败: %w", err)
	}
	return out, total, nil
}

// FindByUsername 实现 AdminWorkerRepo。
func (r *MySQLAdminWorkerRepo) FindByUsername(ctx context.Context, username string) (*domain.AdminWorker, error) {
	query := "SELECT " + adminWorkerColumns + " FROM t_acat_user_worker WHERE username = ? AND is_deleted = 0"
	return r.scanOne(ctx, query, username)
}

// FindByID 实现 AdminWorkerRepo。
func (r *MySQLAdminWorkerRepo) FindByID(ctx context.Context, id string) (*domain.AdminWorker, error) {
	query := "SELECT " + adminWorkerColumns + " FROM t_acat_user_worker WHERE id = ? AND is_deleted = 0"
	return r.scanOne(ctx, query, id)
}

func (r *MySQLAdminWorkerRepo) scanOne(ctx context.Context, query string, args ...any) (*domain.AdminWorker, error) {
	worker, err := scanAdminWorker(r.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return worker, nil
}

func scanAdminWorker(row scanner) (*domain.AdminWorker, error) {
	var (
		worker = &domain.AdminWorker{}
		email  sql.NullString
		avatar sql.NullString
	)
	err := row.Scan(&worker.ID, &worker.Username, &worker.Password, &email, &avatar,
		&worker.Status, &worker.CreatedAt, &worker.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("扫描工作人员失败: %w", err)
	}
	worker.Email = nullableString(email)
	worker.Avatar = nullableString(avatar)
	return worker, nil
}

// Insert 实现 AdminWorkerRepo。
func (r *MySQLAdminWorkerRepo) Insert(ctx context.Context, worker *domain.AdminWorker) error {
	const query = `
		INSERT INTO t_acat_user_worker
			(id, username, password, email, avatar, status, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?)`
	if _, err := r.db.ExecContext(ctx, query, worker.ID, worker.Username, worker.Password,
		worker.Email, worker.Avatar, worker.Status, worker.CreatedAt, worker.UpdatedAt); err != nil {
		return fmt.Errorf("新增工作人员失败: %w", err)
	}
	return nil
}

// Update 实现 AdminWorkerRepo（WHERE id=? AND is_deleted=0）。
func (r *MySQLAdminWorkerRepo) Update(ctx context.Context, worker *domain.AdminWorker) (int64, error) {
	const query = `
		UPDATE t_acat_user_worker
		SET username = ?, email = ?, password = ?, status = ?, updated_at = ?
		WHERE id = ? AND is_deleted = 0`
	result, execErr := r.db.ExecContext(ctx, query, worker.Username, worker.Email, worker.Password,
		worker.Status, worker.UpdatedAt, worker.ID)
	if execErr != nil {
		return 0, fmt.Errorf("更新工作人员失败: %w", execErr)
	}
	return result.RowsAffected()
}

// SoftDelete 实现 AdminWorkerRepo。
func (r *MySQLAdminWorkerRepo) SoftDelete(ctx context.Context, id string) (int64, error) {
	const query = `UPDATE t_acat_user_worker SET is_deleted = 1 WHERE id = ? AND is_deleted = 0`
	result, execErr := r.db.ExecContext(ctx, query, id)
	if execErr != nil {
		return 0, fmt.Errorf("删除工作人员失败: %w", execErr)
	}
	return result.RowsAffected()
}

// RoleCodes 实现 AdminWorkerRepo（仅启用角色）。
func (r *MySQLAdminWorkerRepo) RoleCodes(ctx context.Context, userID string) ([]string, error) {
	const query = `
		SELECT r.code
		FROM t_acat_role r
		INNER JOIN t_acat_user_role ur ON r.id = ur.role_id
		WHERE ur.user_id = ?
		  AND ur.is_deleted = 0
		  AND r.is_deleted = 0
		  AND r.status = 1`
	return queryStrings(ctx, r.db, query, userID)
}

// SoftDeleteRoles 实现 AdminWorkerRepo（SET is_deleted=1）。
func (r *MySQLAdminWorkerRepo) SoftDeleteRoles(ctx context.Context, userID string) (int64, error) {
	const query = `UPDATE t_acat_user_role SET is_deleted = 1 WHERE user_id = ? AND is_deleted = 0`
	result, execErr := r.db.ExecContext(ctx, query, userID)
	if execErr != nil {
		return 0, fmt.Errorf("软删除工作人员角色关联失败: %w", execErr)
	}
	return result.RowsAffected()
}

// InsertRoles 实现 AdminWorkerRepo（幂等恢复软删记录）。
func (r *MySQLAdminWorkerRepo) InsertRoles(ctx context.Context, userID string, roleIDs []string) (int64, error) {
	if len(roleIDs) == 0 {
		return 0, nil
	}
	values := make([]string, 0, len(roleIDs))
	args := make([]any, 0, len(roleIDs)*2)
	for _, roleID := range roleIDs {
		values = append(values, "(?, ?)")
		args = append(args, userID, roleID)
	}
	query := `INSERT INTO t_acat_user_role (user_id, role_id) VALUES ` + strings.Join(values, ", ") +
		` ON DUPLICATE KEY UPDATE is_deleted = 0`
	result, execErr := r.db.ExecContext(ctx, query, args...)
	if execErr != nil {
		return 0, fmt.Errorf("写入工作人员角色关联失败: %w", execErr)
	}
	return result.RowsAffected()
}
