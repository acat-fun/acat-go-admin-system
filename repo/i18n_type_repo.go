package repo

import (
	"context"
	"database/sql"
	"strings"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

// MySQLI18nTypeRepo 是 I18nTypeRepo 的 MySQL 实现。
type MySQLI18nTypeRepo struct {
	db DBTX
}

// NewI18nTypeRepo 构造 MySQL 实现。
func NewI18nTypeRepo(db DBTX) *MySQLI18nTypeRepo { return &MySQLI18nTypeRepo{db: db} }

// i18nTypeColumns 与 MyBatis-Plus 默认查询列一致（排除 is_deleted/create_by/update_by）。
const i18nTypeColumns = "id, code, name, sort_order, is_enabled, created_at, updated_at, version"

func scanI18nType(scan func(dest ...any) error) (*domain.I18nTypeRecord, error) {
	var record domain.I18nTypeRecord
	if err := scan(&record.ID, &record.Code, &record.Name, &record.SortOrder, &record.IsEnabled,
		&record.CreatedAt, &record.UpdatedAt, &record.Version); err != nil {
		return nil, err
	}
	return &record, nil
}

// ListTypesPaged 实现 I18nTypeRepo。
func (r *MySQLI18nTypeRepo) ListTypesPaged(ctx context.Context, keyword string, isEnabled *int, pageIndex, pageSize int) ([]domain.I18nTypeRecord, int64, error) {
	where := " WHERE is_deleted = 0"
	args := []any{}
	if strings.TrimSpace(keyword) != "" {
		where += " AND (code LIKE ? OR name LIKE ?)"
		pattern := "%" + keyword + "%"
		args = append(args, pattern, pattern)
	}
	if isEnabled != nil {
		where += " AND is_enabled = ?"
		args = append(args, *isEnabled)
	}

	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM t_acat_i18n_type"+where, args...).Scan(&total); err != nil {
		return nil, 0, wrapError("统计语言类型失败", err)
	}
	query := "SELECT " + i18nTypeColumns + " FROM t_acat_i18n_type" + where +
		" ORDER BY sort_order ASC, code ASC LIMIT ? OFFSET ?"
	rows, err := r.db.QueryContext(ctx, query, append(args, pageSize, offset(pageIndex, pageSize))...)
	if err != nil {
		return nil, 0, wrapError("查询语言类型失败", err)
	}
	list, err := collectI18nTypes(rows)
	return list, total, err
}

// ListEnabledTypes 实现 I18nTypeRepo。
func (r *MySQLI18nTypeRepo) ListEnabledTypes(ctx context.Context) ([]domain.I18nTypeRecord, error) {
	query := "SELECT " + i18nTypeColumns +
		" FROM t_acat_i18n_type WHERE is_deleted = 0 AND is_enabled = 1 ORDER BY sort_order ASC, code ASC"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, wrapError("查询语言类型失败", err)
	}
	return collectI18nTypes(rows)
}

func collectI18nTypes(rows *sql.Rows) ([]domain.I18nTypeRecord, error) {
	defer func() { _ = rows.Close() }()
	out := make([]domain.I18nTypeRecord, 0, 8)
	for rows.Next() {
		record, err := scanI18nType(rows.Scan)
		if err != nil {
			return nil, wrapError("扫描语言类型失败", err)
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError("遍历语言类型失败", err)
	}
	return out, nil
}

// FindTypeByID 实现 I18nTypeRepo（selectById）。
func (r *MySQLI18nTypeRepo) FindTypeByID(ctx context.Context, id string) (*domain.I18nTypeRecord, error) {
	query := "SELECT " + i18nTypeColumns + " FROM t_acat_i18n_type WHERE id = ? AND is_deleted = 0"
	record, err := scanI18nType(r.db.QueryRowContext(ctx, query, id).Scan)
	if err != nil {
		return nil, wrapError("查询语言类型失败", err)
	}
	return record, nil
}

// CountTypesByCode 实现 I18nTypeRepo（createType 唯一性预检 selectCount）。
func (r *MySQLI18nTypeRepo) CountTypesByCode(ctx context.Context, code string) (int64, error) {
	var count int64
	const query = "SELECT COUNT(*) FROM t_acat_i18n_type WHERE is_deleted = 0 AND code = ?"
	if err := r.db.QueryRowContext(ctx, query, code).Scan(&count); err != nil {
		return 0, wrapError("统计语言编码失败", err)
	}
	return count, nil
}

// InsertType 实现 I18nTypeRepo（myInsert）。
func (r *MySQLI18nTypeRepo) InsertType(ctx context.Context, record domain.I18nTypeRecord) error {
	const query = `INSERT INTO t_acat_i18n_type
		(id, code, name, sort_order, is_enabled, is_deleted, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?, 0)`
	_, err := r.db.ExecContext(ctx, query, record.ID, record.Code, record.Name,
		record.SortOrder, record.IsEnabled, record.CreatedAt, record.UpdatedAt)
	return wrapError("新增语言类型失败", err)
}

// UpdateType 实现 I18nTypeRepo（myUpdate + 乐观锁，返回影响行数）。
func (r *MySQLI18nTypeRepo) UpdateType(ctx context.Context, record domain.I18nTypeRecord) (int64, error) {
	const query = `UPDATE t_acat_i18n_type
		SET code = ?, name = ?, sort_order = ?, is_enabled = ?,
		    created_at = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND version = ? AND is_deleted = 0`
	result, err := r.db.ExecContext(ctx, query, record.Code, record.Name, record.SortOrder,
		record.IsEnabled, record.CreatedAt, record.UpdatedAt, record.ID, record.Version)
	if err != nil {
		return 0, wrapError("更新语言类型失败", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, wrapError("更新语言类型失败", err)
	}
	return affected, nil
}

// SoftDeleteType 实现 I18nTypeRepo（deleteById。
func (r *MySQLI18nTypeRepo) SoftDeleteType(ctx context.Context, id string) (int64, error) {
	const query = `UPDATE t_acat_i18n_type SET is_deleted = 1, updated_at = ? WHERE id = ? AND is_deleted = 0`
	result, err := r.db.ExecContext(ctx, query, domain.Now(), id)
	if err != nil {
		return 0, wrapError("删除语言类型失败", err)
	}
	return result.RowsAffected()
}
