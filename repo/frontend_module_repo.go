package repo

import (
	"context"
	"database/sql"
	"github.com/acat-fun/acat-go-common/mysqlx"
	"strings"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

// MySQLFrontendModuleRepo 是 FrontendModuleRepo 的 MySQL 实现。
type MySQLFrontendModuleRepo struct {
	db DBTX
}

// NewFrontendModuleRepo 构造 MySQL 实现。
func NewFrontendModuleRepo(db DBTX) *MySQLFrontendModuleRepo {
	return &MySQLFrontendModuleRepo{db: db}
}

// moduleColumns 是前端模块查询列清单，不含 is_deleted/create_by/update_by。
const moduleColumns = `id, module_code, name, release_version, contract_version, manifest_path,
	fallback_version, fallback_manifest_path, status, sort_order, created_at, updated_at, version`

func scanModuleRecord(scan func(dest ...any) error) (*domain.FrontendModuleRecord, error) {
	var (
		record       domain.FrontendModuleRecord
		fallbackVer  sql.NullString
		fallbackPath sql.NullString
	)
	if err := scan(&record.ID, &record.ModuleCode, &record.Name, &record.ReleaseVersion,
		&record.ContractVersion, &record.ManifestPath, &fallbackVer, &fallbackPath,
		&record.Status, &record.SortOrder, &record.CreatedAt, &record.UpdatedAt, &record.Version); err != nil {
		return nil, err
	}
	record.FallbackVersion = mysqlx.NullString(fallbackVer)
	record.FallbackManifestPath = mysqlx.NullString(fallbackPath)
	return &record, nil
}

// ListModules 实现 FrontendModuleRepo。
//
// moduleCode 精确匹配；keyword 对 name/module_code LIKE；status 等值过滤。
func (r *MySQLFrontendModuleRepo) ListModules(ctx context.Context, moduleCode, keyword string, status *int) ([]domain.FrontendModuleRecord, error) {
	query := "SELECT " + moduleColumns + " FROM t_acat_frontend_module WHERE is_deleted = 0"
	args := []any{}
	if strings.TrimSpace(moduleCode) != "" {
		query += " AND module_code = ?"
		args = append(args, moduleCode)
	}
	if trimmed := strings.TrimSpace(keyword); trimmed != "" {
		query += " AND (name LIKE ? OR module_code LIKE ?)"
		pattern := "%" + trimmed + "%"
		args = append(args, pattern, pattern)
	}
	if status != nil {
		query += " AND status = ?"
		args = append(args, *status)
	}
	if strings.TrimSpace(moduleCode) != "" {
		query += " ORDER BY created_at DESC"
	} else {
		query += " ORDER BY sort_order ASC, module_code ASC, created_at DESC"
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapError("查询前端模块失败", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]domain.FrontendModuleRecord, 0, 8)
	for rows.Next() {
		record, scanErr := scanModuleRecord(rows.Scan)
		if scanErr != nil {
			return nil, wrapError("扫描前端模块失败", scanErr)
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError("遍历前端模块失败", err)
	}
	return out, nil
}

// FindModuleByID 实现 FrontendModuleRepo（WHERE id=? AND is_deleted=0）。
func (r *MySQLFrontendModuleRepo) FindModuleByID(ctx context.Context, id string) (*domain.FrontendModuleRecord, error) {
	query := "SELECT " + moduleColumns + " FROM t_acat_frontend_module WHERE id = ? AND is_deleted = 0"
	record, err := scanModuleRecord(r.db.QueryRowContext(ctx, query, id).Scan)
	if err != nil {
		return nil, wrapError("查询前端模块失败", err)
	}
	return record, nil
}

// FindModuleByCodeAndVersion 实现 FrontendModuleRepo（按 module_code + release_version 查单条）。
func (r *MySQLFrontendModuleRepo) FindModuleByCodeAndVersion(ctx context.Context, moduleCode, releaseVersion string) (*domain.FrontendModuleRecord, error) {
	query := "SELECT " + moduleColumns +
		" FROM t_acat_frontend_module WHERE module_code = ? AND release_version = ? AND is_deleted = 0 LIMIT 1"
	record, err := scanModuleRecord(r.db.QueryRowContext(ctx, query, moduleCode, releaseVersion).Scan)
	if err != nil {
		return nil, wrapError("查询前端模块失败", err)
	}
	return record, nil
}

// InsertModule 实现 FrontendModuleRepo（is_deleted=0，审计字段由 service 填充）。
func (r *MySQLFrontendModuleRepo) InsertModule(ctx context.Context, record domain.FrontendModuleRecord) error {
	const query = `INSERT INTO t_acat_frontend_module
		(id, module_code, name, release_version, contract_version, manifest_path,
		 fallback_version, fallback_manifest_path, status, sort_order,
		 is_deleted, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, 0)`
	_, err := r.db.ExecContext(ctx, query,
		record.ID, record.ModuleCode, record.Name, record.ReleaseVersion, record.ContractVersion,
		record.ManifestPath, mysqlx.Arg(record.FallbackVersion), mysqlx.Arg(record.FallbackManifestPath),
		record.Status, record.SortOrder, record.CreatedAt, record.UpdatedAt)
	return wrapError("新增前端模块失败", err)
}

// UpdateModule 实现 FrontendModuleRepo（乐观锁：WHERE id=? AND version=? AND is_deleted=0，返回影响行数）。
//
// publish 依赖该行数判定 40901。
func (r *MySQLFrontendModuleRepo) UpdateModule(ctx context.Context, record domain.FrontendModuleRecord) (int64, error) {
	const query = `UPDATE t_acat_frontend_module
		SET module_code = ?, name = ?, release_version = ?, contract_version = ?, manifest_path = ?,
		    fallback_version = ?, fallback_manifest_path = ?, status = ?, sort_order = ?,
		    created_at = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND version = ? AND is_deleted = 0`
	result, err := r.db.ExecContext(ctx, query,
		record.ModuleCode, record.Name, record.ReleaseVersion, record.ContractVersion,
		record.ManifestPath, mysqlx.Arg(record.FallbackVersion), mysqlx.Arg(record.FallbackManifestPath),
		record.Status, record.SortOrder, record.CreatedAt, record.UpdatedAt, record.ID, record.Version)
	if err != nil {
		return 0, wrapError("更新前端模块失败", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, wrapError("更新前端模块失败", err)
	}
	return affected, nil
}

// DisableOtherEnabledVersions 实现 FrontendModuleRepo（disableOtherEnabledVersions）。
func (r *MySQLFrontendModuleRepo) DisableOtherEnabledVersions(ctx context.Context, moduleCode, keepID string) error {
	const query = `UPDATE t_acat_frontend_module SET status = ?
		WHERE module_code = ? AND status = ? AND id <> ? AND is_deleted = 0`
	_, err := r.db.ExecContext(ctx, query,
		domain.FrontendModuleStatusDisabled, moduleCode, domain.FrontendModuleStatusEnabled, keepID)
	return wrapError("停用同模块其他版本失败", err)
}

// ListEnabledModuleCodes 实现 FrontendModuleRepo。
func (r *MySQLFrontendModuleRepo) ListEnabledModuleCodes(ctx context.Context) ([]string, error) {
	const query = "SELECT module_code FROM t_acat_frontend_module WHERE is_deleted = 0 AND status = ?"
	rows, err := r.db.QueryContext(ctx, query, domain.FrontendModuleStatusEnabled)
	if err != nil {
		return nil, wrapError("查询启用模块失败", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]string, 0, 8)
	for rows.Next() {
		var code sql.NullString
		if err := rows.Scan(&code); err != nil {
			return nil, wrapError("扫描启用模块失败", err)
		}
		if trimmed := strings.TrimSpace(code.String); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError("遍历启用模块失败", err)
	}
	return out, nil
}

// CountEnabledModuleByCode 实现 FrontendModuleRepo（统计指定模块码的启用版本数）。
func (r *MySQLFrontendModuleRepo) CountEnabledModuleByCode(ctx context.Context, moduleCode string) (int64, error) {
	const query = `SELECT COUNT(*) FROM t_acat_frontend_module
		WHERE is_deleted = 0 AND module_code = ? AND status = ?`
	var count int64
	if err := r.db.QueryRowContext(ctx, query, moduleCode, domain.FrontendModuleStatusEnabled).Scan(&count); err != nil {
		return 0, wrapError("统计启用模块失败", err)
	}
	return count, nil
}

// ListEnabledModulesByCodes 实现 FrontendModuleRepo
// 每个 module_code 取 sort_order/updated_at/created_at 最优的一条（ROW_NUMBER 窗口排序）。
func (r *MySQLFrontendModuleRepo) ListEnabledModulesByCodes(ctx context.Context, codes []string) ([]domain.FrontendModuleRecord, error) {
	if len(codes) == 0 {
		return []domain.FrontendModuleRecord{}, nil
	}
	args := make([]any, 0, len(codes))
	for _, code := range codes {
		args = append(args, code)
	}
	query := `
		SELECT module_code, name, release_version, contract_version, manifest_path,
		       fallback_version, fallback_manifest_path, status, sort_order,
		       created_at, updated_at, version, id
		FROM (
			SELECT module.*,
			       ROW_NUMBER() OVER (
			           PARTITION BY module.module_code
			           ORDER BY module.sort_order ASC, module.updated_at DESC, module.created_at DESC
			       ) AS rn
			FROM t_acat_frontend_module module
			WHERE module.module_code IN (` + mysqlx.Placeholders(len(codes)) + `)
			  AND module.status = 1 AND module.is_deleted = 0
		) ranked
		WHERE ranked.rn = 1
		ORDER BY ranked.sort_order ASC, ranked.module_code ASC`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapError("查询前端模块失败", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]domain.FrontendModuleRecord, 0, len(codes))
	for rows.Next() {
		var (
			record       domain.FrontendModuleRecord
			fallbackVer  sql.NullString
			fallbackPath sql.NullString
		)
		if err := rows.Scan(&record.ModuleCode, &record.Name, &record.ReleaseVersion,
			&record.ContractVersion, &record.ManifestPath, &fallbackVer, &fallbackPath,
			&record.Status, &record.SortOrder, &record.CreatedAt, &record.UpdatedAt,
			&record.Version, &record.ID); err != nil {
			return nil, wrapError("扫描前端模块失败", err)
		}
		record.FallbackVersion = mysqlx.NullString(fallbackVer)
		record.FallbackManifestPath = mysqlx.NullString(fallbackPath)
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError("遍历前端模块失败", err)
	}
	return out, nil
}
