package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
)

// RoleRepo 提供角色（t_acat_role）与角色权限关联（t_acat_role_permission）数据访问。
//
// SQL 与 侧 RoleMapper.xml / MyBatis-Plus 通用方法逐条对齐。
type RoleRepo interface {
	// List 查询全量未删除角色（selectList(null)，all=true/下拉/工作人员角色装配使用）。
	List(ctx context.Context) ([]domain.Role, error)
	// ListPage 分页查询未删除角色（默认分页列表）。
	ListPage(ctx context.Context, offset, limit int) ([]domain.Role, int64, error)
	// FindByID 按主键查询（selectById + @TableLogic）。
	FindByID(ctx context.Context, id string) (*domain.Role, error)
	// FindByIDs 批量按主键查询（selectBatchIds + @TableLogic），用于过滤不存在的角色 id。
	FindByIDs(ctx context.Context, ids []string) ([]domain.Role, error)
	// Insert 新增（myInsert）。
	Insert(ctx context.Context, role *domain.Role) error
	// Update 按主键更新（myUpdate）。
	Update(ctx context.Context, role *domain.Role) (int64, error)
	// SoftDelete 软删除（myDelete）。
	SoftDelete(ctx context.Context, id string) (int64, error)
	// SoftDeleteUserRelations 软删除角色-用户关联（RoleMapper.deleteRoleUserRelations）。
	SoftDeleteUserRelations(ctx context.Context, roleID string) (int64, error)
	// SoftDeletePermissions 软删除角色全部权限关联（RoleMapper.deleteRolePermissions）。
	SoftDeletePermissions(ctx context.Context, roleID string) (int64, error)
	// PermissionIDs 查询角色已分配权限 id（RoleMapper.selectPermissionIds）。
	PermissionIDs(ctx context.Context, roleID string) ([]string, error)
	// ExistingPageIDs 查询给定 id 中真实存在的页面 id（RoleMapper.selectExistingPageIds）。
	ExistingPageIDs(ctx context.Context, ids []string) ([]string, error)
	// InsertPermissions 幂等写入角色权限关联并区分 resource_type（0=页面 1=按钮）。
	InsertPermissions(ctx context.Context, roleID string, permissionIDs []string, resourceType int) (int64, error)
}

// MySQLRoleRepo 是 RoleRepo 的 MySQL 实现。
type MySQLRoleRepo struct {
	db DBTX
}

// NewRoleRepo 构造 MySQL 实现。
func NewRoleRepo(db DBTX) *MySQLRoleRepo { return &MySQLRoleRepo{db: db} }

const roleColumns = "id, code, name, description, status, created_at, updated_at"

// List 实现 RoleRepo。
func (r *MySQLRoleRepo) List(ctx context.Context) ([]domain.Role, error) {
	query := "SELECT " + roleColumns + " FROM t_acat_role WHERE is_deleted = 0 ORDER BY id ASC"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("查询角色失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanRoles(rows)
}

// ListPage 实现 RoleRepo。
func (r *MySQLRoleRepo) ListPage(ctx context.Context, offset, limit int) ([]domain.Role, int64, error) {
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM t_acat_role WHERE is_deleted = 0").Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计角色失败: %w", err)
	}
	query := "SELECT " + roleColumns + " FROM t_acat_role WHERE is_deleted = 0 ORDER BY id ASC LIMIT ? OFFSET ?"
	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("查询角色失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	list, err := scanRolesWithCapacity(rows, limit)
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// FindByID 实现 RoleRepo。
func (r *MySQLRoleRepo) FindByID(ctx context.Context, id string) (*domain.Role, error) {
	query := "SELECT " + roleColumns + " FROM t_acat_role WHERE id = ? AND is_deleted = 0"
	role, err := scanRole(r.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return role, nil
}

// FindByIDs 实现 RoleRepo（selectBatchIds：仅返回存在且未删除的行）。
func (r *MySQLRoleRepo) FindByIDs(ctx context.Context, ids []string) ([]domain.Role, error) {
	if len(ids) == 0 {
		return []domain.Role{}, nil
	}
	query := "SELECT " + roleColumns + " FROM t_acat_role WHERE id IN (" + inPlaceholders(len(ids)) + ") AND is_deleted = 0"
	rows, err := r.db.QueryContext(ctx, query, stringsToArgs(ids)...)
	if err != nil {
		return nil, fmt.Errorf("批量查询角色失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanRolesWithCapacity(rows, len(ids))
}

// Insert 实现 RoleRepo（myInsert：is_deleted=0 + created_at/updated_at）。
func (r *MySQLRoleRepo) Insert(ctx context.Context, role *domain.Role) error {
	const query = `
		INSERT INTO t_acat_role (id, code, name, description, status, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)`
	if _, err := r.db.ExecContext(ctx, query, role.ID, role.Code, role.Name, role.Description,
		role.Status, role.CreatedAt, role.UpdatedAt); err != nil {
		return fmt.Errorf("新增角色失败: %w", err)
	}
	return nil
}

// Update 实现 RoleRepo（myUpdate：name/description/status + updated_at）。
func (r *MySQLRoleRepo) Update(ctx context.Context, role *domain.Role) (int64, error) {
	const query = `
		UPDATE t_acat_role
		SET name = ?, description = ?, status = ?, updated_at = ?
		WHERE id = ? AND is_deleted = 0`
	result, execErr := r.db.ExecContext(ctx, query, role.Name, role.Description, role.Status,
		role.UpdatedAt, role.ID)
	if execErr != nil {
		return 0, fmt.Errorf("更新角色失败: %w", execErr)
	}
	return result.RowsAffected()
}

// SoftDelete 实现 RoleRepo。
func (r *MySQLRoleRepo) SoftDelete(ctx context.Context, id string) (int64, error) {
	const query = `UPDATE t_acat_role SET is_deleted = 1 WHERE id = ? AND is_deleted = 0`
	result, execErr := r.db.ExecContext(ctx, query, id)
	if execErr != nil {
		return 0, fmt.Errorf("删除角色失败: %w", execErr)
	}
	return result.RowsAffected()
}

// SoftDeleteUserRelations 实现 RoleRepo（RoleMapper.deleteRoleUserRelations）。
func (r *MySQLRoleRepo) SoftDeleteUserRelations(ctx context.Context, roleID string) (int64, error) {
	const query = `UPDATE t_acat_user_role SET is_deleted = 1 WHERE role_id = ? AND is_deleted = 0`
	result, execErr := r.db.ExecContext(ctx, query, roleID)
	if execErr != nil {
		return 0, fmt.Errorf("软删除角色用户关联失败: %w", execErr)
	}
	return result.RowsAffected()
}

// SoftDeletePermissions 实现 RoleRepo（RoleMapper.deleteRolePermissions）。
func (r *MySQLRoleRepo) SoftDeletePermissions(ctx context.Context, roleID string) (int64, error) {
	const query = `UPDATE t_acat_role_permission SET is_deleted = 1 WHERE role_id = ? AND is_deleted = 0`
	result, execErr := r.db.ExecContext(ctx, query, roleID)
	if execErr != nil {
		return 0, fmt.Errorf("软删除角色权限关联失败: %w", execErr)
	}
	return result.RowsAffected()
}

// PermissionIDs 实现 RoleRepo（RoleMapper.selectPermissionIds）。
func (r *MySQLRoleRepo) PermissionIDs(ctx context.Context, roleID string) ([]string, error) {
	const query = `
		SELECT rp.permission_id
		FROM t_acat_role_permission rp
		WHERE rp.role_id = ? AND rp.is_deleted = 0
		ORDER BY rp.permission_id ASC`
	return queryStrings(ctx, r.db, query, roleID)
}

// ExistingPageIDs 实现 RoleRepo（RoleMapper.selectExistingPageIds）。
func (r *MySQLRoleRepo) ExistingPageIDs(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return []string{}, nil
	}
	query := "SELECT id FROM t_acat_page WHERE id IN (" + inPlaceholders(len(ids)) + ") AND is_deleted = 0"
	return queryStrings(ctx, r.db, query, stringsToArgs(ids)...)
}

// InsertPermissions 实现 RoleRepo。
//
// 因此追加 ON DUPLICATE KEY UPDATE（主键为 role_id + permission_id，软删行仍占用主键）。
func (r *MySQLRoleRepo) InsertPermissions(ctx context.Context, roleID string, permissionIDs []string, resourceType int) (int64, error) {
	if len(permissionIDs) == 0 {
		return 0, nil
	}
	values := make([]string, 0, len(permissionIDs))
	args := make([]any, 0, len(permissionIDs)*3)
	for _, permissionID := range permissionIDs {
		values = append(values, "(?, ?, ?)")
		args = append(args, roleID, permissionID, resourceType)
	}
	query := `INSERT INTO t_acat_role_permission (role_id, permission_id, resource_type) VALUES ` +
		strings.Join(values, ", ") +
		` ON DUPLICATE KEY UPDATE resource_type = VALUES(resource_type), is_deleted = 0`
	result, execErr := r.db.ExecContext(ctx, query, args...)
	if execErr != nil {
		return 0, fmt.Errorf("写入角色权限关联失败: %w", execErr)
	}
	return result.RowsAffected()
}

func scanRoles(rows *sql.Rows) ([]domain.Role, error) {
	return scanRolesWithCapacity(rows, 0)
}

func scanRolesWithCapacity(rows *sql.Rows, capacity int) ([]domain.Role, error) {
	if capacity < 0 {
		capacity = 0
	}
	out := make([]domain.Role, 0, capacity)
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历角色失败: %w", err)
	}
	return out, nil
}

func scanRole(row scanner) (*domain.Role, error) {
	var (
		role        = &domain.Role{}
		description sql.NullString
	)
	err := row.Scan(&role.ID, &role.Code, &role.Name, &description, &role.Status,
		&role.CreatedAt, &role.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("扫描角色失败: %w", err)
	}
	role.Description = nullableString(description)
	return role, nil
}
