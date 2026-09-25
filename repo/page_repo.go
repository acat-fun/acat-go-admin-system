package repo

import (
	"context"
	"database/sql"
	"github.com/acat-fun/acat-go-common/mysqlx"
	"strings"
	"time"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

// MySQLPageRepo 是 PageRepo 的 MySQL 实现。
type MySQLPageRepo struct {
	db DBTX
}

// NewPageRepo 构造 MySQL 实现。
func NewPageRepo(db DBTX) *MySQLPageRepo { return &MySQLPageRepo{db: db} }

const pageSelectColumns = `uc.id, uc.code, COALESCE(label.label_value, uc.name) AS name,
	uc.type, uc.path, uc.icon, uc.parent_id, uc.sort_order,
	uc.scope, uc.is_enabled, uc.frontend_module_code, uc.route_key,
	uc.created_at, uc.updated_at, uc.version`

const pageI18nJoin = `LEFT JOIN t_acat_i18n_label label
		ON label.source_table = 'acat_user.t_acat_page'
	       AND label.source_field = 'name'
	       AND label.table_data_id = uc.id
	       AND label.i18n_code = ?
	       AND label.is_deleted = 0`

func scanPageRecord(scan func(dest ...any) error) (*domain.PageRecord, error) {
	var (
		record     domain.PageRecord
		path       sql.NullString
		icon       sql.NullString
		parentID   sql.NullString
		moduleCode sql.NullString
		routeKey   sql.NullString
	)
	if err := scan(&record.ID, &record.Code, &record.Name, &record.Type, &path, &icon, &parentID,
		&record.SortOrder, &record.Scope, &record.IsEnabled, &moduleCode, &routeKey,
		&record.CreatedAt, &record.UpdatedAt, &record.Version); err != nil {
		return nil, err
	}
	record.Path = mysqlx.NullString(path)
	record.Icon = mysqlx.NullString(icon)
	record.ParentID = mysqlx.NullString(parentID)
	record.FrontendModuleCode = mysqlx.NullString(moduleCode)
	record.RouteKey = mysqlx.NullString(routeKey)
	return &record, nil
}

func collectPageRecords(rows *sql.Rows) ([]domain.PageRecord, error) {
	defer func() { _ = rows.Close() }()
	out := make([]domain.PageRecord, 0, 32)
	for rows.Next() {
		record, err := scanPageRecord(rows.Scan)
		if err != nil {
			return nil, wrapError("扫描页面失败", err)
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError("遍历页面失败", err)
	}
	return out, nil
}

// ListPagesByScope 实现 PageRepo（AdminPageMapper.selectByScope）。
func (r *MySQLPageRepo) ListPagesByScope(ctx context.Context, scope int, i18nCode string) ([]domain.PageRecord, error) {
	query := `SELECT ` + pageSelectColumns + `
		FROM t_acat_page uc
		` + pageI18nJoin + `
		WHERE uc.scope = ? AND uc.is_deleted = 0
		ORDER BY uc.sort_order ASC, uc.id ASC`
	rows, err := r.db.QueryContext(ctx, query, i18nCode, scope)
	if err != nil {
		return nil, wrapError("查询页面失败", err)
	}
	return collectPageRecords(rows)
}

// ListPagesByParentID 实现 PageRepo（AdminPageMapper.selectByParentId）。
func (r *MySQLPageRepo) ListPagesByParentID(ctx context.Context, parentID, i18nCode string) ([]domain.PageRecord, error) {
	query := `SELECT ` + pageSelectColumns + `
		FROM t_acat_page uc
		` + pageI18nJoin + `
		WHERE uc.parent_id = ? AND uc.is_deleted = 0
		ORDER BY uc.sort_order ASC, uc.id ASC`
	rows, err := r.db.QueryContext(ctx, query, i18nCode, parentID)
	if err != nil {
		return nil, wrapError("查询子页面失败", err)
	}
	return collectPageRecords(rows)
}

const pagePlainColumns = `id, code, name, type, path, icon, parent_id, sort_order, scope,
	is_enabled, frontend_module_code, route_key, created_at, updated_at, version`

func (r *MySQLPageRepo) scanPlainPage(ctx context.Context, query string, args ...any) (*domain.PageRecord, error) {
	var (
		record     domain.PageRecord
		path       sql.NullString
		icon       sql.NullString
		parentID   sql.NullString
		moduleCode sql.NullString
		routeKey   sql.NullString
	)
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&record.ID, &record.Code, &record.Name, &record.Type, &path, &icon, &parentID,
		&record.SortOrder, &record.Scope, &record.IsEnabled, &moduleCode, &routeKey,
		&record.CreatedAt, &record.UpdatedAt, &record.Version)
	if err != nil {
		return nil, wrapError("查询页面失败", err)
	}
	record.Path = mysqlx.NullString(path)
	record.Icon = mysqlx.NullString(icon)
	record.ParentID = mysqlx.NullString(parentID)
	record.FrontendModuleCode = mysqlx.NullString(moduleCode)
	record.RouteKey = mysqlx.NullString(routeKey)
	return &record, nil
}

// FindPageByID 实现 PageRepo（selectById → WHERE id=? AND is_deleted=0）。
func (r *MySQLPageRepo) FindPageByID(ctx context.Context, id string) (*domain.PageRecord, error) {
	query := `SELECT ` + pagePlainColumns + ` FROM t_acat_page WHERE id = ? AND is_deleted = 0`
	return r.scanPlainPage(ctx, query, id)
}

// FindPageByCode 实现 PageRepo（AdminPageMapper.selectByCode，LIMIT 1）。
func (r *MySQLPageRepo) FindPageByCode(ctx context.Context, code string) (*domain.PageRecord, error) {
	query := `SELECT ` + pagePlainColumns + ` FROM t_acat_page WHERE code = ? AND is_deleted = 0 LIMIT 1`
	return r.scanPlainPage(ctx, query, code)
}

// FindDeletedPageByCode 实现 PageRepo（AdminPageMapper.selectDeletedByCode，LIMIT 1）。
func (r *MySQLPageRepo) FindDeletedPageByCode(ctx context.Context, code string) (*domain.PageRecord, error) {
	query := `SELECT ` + pagePlainColumns + ` FROM t_acat_page WHERE code = ? AND is_deleted = 1 LIMIT 1`
	record, err := r.scanPlainPage(ctx, query, code)
	if err != nil {
		return nil, err
	}
	record.IsDeleted = 1
	return record, nil
}

// RestoreDeletedPage 实现 PageRepo（AdminPageMapper.restoreDeleted，手写 SQL 绕过逻辑删除条件）。
func (r *MySQLPageRepo) RestoreDeletedPage(ctx context.Context, record domain.PageRecord) (int64, error) {
	const query = `UPDATE t_acat_page
		SET is_deleted = 0, code = ?, name = ?, type = ?, path = ?, icon = ?, parent_id = ?,
		    sort_order = ?, scope = ?, is_enabled = ?, frontend_module_code = ?, route_key = ?,
		    updated_at = ?
		WHERE id = ? AND is_deleted = 1`
	result, err := r.db.ExecContext(ctx, query,
		record.Code, record.Name, record.Type, mysqlx.Arg(record.Path), mysqlx.Arg(record.Icon),
		mysqlx.Arg(record.ParentID), record.SortOrder, record.Scope, record.IsEnabled,
		mysqlx.Arg(record.FrontendModuleCode), mysqlx.Arg(record.RouteKey), domain.Now(), record.ID)
	if err != nil {
		return 0, wrapError("恢复页面失败", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, wrapError("恢复页面失败", err)
	}
	return affected, nil
}

// InsertPage 实现 PageRepo（myInsert：is_deleted=0 + 审计字段由 service 填充）。
func (r *MySQLPageRepo) InsertPage(ctx context.Context, record domain.PageRecord) error {
	const query = `INSERT INTO t_acat_page
		(id, code, name, type, path, icon, parent_id, sort_order, scope, is_enabled,
		 frontend_module_code, route_key, is_deleted, create_by, update_by, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, 0)`
	_, err := r.db.ExecContext(ctx, query,
		record.ID, record.Code, record.Name, record.Type, mysqlx.Arg(record.Path), mysqlx.Arg(record.Icon),
		mysqlx.Arg(record.ParentID), record.SortOrder, record.Scope, record.IsEnabled,
		mysqlx.Arg(record.FrontendModuleCode), mysqlx.Arg(record.RouteKey),
		record.CreateBy, record.UpdateBy, record.CreatedAt, record.UpdatedAt)
	return wrapError("新增页面失败", err)
}

// UpdatePage 实现 PageRepo（myUpdate + 乐观锁）。
func (r *MySQLPageRepo) UpdatePage(ctx context.Context, record domain.PageRecord) (int64, error) {
	const query = `UPDATE t_acat_page
		SET code = ?, name = ?, type = ?, path = ?, icon = ?, parent_id = ?, sort_order = ?,
		    scope = ?, is_enabled = ?, frontend_module_code = ?, route_key = ?,
		    created_at = ?, update_by = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND version = ? AND is_deleted = 0`
	result, err := r.db.ExecContext(ctx, query,
		record.Code, record.Name, record.Type, mysqlx.Arg(record.Path), mysqlx.Arg(record.Icon),
		mysqlx.Arg(record.ParentID), record.SortOrder, record.Scope, record.IsEnabled,
		mysqlx.Arg(record.FrontendModuleCode), mysqlx.Arg(record.RouteKey),
		record.CreatedAt, record.UpdateBy, record.UpdatedAt, record.ID, record.Version)
	if err != nil {
		return 0, wrapError("更新页面失败", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, wrapError("更新页面失败", err)
	}
	return affected, nil
}

// SoftDeletePage 实现 PageRepo（deleteById）。
func (r *MySQLPageRepo) SoftDeletePage(ctx context.Context, id string) (int64, error) {
	const query = `UPDATE t_acat_page SET is_deleted = 1, updated_at = ? WHERE id = ? AND is_deleted = 0`
	result, err := r.db.ExecContext(ctx, query, domain.Now(), id)
	if err != nil {
		return 0, wrapError("删除页面失败", err)
	}
	return result.RowsAffected()
}

// SelectPageIDsByPermissions 实现 PageRepo（AdminPageMapper.selectPageIdsByPermissions）。
func (r *MySQLPageRepo) SelectPageIDsByPermissions(ctx context.Context, permissions []string) ([]string, error) {
	if len(permissions) == 0 {
		return []string{}, nil
	}
	args := make([]any, 0, len(permissions))
	for _, permission := range permissions {
		args = append(args, permission)
	}
	query := `SELECT id FROM t_acat_page WHERE code IN (` + mysqlx.Placeholders(len(permissions)) + `) AND is_deleted = 0`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapError("查询页面权限失败", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]string, 0, 16)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, wrapError("扫描页面权限失败", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError("遍历页面权限失败", err)
	}
	return out, nil
}

// InsertPageHistory 实现 PageRepo（AdminPageHistoryMapper.insert）。
//
// id 由 IdType.ASSIGN_UUID 生成：32 位无连字符 UUID（与 UUID v7 的 36 位带连字符不同）。
func (r *MySQLPageRepo) InsertPageHistory(ctx context.Context, record domain.PageHistoryRecord) error {
	const query = `INSERT INTO t_acat_page_history
		(id, page_id, code, name, type, path, icon, parent_id, sort_order, scope, is_enabled,
		 frontend_module_code, route_key, is_deleted, create_by, update_by, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, query,
		newCompactID(), mysqlx.Arg(&record.PageID), mysqlx.Arg(&record.Code), mysqlx.Arg(&record.Name),
		mysqlx.IntArg(record.Type), mysqlx.Arg(record.Path), mysqlx.Arg(record.Icon), mysqlx.Arg(record.ParentID),
		mysqlx.IntArg(record.SortOrder), mysqlx.IntArg(record.Scope), mysqlx.IntArg(record.IsEnabled),
		mysqlx.Arg(record.FrontendModuleCode), mysqlx.Arg(record.RouteKey), mysqlx.IntArg(record.IsDeleted),
		mysqlx.Arg(record.CreateBy), mysqlx.Arg(record.UpdateBy), timeArg(record.CreatedAt),
		record.UpdatedAt, mysqlx.IntArg(record.Version))
	return wrapError("写入页面历史失败", err)
}

// GrantPageToRootAndAdmin 实现 PageRepo（AdminPageMapper.grantPermissionToRootAndAdmin，幂等）。
func (r *MySQLPageRepo) GrantPageToRootAndAdmin(ctx context.Context, pageID string) error {
	const query = `INSERT INTO t_acat_role_permission (role_id, permission_id, resource_type)
		SELECT r.id, ?, 0
		FROM t_acat_role r
		WHERE r.code IN ('root', 'admin')
		  AND r.is_deleted = 0
		ON DUPLICATE KEY UPDATE
			resource_type = 0,
			is_deleted = 0,
			updated_at = NOW()`
	if _, err := r.db.ExecContext(ctx, query, pageID); err != nil {
		return wrapError("同步页面权限失败", err)
	}
	return nil
}

// RestoreRolePermissionsByPageID 实现 PageRepo（AdminPageMapper.restoreRolePermissionsByPageId）。
func (r *MySQLPageRepo) RestoreRolePermissionsByPageID(ctx context.Context, pageID string) (int64, error) {
	const query = `UPDATE t_acat_role_permission
		SET is_deleted = 0, resource_type = 0, updated_at = NOW()
		WHERE permission_id = ? AND resource_type = 0 AND is_deleted = 1`
	result, err := r.db.ExecContext(ctx, query, pageID)
	if err != nil {
		return 0, wrapError("恢复页面授权失败", err)
	}
	return result.RowsAffected()
}

// DeleteRolePermissionsByPageID 实现 PageRepo（AdminPageMapper.deleteRolePermissionsByPageId）。
func (r *MySQLPageRepo) DeleteRolePermissionsByPageID(ctx context.Context, pageID string) (int64, error) {
	const query = `UPDATE t_acat_role_permission
		SET is_deleted = 1, updated_at = NOW()
		WHERE permission_id = ? AND resource_type = 0 AND is_deleted = 0`
	result, err := r.db.ExecContext(ctx, query, pageID)
	if err != nil {
		return 0, wrapError("清理页面授权失败", err)
	}
	return result.RowsAffected()
}

// newCompactID 生成 32 位无连字符 UUID。
func newCompactID() string {
	return strings.ReplaceAll(domain.NewID(), "-", "")
}

func timeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}
