package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
)

// PermissionRepo 提供权限码注册表（t_acat_permission）数据访问。
//
// SQL 与 侧 PermissionMapper.xml / MyBatis-Plus 通用方法逐条对齐。
type PermissionRepo interface {
	// List 查询全量权限并按 id 升序（listAll：orderByAsc(id)）。
	List(ctx context.Context) ([]domain.AdminPermission, error)
	// FindByID 按主键查询未删除权限（selectById + @TableLogic）。
	FindByID(ctx context.Context, id string) (*domain.AdminPermission, error)
	// FindDeletedByCode 按 code 查询已软删记录（PermissionMapper.selectDeletedByCode）。
	FindDeletedByCode(ctx context.Context, code string) (*domain.AdminPermission, error)
	// ExistsByCode 判断 code 是否已被未删除记录占用（selectCount，excludeID 非空时排除自身）。
	ExistsByCode(ctx context.Context, code, excludeID string) (bool, error)
	// FindByIDs 批量查询存在的权限 id（selectBatchIds + @TableLogic）。
	FindByIDs(ctx context.Context, ids []string) ([]domain.AdminPermission, error)
	// Insert 新增（myInsert）。
	Insert(ctx context.Context, permission *domain.AdminPermission) error
	// Update 按主键更新（updateById）。
	Update(ctx context.Context, permission *domain.AdminPermission) (int64, error)
	// SoftDelete 软删除（myDelete）。
	SoftDelete(ctx context.Context, id string) (int64, error)
	// Restore 恢复已软删记录并更新 name/page_id（PermissionMapper.restoreDeleted）。
	Restore(ctx context.Context, id, name string, pageID *string, updatedAt time.Time) error
}

// MySQLPermissionRepo 是 PermissionRepo 的 MySQL 实现。
type MySQLPermissionRepo struct {
	db DBTX
}

// NewPermissionRepo 构造 MySQL 实现。
func NewPermissionRepo(db DBTX) *MySQLPermissionRepo { return &MySQLPermissionRepo{db: db} }

const permissionColumns = "id, code, name, page_id, created_at, updated_at"

// List 实现 PermissionRepo。
func (r *MySQLPermissionRepo) List(ctx context.Context) ([]domain.AdminPermission, error) {
	query := "SELECT " + permissionColumns + " FROM t_acat_permission WHERE is_deleted = 0 ORDER BY id ASC"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("查询权限失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPermissions(rows, 0)
}

// FindByID 实现 PermissionRepo。
func (r *MySQLPermissionRepo) FindByID(ctx context.Context, id string) (*domain.AdminPermission, error) {
	query := "SELECT " + permissionColumns + " FROM t_acat_permission WHERE id = ? AND is_deleted = 0"
	permission, err := scanPermission(r.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return permission, nil
}

// FindDeletedByCode 实现 PermissionRepo（PermissionMapper.selectDeletedByCode，LIMIT 1）。
func (r *MySQLPermissionRepo) FindDeletedByCode(ctx context.Context, code string) (*domain.AdminPermission, error) {
	query := "SELECT " + permissionColumns + " FROM t_acat_permission WHERE code = ? AND is_deleted = 1 LIMIT 1"
	permission, err := scanPermission(r.db.QueryRowContext(ctx, query, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return permission, nil
}

// ExistsByCode 实现 PermissionRepo。
func (r *MySQLPermissionRepo) ExistsByCode(ctx context.Context, code, excludeID string) (bool, error) {
	query := "SELECT COUNT(*) FROM t_acat_permission WHERE code = ? AND is_deleted = 0"
	args := []any{code}
	if excludeID != "" {
		query += " AND id <> ?"
		args = append(args, excludeID)
	}
	var count int64
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return false, fmt.Errorf("校验权限标识唯一性失败: %w", err)
	}
	return count > 0, nil
}

// FindByIDs 实现 PermissionRepo。
func (r *MySQLPermissionRepo) FindByIDs(ctx context.Context, ids []string) ([]domain.AdminPermission, error) {
	if len(ids) == 0 {
		return []domain.AdminPermission{}, nil
	}
	query := "SELECT " + permissionColumns + " FROM t_acat_permission WHERE id IN (" +
		inPlaceholders(len(ids)) + ") AND is_deleted = 0"
	rows, err := r.db.QueryContext(ctx, query, stringsToArgs(ids)...)
	if err != nil {
		return nil, fmt.Errorf("批量查询权限失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPermissions(rows, len(ids))
}

// Insert 实现 PermissionRepo。
func (r *MySQLPermissionRepo) Insert(ctx context.Context, permission *domain.AdminPermission) error {
	const query = `
		INSERT INTO t_acat_permission (id, code, name, page_id, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)`
	if _, err := r.db.ExecContext(ctx, query, permission.ID, permission.Code, permission.Name,
		permission.PageID, permission.CreatedAt, permission.UpdatedAt); err != nil {
		return fmt.Errorf("新增权限失败: %w", err)
	}
	return nil
}

// Update 实现 PermissionRepo（updateById：code/name/page_id）。
func (r *MySQLPermissionRepo) Update(ctx context.Context, permission *domain.AdminPermission) (int64, error) {
	const query = `
		UPDATE t_acat_permission
		SET code = ?, name = ?, page_id = ?, updated_at = ?
		WHERE id = ? AND is_deleted = 0`
	result, execErr := r.db.ExecContext(ctx, query, permission.Code, permission.Name, permission.PageID,
		permission.UpdatedAt, permission.ID)
	if execErr != nil {
		return 0, fmt.Errorf("更新权限失败: %w", execErr)
	}
	return result.RowsAffected()
}

// SoftDelete 实现 PermissionRepo。
func (r *MySQLPermissionRepo) SoftDelete(ctx context.Context, id string) (int64, error) {
	const query = `UPDATE t_acat_permission SET is_deleted = 1 WHERE id = ? AND is_deleted = 0`
	result, execErr := r.db.ExecContext(ctx, query, id)
	if execErr != nil {
		return 0, fmt.Errorf("删除权限失败: %w", execErr)
	}
	return result.RowsAffected()
}

// Restore 实现 PermissionRepo（PermissionMapper.restoreDeleted：仅更新已软删行）。
func (r *MySQLPermissionRepo) Restore(ctx context.Context, id, name string, pageID *string, updatedAt time.Time) error {
	const query = `
		UPDATE t_acat_permission
		SET is_deleted = 0, name = ?, page_id = ?, updated_at = ?
		WHERE id = ? AND is_deleted = 1`
	if _, err := r.db.ExecContext(ctx, query, name, pageID, updatedAt, id); err != nil {
		return fmt.Errorf("恢复权限失败: %w", err)
	}
	return nil
}

func scanPermissions(rows *sql.Rows, capacity int) ([]domain.AdminPermission, error) {
	if capacity < 0 {
		capacity = 0
	}
	out := make([]domain.AdminPermission, 0, capacity)
	for rows.Next() {
		permission, err := scanPermission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *permission)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历权限失败: %w", err)
	}
	return out, nil
}

func scanPermission(row scanner) (*domain.AdminPermission, error) {
	var (
		permission = &domain.AdminPermission{}
		pageID     sql.NullString
	)
	err := row.Scan(&permission.ID, &permission.Code, &permission.Name, &pageID,
		&permission.CreatedAt, &permission.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("扫描权限失败: %w", err)
	}
	permission.PageID = nullableString(pageID)
	return permission, nil
}
