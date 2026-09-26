package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/acat-fun/acat-go-common/mysqlx"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
)

// MySQLDictRepo 是 DictRepo + LabelRepo 的 MySQL 实现。
type MySQLDictRepo struct {
	db DBTX
}

// NewDictRepo 构造 MySQL 实现（同一实例同时提供 DictRepo 与 LabelRepo 能力）。
func NewDictRepo(db DBTX) *MySQLDictRepo { return &MySQLDictRepo{db: db} }

// dictColumns 是查询列清单，不含 is_deleted/create_by/update_by。
const dictColumns = "id, code, name, is_enabled, is_tree, scope, description, is_builtin, created_at, updated_at, version"

// dictDataColumns 是数据项查询列清单，不含 is_deleted/create_by/update_by。
const dictDataColumns = "id, dict_id, parent_id, code, name, value, age_level, sort_order, is_enabled, description, is_builtin, color, created_at, updated_at, version"

func scanDictRow(scan func(dest ...any) error) (*domain.DictRecord, error) {
	var (
		record      domain.DictRecord
		description sql.NullString
	)
	if err := scan(&record.ID, &record.Code, &record.Name, &record.IsEnabled, &record.IsTree,
		&record.Scope, &description, &record.IsBuiltin, &record.CreatedAt, &record.UpdatedAt, &record.Version); err != nil {
		return nil, err
	}
	record.Description = mysqlx.NullString(description)
	return &record, nil
}

// ListDicts 实现 DictRepo。
func (r *MySQLDictRepo) ListDicts(ctx context.Context, filter domain.DictFilter, pageIndex, pageSize int) ([]domain.DictRecord, int64, error) {
	where := " WHERE is_deleted = 0"
	args := make([]any, 0, 4)
	if trimmed := trimSpace(filter.Name); trimmed != "" {
		where += " AND name LIKE ?"
		args = append(args, "%"+trimmed+"%")
	}
	if filter.IsEnabled != nil {
		where += " AND is_enabled = ?"
		args = append(args, *filter.IsEnabled)
	}
	if filter.IsTree != nil {
		where += " AND is_tree = ?"
		args = append(args, *filter.IsTree)
	}
	if filter.Scope != nil {
		where += " AND scope = ?"
		args = append(args, *filter.Scope)
	}

	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM t_acat_dict"+where, args...).Scan(&total); err != nil {
		return nil, 0, wrapError("统计字典失败", err)
	}

	query := "SELECT " + dictColumns + " FROM t_acat_dict" + where + " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	rows, err := r.db.QueryContext(ctx, query, append(args, pageSize, offset(pageIndex, pageSize))...)
	if err != nil {
		return nil, 0, wrapError("查询字典失败", err)
	}
	return collectDictRows(rows, total)
}

// ListAllDicts 实现 DictRepo。
func (r *MySQLDictRepo) ListAllDicts(ctx context.Context) ([]domain.DictRecord, error) {
	query := "SELECT " + dictColumns + " FROM t_acat_dict WHERE is_deleted = 0 ORDER BY created_at DESC"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, wrapError("查询字典失败", err)
	}
	list, _, err := collectDictRows(rows, 0)
	return list, err
}

// ListDictsForSelect 实现 DictRepo。
func (r *MySQLDictRepo) ListDictsForSelect(ctx context.Context) ([]domain.DictRecord, error) {
	query := "SELECT " + dictColumns + " FROM t_acat_dict WHERE is_deleted = 0"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, wrapError("查询字典失败", err)
	}
	list, _, err := collectDictRows(rows, 0)
	return list, err
}

func collectDictRows(rows *sql.Rows, total int64) ([]domain.DictRecord, int64, error) {
	defer func() { _ = rows.Close() }()
	out := make([]domain.DictRecord, 0, 16)
	for rows.Next() {
		record, err := scanDictRow(rows.Scan)
		if err != nil {
			return nil, 0, wrapError("扫描字典失败", err)
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, wrapError("遍历字典失败", err)
	}
	if total == 0 {
		total = int64(len(out))
	}
	return out, total, nil
}

// CountDataItemsByDictID 实现 DictRepo。
func (r *MySQLDictRepo) CountDataItemsByDictID(ctx context.Context, dictID string) (int64, error) {
	var count int64
	query := "SELECT COUNT(*) FROM t_acat_dict_data WHERE is_deleted = 0 AND dict_id = ?"
	if err := r.db.QueryRowContext(ctx, query, dictID).Scan(&count); err != nil {
		return 0, wrapError("统计数据项失败", err)
	}
	return count, nil
}

// FindDictByID 实现 DictRepo（WHERE id=? AND is_deleted=0）。
func (r *MySQLDictRepo) FindDictByID(ctx context.Context, id string) (*domain.DictRecord, error) {
	query := "SELECT " + dictColumns + " FROM t_acat_dict WHERE id = ? AND is_deleted = 0"
	record, err := scanDictRow(r.db.QueryRowContext(ctx, query, id).Scan)
	if err != nil {
		return nil, wrapError("查询字典失败", err)
	}
	return record, nil
}

// FindDictByCode 实现 DictRepo（WHERE code=? AND is_deleted=0）。
//
// 匹配到多条时返回 ErrMultipleResults。
func (r *MySQLDictRepo) FindDictByCode(ctx context.Context, code string) (*domain.DictRecord, error) {
	query := "SELECT " + dictColumns + " FROM t_acat_dict WHERE code = ? AND is_deleted = 0 LIMIT 2"
	rows, err := r.db.QueryContext(ctx, query, code)
	if err != nil {
		return nil, wrapError("查询字典失败", err)
	}
	defer func() { _ = rows.Close() }()
	records := make([]domain.DictRecord, 0, 2)
	for rows.Next() {
		record, scanErr := scanDictRow(rows.Scan)
		if scanErr != nil {
			return nil, wrapError("扫描字典失败", scanErr)
		}
		records = append(records, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError("遍历字典失败", err)
	}
	switch len(records) {
	case 0:
		return nil, ErrNotFound
	case 1:
		return &records[0], nil
	default:
		return nil, ErrMultipleResults
	}
}

// CountDictsByCode 实现 DictRepo（新增字典的唯一性预检）。
func (r *MySQLDictRepo) CountDictsByCode(ctx context.Context, code string) (int64, error) {
	var count int64
	query := "SELECT COUNT(*) FROM t_acat_dict WHERE is_deleted = 0 AND code = ?"
	if err := r.db.QueryRowContext(ctx, query, code).Scan(&count); err != nil {
		return 0, wrapError("统计字典编码失败", err)
	}
	return count, nil
}

// InsertDict 实现 DictRepo。
//
// 这里由 service 组装完整 record（含审计字段）后一次性写入。
func (r *MySQLDictRepo) InsertDict(ctx context.Context, record domain.DictRecord) error {
	const query = `INSERT INTO t_acat_dict
		(id, code, name, is_enabled, is_tree, scope, description, is_builtin, is_deleted,
		 create_by, update_by, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, 0)`
	_, err := r.db.ExecContext(ctx, query,
		record.ID, record.Code, record.Name, record.IsEnabled, record.IsTree, record.Scope,
		mysqlx.Arg(record.Description), record.IsBuiltin,
		record.CreateBy, record.UpdateBy, record.CreatedAt, record.UpdatedAt)
	return wrapError("新增字典失败", err)
}

// UpdateDict 实现 DictRepo（乐观锁：WHERE id=? AND version=? AND is_deleted=0）。
//
// 返回影响行数。
func (r *MySQLDictRepo) UpdateDict(ctx context.Context, record domain.DictRecord) (int64, error) {
	const query = `UPDATE t_acat_dict
		SET code = ?, name = ?, is_enabled = ?, is_tree = ?, scope = ?, description = ?,
		    is_builtin = ?, created_at = ?, update_by = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND version = ? AND is_deleted = 0`
	result, err := r.db.ExecContext(ctx, query,
		record.Code, record.Name, record.IsEnabled, record.IsTree, record.Scope,
		mysqlx.Arg(record.Description), record.IsBuiltin, record.CreatedAt, record.UpdateBy, record.UpdatedAt,
		record.ID, record.Version)
	if err != nil {
		return 0, wrapError("更新字典失败", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, wrapError("更新字典失败", err)
	}
	return affected, nil
}

// SoftDeleteDict 实现 DictRepo（逻辑删除：SET is_deleted=1；内置行不参与删除，配合服务层判定兜底）。
func (r *MySQLDictRepo) SoftDeleteDict(ctx context.Context, id string) (int64, error) {
	const query = `UPDATE t_acat_dict SET is_deleted = 1, updated_at = ?
		WHERE id = ? AND is_deleted = 0 AND is_builtin = 0`
	result, err := r.db.ExecContext(ctx, query, domain.Now(), id)
	if err != nil {
		return 0, wrapError("删除字典失败", err)
	}
	return result.RowsAffected()
}

// ---------------------------------------------------------------------------
// 字典数据项
// ---------------------------------------------------------------------------

func scanDictDataRow(scan func(dest ...any) error) (*domain.DictDataRecord, error) {
	var (
		record      domain.DictDataRecord
		parentID    sql.NullString
		ageLevel    sql.NullInt64
		description sql.NullString
	)
	if err := scan(&record.ID, &record.DictID, &parentID, &record.Code, &record.Name, &record.Value,
		&ageLevel, &record.SortOrder, &record.IsEnabled, &description, &record.IsBuiltin, &record.Color,
		&record.CreatedAt, &record.UpdatedAt, &record.Version); err != nil {
		return nil, err
	}
	record.ParentID = mysqlx.NullString(parentID)
	if ageLevel.Valid {
		record.AgeLevel = int(ageLevel.Int64)
	}
	record.Description = mysqlx.NullString(description)
	return &record, nil
}

// ListDataItems 实现 DictRepo。
func (r *MySQLDictRepo) ListDataItems(ctx context.Context, dictID string, filter domain.DictDataFilter, pageIndex, pageSize int) ([]domain.DictDataRecord, int64, error) {
	where := " WHERE is_deleted = 0 AND dict_id = ?"
	args := []any{dictID}
	if trimmed := trimSpace(filter.Name); trimmed != "" {
		where += " AND (name LIKE ? OR code LIKE ?)"
		args = append(args, "%"+trimmed+"%", "%"+trimmed+"%")
	}

	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM t_acat_dict_data"+where, args...).Scan(&total); err != nil {
		return nil, 0, wrapError("统计数据项失败", err)
	}

	query := "SELECT " + dictDataColumns + " FROM t_acat_dict_data" + where + " ORDER BY sort_order ASC, id ASC LIMIT ? OFFSET ?"
	rows, err := r.db.QueryContext(ctx, query, append(args, pageSize, offset(pageIndex, pageSize))...)
	if err != nil {
		return nil, 0, wrapError("查询数据项失败", err)
	}
	list, err := collectDictDataRows(rows)
	return list, total, err
}

// ListAllDataItems 实现 DictRepo。
func (r *MySQLDictRepo) ListAllDataItems(ctx context.Context, dictID string, isEnabled *int) ([]domain.DictDataRecord, error) {
	query := "SELECT " + dictDataColumns + " FROM t_acat_dict_data WHERE dict_id = ? AND is_deleted = 0"
	args := []any{dictID}
	if isEnabled != nil {
		query += " AND is_enabled = ?"
		args = append(args, *isEnabled)
	}
	query += " ORDER BY sort_order ASC, id ASC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapError("查询数据项失败", err)
	}
	return collectDictDataRows(rows)
}

func collectDictDataRows(rows *sql.Rows) ([]domain.DictDataRecord, error) {
	defer func() { _ = rows.Close() }()
	out := make([]domain.DictDataRecord, 0, 16)
	for rows.Next() {
		record, err := scanDictDataRow(rows.Scan)
		if err != nil {
			return nil, wrapError("扫描数据项失败", err)
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError("遍历数据项失败", err)
	}
	return out, nil
}

// FindDataItemByID 实现 DictRepo（WHERE id=? AND is_deleted=0）。
func (r *MySQLDictRepo) FindDataItemByID(ctx context.Context, id string) (*domain.DictDataRecord, error) {
	query := "SELECT " + dictDataColumns + " FROM t_acat_dict_data WHERE id = ? AND is_deleted = 0"
	record, err := scanDictDataRow(r.db.QueryRowContext(ctx, query, id).Scan)
	if err != nil {
		return nil, wrapError("查询数据项失败", err)
	}
	return record, nil
}

// ListChildDataItems 实现 DictRepo（WHERE dict_id=? AND parent_id=? AND is_deleted=0）。
func (r *MySQLDictRepo) ListChildDataItems(ctx context.Context, dictID, parentID string) ([]domain.DictDataRecord, error) {
	query := "SELECT " + dictDataColumns +
		" FROM t_acat_dict_data WHERE is_deleted = 0 AND dict_id = ? AND parent_id = ?"
	rows, err := r.db.QueryContext(ctx, query, dictID, parentID)
	if err != nil {
		return nil, wrapError("查询子数据项失败", err)
	}
	return collectDictDataRows(rows)
}

// InsertDataItem 实现 DictRepo。
func (r *MySQLDictRepo) InsertDataItem(ctx context.Context, record domain.DictDataRecord) error {
	const query = `INSERT INTO t_acat_dict_data
		(id, dict_id, parent_id, code, name, value, age_level, sort_order, is_enabled, description,
		 is_builtin, color, is_deleted, create_by, update_by, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, 0)`
	_, err := r.db.ExecContext(ctx, query,
		record.ID, record.DictID, mysqlx.Arg(record.ParentID), record.Code, record.Name, record.Value, record.AgeLevel,
		record.SortOrder, record.IsEnabled, mysqlx.Arg(record.Description), record.IsBuiltin, record.Color,
		record.CreateBy, record.UpdateBy, record.CreatedAt, record.UpdatedAt)
	return wrapError("新增数据项失败", err)
}

// UpdateDataItem 实现 DictRepo（乐观锁：WHERE id=? AND version=? AND is_deleted=0）。
func (r *MySQLDictRepo) UpdateDataItem(ctx context.Context, record domain.DictDataRecord) (int64, error) {
	const query = `UPDATE t_acat_dict_data
		SET dict_id = ?, parent_id = ?, code = ?, name = ?, value = ?, sort_order = ?,
			is_enabled = ?, age_level = ?, description = ?, is_builtin = ?, color = ?,
			created_at = ?, update_by = ?, updated_at = ?,
		    version = version + 1
		WHERE id = ? AND version = ? AND is_deleted = 0`
	result, err := r.db.ExecContext(ctx, query,
		record.DictID, mysqlx.Arg(record.ParentID), record.Code, record.Name, record.Value,
		record.SortOrder, record.IsEnabled, record.AgeLevel, mysqlx.Arg(record.Description),
		record.IsBuiltin, record.Color, record.CreatedAt,
		record.UpdateBy, record.UpdatedAt, record.ID, record.Version)
	if err != nil {
		return 0, wrapError("更新数据项失败", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, wrapError("更新数据项失败", err)
	}
	return affected, nil
}

// UpdateDataItemByID 实现 DictRepo（批量保存路径：不带 version 条件，仅 WHERE id=? AND is_deleted=0）。
func (r *MySQLDictRepo) UpdateDataItemByID(ctx context.Context, record domain.DictDataRecord) (int64, error) {
	const query = `UPDATE t_acat_dict_data
		SET dict_id = ?, parent_id = ?, code = ?, name = ?, value = ?, sort_order = ?,
			is_enabled = ?, age_level = ?, description = ?, is_builtin = ?, color = ?,
			created_at = ?, update_by = ?, updated_at = ?,
		    version = version + 1
		WHERE id = ? AND is_deleted = 0`
	result, err := r.db.ExecContext(ctx, query,
		record.DictID, mysqlx.Arg(record.ParentID), record.Code, record.Name, record.Value,
		record.SortOrder, record.IsEnabled, record.AgeLevel, mysqlx.Arg(record.Description),
		record.IsBuiltin, record.Color, record.CreatedAt,
		record.UpdateBy, record.UpdatedAt, record.ID)
	if err != nil {
		return 0, wrapError("更新数据项失败", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, wrapError("更新数据项失败", err)
	}
	return affected, nil
}

// SoftDeleteDataItem 实现 DictRepo（逻辑删除并刷新 updated_at；内置行不参与删除，配合服务层判定兜底）。
func (r *MySQLDictRepo) SoftDeleteDataItem(ctx context.Context, id string) (int64, error) {
	const query = `UPDATE t_acat_dict_data SET is_deleted = 1, updated_at = ?
		WHERE id = ? AND is_deleted = 0 AND is_builtin = 0`
	result, err := r.db.ExecContext(ctx, query, domain.Now(), id)
	if err != nil {
		return 0, wrapError("删除数据项失败", err)
	}
	return result.RowsAffected()
}

// ---------------------------------------------------------------------------
// t_acat_i18n_label
// ---------------------------------------------------------------------------

// ListNameLabels 实现 LabelRepo（按来源查询，ORDER BY i18n_code ASC）。
func (r *MySQLDictRepo) ListNameLabels(ctx context.Context, sourceTable, tableDataID string) ([]domain.LabelRecord, error) {
	const query = `SELECT i18n_code, label_value FROM t_acat_i18n_label
		WHERE source_table = ? AND source_field = 'name' AND table_data_id = ? AND is_deleted = 0
		ORDER BY i18n_code ASC`
	rows, err := r.db.QueryContext(ctx, query, sourceTable, tableDataID)
	if err != nil {
		return nil, wrapError("查询国际化标签失败", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]domain.LabelRecord, 0, 4)
	for rows.Next() {
		var record domain.LabelRecord
		if err := rows.Scan(&record.I18nCode, &record.LabelValue); err != nil {
			return nil, wrapError("扫描国际化标签失败", err)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError("遍历国际化标签失败", err)
	}
	return out, nil
}

// ResolveCurrentName 实现 LabelRepo（按来源 + 语言取单条，LIMIT 1）。
func (r *MySQLDictRepo) ResolveCurrentName(ctx context.Context, sourceTable, tableDataID, i18nCode string) (string, error) {
	const query = `SELECT label_value FROM t_acat_i18n_label
		WHERE source_table = ? AND source_field = 'name' AND table_data_id = ?
		  AND i18n_code = ? AND is_deleted = 0
		LIMIT 1`
	var value string
	err := r.db.QueryRowContext(ctx, query, sourceTable, tableDataID, i18nCode).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", wrapError("查询国际化标签失败", err)
	}
	return value, nil
}

// SaveNameLabels 实现 LabelRepo（幂等 upsert：ON DUPLICATE KEY UPDATE 恢复软删并刷新值）。
func (r *MySQLDictRepo) SaveNameLabels(ctx context.Context, sourceTable, tableDataID string, values []domain.I18nValue) error {
	if len(values) == 0 {
		return nil
	}
	const query = `INSERT INTO t_acat_i18n_label
		(id, i18n_code, label_value, source_table, source_field, table_data_id,
		 is_deleted, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, 'name', ?, 0, ?, ?, 0)
		ON DUPLICATE KEY UPDATE
			label_value = VALUES(label_value),
			is_deleted = 0,
			updated_at = VALUES(updated_at)`
	now := domain.Now()
	for _, value := range values {
		if trimSpace(value.I18n) == "" || trimSpace(value.Value) == "" {
			continue
		}
		if _, err := r.db.ExecContext(ctx, query,
			domain.NewID(), value.I18n, value.Value, sourceTable, tableDataID, now, now); err != nil {
			return wrapError("写入国际化标签失败", err)
		}
	}
	return nil
}

// DeleteNameLabels 实现 LabelRepo（按来源软删除全部标签）。
func (r *MySQLDictRepo) DeleteNameLabels(ctx context.Context, sourceTable, tableDataID string) (int64, error) {
	const query = `UPDATE t_acat_i18n_label SET is_deleted = 1, updated_at = ?
		WHERE source_table = ? AND source_field = 'name' AND table_data_id = ? AND is_deleted = 0`
	result, err := r.db.ExecContext(ctx, query, domain.Now(), sourceTable, tableDataID)
	if err != nil {
		return 0, wrapError("删除国际化标签失败", err)
	}
	return result.RowsAffected()
}

// ListFrontendLabels 实现 LabelRepo（字典项 ⋈ 标签：仅取启用字典与启用数据项，ORDER BY sort_order、code）。
func (r *MySQLDictRepo) ListFrontendLabels(ctx context.Context, dictCode, i18nCode string) ([]domain.FrontendLabelRecord, error) {
	const query = `
		SELECT data.code AS table_data_id,
		       label.label_value AS label_value,
		       ? AS i18n_code,
		       'acat_user.t_acat_dict_data' AS source_table,
		       'name' AS source_field
		FROM t_acat_dict dict
		INNER JOIN t_acat_dict_data data
		        ON data.dict_id = dict.id
		       AND data.is_deleted = 0
		       AND data.is_enabled = 1
		INNER JOIN t_acat_i18n_label label
		        ON label.source_table = 'acat_user.t_acat_dict_data'
		       AND label.source_field = 'name'
		       AND label.table_data_id = data.id
		       AND label.i18n_code = ?
		       AND label.is_deleted = 0
		WHERE dict.code = ?
		  AND dict.is_deleted = 0
		  AND dict.is_enabled = 1
		ORDER BY data.sort_order ASC, data.code ASC`
	rows, err := r.db.QueryContext(ctx, query, i18nCode, i18nCode, dictCode)
	if err != nil {
		return nil, wrapError("查询前端国际化字典失败", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]domain.FrontendLabelRecord, 0, 64)
	for rows.Next() {
		var (
			record      domain.FrontendLabelRecord
			tableDataID string
			labelValue  string
			echoCode    string
			sourceTable string
			sourceField string
		)
		if err := rows.Scan(&tableDataID, &labelValue, &echoCode, &sourceTable, &sourceField); err != nil {
			return nil, wrapError("扫描前端国际化字典失败", err)
		}
		record.Code = tableDataID
		record.LabelValue = labelValue
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError("遍历前端国际化字典失败", err)
	}
	return out, nil
}

// offset 计算 SQL OFFSET（页码最小 1）。
func offset(pageIndex, pageSize int) int {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize < 0 {
		pageSize = 0
	}
	return (pageIndex - 1) * pageSize
}

// trimSpace 局部实现，避免与 domain 包循环依赖。
func trimSpace(value string) string {
	start := 0
	end := len(value)
	for start < end && isSpaceByte(value[start]) {
		start++
	}
	for end > start && isSpaceByte(value[end-1]) {
		end--
	}
	return value[start:end]
}

func isSpaceByte(char byte) bool {
	return char == ' ' || char == '\t' || char == '\n' || char == '\r' || char == '\v' || char == '\f'
}

var _ = fmt.Sprintf
