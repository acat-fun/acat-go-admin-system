package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
)

// ReaderUserRepo 提供读者用户（t_acat_user）管理数据访问。
//
// SQL 语句形状保持稳定（测试按字面断言）；。
type ReaderUserRepo interface {
	// List 分页查询（UserServiceImpl.listUsers）：keyword 对 username/email 做 LIKE，无排序（默认主键序）。
	List(ctx context.Context, keyword string, offset, limit int) ([]domain.ReaderUser, int64, error)
	// FindByUsername 按用户名查询未删除用户（UserMapper.selectByUsername）。
	FindByUsername(ctx context.Context, username string) (*domain.ReaderUser, error)
	// FindByID 按主键查询未删除用户（@TableLogic + selectById）。
	FindByID(ctx context.Context, id string) (*domain.ReaderUser, error)
	// Insert 新增（MyBaseMapper.myInsert：is_deleted=0 + created_at/updated_at）。
	Insert(ctx context.Context, user *domain.ReaderUser) error
	// Update 按主键更新（MyBaseMapper.myUpdate）。
	Update(ctx context.Context, user *domain.ReaderUser) (int64, error)
	// SoftDelete 软删除（MyBaseMapper.myDelete → is_deleted=1）。
	SoftDelete(ctx context.Context, id string) (int64, error)
	// RoleCodes 查询用户的启用角色编码（UserMapper.selectRolesByUserId）。
	RoleCodes(ctx context.Context, userID string) ([]string, error)
	// SoftDeleteRoles 软删除用户全部角色关联（UserMapper.deleteUserRoles）。
	SoftDeleteRoles(ctx context.Context, userID string) (int64, error)
	// InsertRoles 幂等插入角色关联（UserMapper.insertUserRoles，ON DUPLICATE KEY UPDATE is_deleted=0）。
	InsertRoles(ctx context.Context, userID string, roleIDs []string) (int64, error)
}

// MySQLReaderRepo 是 ReaderUserRepo 的 MySQL 实现。
type MySQLReaderRepo struct {
	db DBTX
}

// NewReaderRepo 构造 MySQL 实现。
func NewReaderRepo(db DBTX) *MySQLReaderRepo { return &MySQLReaderRepo{db: db} }

const readerColumns = "id, username, password, email, avatar, status, muted, age_level, created_at, updated_at"

// List 实现 ReaderUserRepo。
//
// 这里显式 ORDER BY id ASC 保证分页稳定且与观测结果一致。
func (r *MySQLReaderRepo) List(ctx context.Context, keyword string, offset, limit int) ([]domain.ReaderUser, int64, error) {
	where := " WHERE is_deleted = 0"
	args := make([]any, 0, 4)
	if keyword != "" {
		where += " AND (username LIKE ? OR email LIKE ?)"
		pattern := "%" + keyword + "%"
		args = append(args, pattern, pattern)
	}

	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM t_acat_user"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计读者用户失败: %w", err)
	}

	query := "SELECT " + readerColumns + " FROM t_acat_user" + where + " ORDER BY id ASC LIMIT ? OFFSET ?"
	rows, err := r.db.QueryContext(ctx, query, append(append([]any{}, args...), limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("查询读者用户失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.ReaderUser, 0, limit)
	for rows.Next() {
		user, scanErr := scanReaderUser(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		out = append(out, *user)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("遍历读者用户失败: %w", err)
	}
	return out, total, nil
}

// FindByUsername 实现 ReaderUserRepo（UserMapper.selectByUsername）。
func (r *MySQLReaderRepo) FindByUsername(ctx context.Context, username string) (*domain.ReaderUser, error) {
	query := "SELECT " + readerColumns + " FROM t_acat_user WHERE username = ? AND is_deleted = 0"
	return r.scanOne(ctx, query, username)
}

// FindByID 实现 ReaderUserRepo。
func (r *MySQLReaderRepo) FindByID(ctx context.Context, id string) (*domain.ReaderUser, error) {
	query := "SELECT " + readerColumns + " FROM t_acat_user WHERE id = ? AND is_deleted = 0"
	return r.scanOne(ctx, query, id)
}

func (r *MySQLReaderRepo) scanOne(ctx context.Context, query string, args ...any) (*domain.ReaderUser, error) {
	user, err := scanReaderUser(r.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return user, nil
}

// scanner 抽象 *sql.Row 与 *sql.Rows 的公共 Scan 能力。
type scanner interface {
	Scan(dest ...any) error
}

func scanReaderUser(row scanner) (*domain.ReaderUser, error) {
	var (
		user   = &domain.ReaderUser{}
		email  sql.NullString
		avatar sql.NullString
	)
	err := row.Scan(&user.ID, &user.Username, &user.Password, &email, &avatar,
		&user.Status, &user.Muted, &user.AgeLevel, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("扫描读者用户失败: %w", err)
	}
	user.Email = nullableString(email)
	user.Avatar = nullableString(avatar)
	return user, nil
}

// Insert 实现 ReaderUserRepo。
//
// 这里显式列出业务列，create_by/update_by/version 走数据库默认值。
func (r *MySQLReaderRepo) Insert(ctx context.Context, user *domain.ReaderUser) error {
	const query = `
		INSERT INTO t_acat_user
			(id, username, password, email, avatar, status, muted, age_level, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`
	if _, err := r.db.ExecContext(ctx, query, user.ID, user.Username, user.Password, user.Email, user.Avatar,
		user.Status, user.Muted, user.AgeLevel, user.CreatedAt, user.UpdatedAt); err != nil {
		return fmt.Errorf("新增读者用户失败: %w", err)
	}
	return nil
}

// Update 实现 ReaderUserRepo（updateById + @TableLogic 的 is_deleted=0 条件）。
func (r *MySQLReaderRepo) Update(ctx context.Context, user *domain.ReaderUser) (int64, error) {
	const query = `
		UPDATE t_acat_user
		SET username = ?, email = ?, status = ?, muted = ?, age_level = ?, updated_at = ?
		WHERE id = ? AND is_deleted = 0`
	result, execErr := r.db.ExecContext(ctx, query, user.Username, user.Email, user.Status, user.Muted,
		user.AgeLevel, user.UpdatedAt, user.ID)
	if execErr != nil {
		return 0, fmt.Errorf("更新读者用户失败: %w", execErr)
	}
	return result.RowsAffected()
}

// SoftDelete 实现 ReaderUserRepo（deleteById → @TableLogic 软删除）。
func (r *MySQLReaderRepo) SoftDelete(ctx context.Context, id string) (int64, error) {
	const query = `UPDATE t_acat_user SET is_deleted = 1 WHERE id = ? AND is_deleted = 0`
	result, execErr := r.db.ExecContext(ctx, query, id)
	if execErr != nil {
		return 0, fmt.Errorf("删除读者用户失败: %w", execErr)
	}
	return result.RowsAffected()
}

// RoleCodes 实现 ReaderUserRepo（UserMapper.selectRolesByUserId：仅启用角色）。
func (r *MySQLReaderRepo) RoleCodes(ctx context.Context, userID string) ([]string, error) {
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

// SoftDeleteRoles 实现 ReaderUserRepo（UserMapper.deleteUserRoles）。
func (r *MySQLReaderRepo) SoftDeleteRoles(ctx context.Context, userID string) (int64, error) {
	const query = `UPDATE t_acat_user_role SET is_deleted = 1 WHERE user_id = ? AND is_deleted = 0`
	result, execErr := r.db.ExecContext(ctx, query, userID)
	if execErr != nil {
		return 0, fmt.Errorf("软删除读者用户角色关联失败: %w", execErr)
	}
	return result.RowsAffected()
}

// InsertRoles 实现 ReaderUserRepo（UserMapper.insertUserRoles，幂等恢复软删记录）。
func (r *MySQLReaderRepo) InsertRoles(ctx context.Context, userID string, roleIDs []string) (int64, error) {
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
		return 0, fmt.Errorf("写入读者用户角色关联失败: %w", execErr)
	}
	return result.RowsAffected()
}

// nullableString 把可空列转换为指针（NULL → nil。
func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	out := value.String
	return &out
}

// queryStrings 查询单列字符串列表；无结果返回空切片。
func queryStrings(ctx context.Context, db DBTX, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询字符串列表失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]string, 0, 16)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("扫描字符串列表失败: %w", err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历字符串列表失败: %w", err)
	}
	return out, nil
}

// inPlaceholders 生成 "?, ?, ?" 形式的占位符。
func inPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?, ", count), ", ")
}

// stringsToArgs 把字符串切片转换为 []any，便于 IN 查询传参。
func stringsToArgs(values []string) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value)
	}
	return args
}
