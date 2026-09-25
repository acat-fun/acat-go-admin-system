// Package repo 提供 admin-user 服务的数据访问实现。
//
// SQL 语句形状保持稳定（测试按字面断言）。
package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"47.108.230.93/acat-fun/acat-go-admin-system/auth/domain"
)

// ErrNotFound 表示查询无结果。
var ErrNotFound = errors.New("repo: 记录不存在")

// WorkerRepo 提供工作人员与权限码查询。
type WorkerRepo interface {
	// FindByUsername 按用户名查询未删除的工作人员（WorkerMapper.selectByUsername）。
	FindByUsername(ctx context.Context, username string) (*domain.Worker, error)
	// FindByID 按 id 查询工作人员（单表主键查询）。
	FindByID(ctx context.Context, id string) (*domain.Worker, error)
	// RoleCodes 查询用户的角色标识列表（WorkerMapper.selectRolesByUserId）。
	RoleCodes(ctx context.Context, userID string) ([]string, error)
	// URLAndButtonCodes 查询用户的页面 + 按钮权限码（WorkerMapper.selectPermissionsByUserId）。
	URLAndButtonCodes(ctx context.Context, userID string) ([]string, error)
	// URLCodes 查询用户可访问的页面编码（WorkerMapper.selectUrlCodesByUserId）。
	URLCodes(ctx context.Context, userID string) ([]string, error)
	// AllPermissionCodes 查询全量权限码（root 专用，WorkerMapper.selectAllPermissions）。
	AllPermissionCodes(ctx context.Context) ([]string, error)
	// AllURLCodes 查询全量页面编码（root 专用，WorkerMapper.selectAllUrlCodes）。
	AllURLCodes(ctx context.Context) ([]string, error)
}

// PageRepo 提供页面查询。
type PageRepo interface {
	// ListByScope 按 scope 查询启用页面并做 i18n 名称回退（AdminPageMapper.selectByScope）。
	ListByScope(ctx context.Context, scope int, i18nCode string) ([]domain.Page, error)
}

// FrontendModuleRepo 提供前端模块查询。
type FrontendModuleRepo interface {
	// ListEnabledByCodes 查询指定模块代码下的启用版本（每模块取排序最优的一条）。
	ListEnabledByCodes(ctx context.Context, codes []string) ([]domain.FrontendModule, error)
}

// DBTX 是 *sql.DB 与 *sql.Tx 的公共能力子集：repo 只通过它访问数据库，
// 因此 service 层把事务放进 context 后，同一用例内的多次调用自动落在同一事务里
// （规范 §8.7：Repository 不得隐式创建独立事务）。
type DBTX interface {
	// ExecContext 执行不返回结果集的语句。
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	// QueryContext 执行返回结果集的查询。
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	// QueryRowContext 执行返回单行的查询。
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// MySQLWorkerRepo 是 WorkerRepo 的 MySQL 实现。
type MySQLWorkerRepo struct {
	db DBTX
}

// NewWorkerRepo 构造 MySQL 实现。
func NewWorkerRepo(db DBTX) *MySQLWorkerRepo { return &MySQLWorkerRepo{db: db} }

const workerColumns = "id, username, password, email, avatar, status"

// FindByUsername 实现 WorkerRepo。
func (r *MySQLWorkerRepo) FindByUsername(ctx context.Context, username string) (*domain.Worker, error) {
	query := `SELECT ` + workerColumns + ` FROM t_acat_user_worker WHERE username = ? AND is_deleted = 0`
	return r.scanWorker(ctx, query, username)
}

// FindByID 实现 WorkerRepo。
func (r *MySQLWorkerRepo) FindByID(ctx context.Context, id string) (*domain.Worker, error) {
	query := `SELECT ` + workerColumns + ` FROM t_acat_user_worker WHERE id = ? AND is_deleted = 0`
	return r.scanWorker(ctx, query, id)
}

func (r *MySQLWorkerRepo) scanWorker(ctx context.Context, query string, args ...any) (*domain.Worker, error) {
	var (
		worker = &domain.Worker{}
		email  sql.NullString
		avatar sql.NullString
	)
	err := r.db.QueryRowContext(ctx, query, args...).
		Scan(&worker.ID, &worker.Username, &worker.Password, &email, &avatar, &worker.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询工作人员失败: %w", err)
	}
	worker.Email = email.String
	worker.Avatar = avatar.String
	return worker, nil
}

// RoleCodes 实现 WorkerRepo（WorkerMapper.selectRolesByUserId）。
func (r *MySQLWorkerRepo) RoleCodes(ctx context.Context, userID string) ([]string, error) {
	const query = `
		SELECT r.code
		FROM t_acat_role r
		INNER JOIN t_acat_user_role ur ON r.id = ur.role_id
		WHERE ur.user_id = ?
		  AND ur.is_deleted = 0
		  AND r.is_deleted = 0
		  AND r.status = 1`
	return r.queryStrings(ctx, query, userID)
}

// URLAndButtonCodes 实现 WorkerRepo（WorkerMapper.selectPermissionsByUserId）。
func (r *MySQLWorkerRepo) URLAndButtonCodes(ctx context.Context, userID string) ([]string, error) {
	const query = `
		SELECT code FROM (
			SELECT DISTINCT uc.code
			FROM t_acat_page uc
			INNER JOIN t_acat_role_permission rp ON uc.id = rp.permission_id AND rp.resource_type = 0
			INNER JOIN t_acat_user_role ur ON rp.role_id = ur.role_id
			INNER JOIN t_acat_role r ON r.id = ur.role_id
			WHERE ur.user_id = ?
			  AND ur.is_deleted = 0
			  AND rp.is_deleted = 0
			  AND uc.is_deleted = 0
			  AND uc.is_enabled = 1
			  AND r.status = 1
			UNION
			SELECT DISTINCT p.code
			FROM t_acat_permission p
			INNER JOIN t_acat_role_permission rp ON p.id = rp.permission_id AND rp.resource_type = 1
			INNER JOIN t_acat_user_role ur ON rp.role_id = ur.role_id
			INNER JOIN t_acat_role r ON r.id = ur.role_id
			WHERE ur.user_id = ?
			  AND ur.is_deleted = 0
			  AND rp.is_deleted = 0
			  AND p.is_deleted = 0
			  AND r.status = 1
		) permission_codes`
	return r.queryStrings(ctx, query, userID, userID)
}

// URLCodes 实现 WorkerRepo（WorkerMapper.selectUrlCodesByUserId）。
func (r *MySQLWorkerRepo) URLCodes(ctx context.Context, userID string) ([]string, error) {
	const query = `
		SELECT DISTINCT uc.code
		FROM t_acat_page uc
		INNER JOIN t_acat_role_permission rp ON uc.id = rp.permission_id AND rp.resource_type = 0
		INNER JOIN t_acat_user_role ur ON rp.role_id = ur.role_id
		INNER JOIN t_acat_role r ON r.id = ur.role_id
		WHERE ur.user_id = ?
		  AND ur.is_deleted = 0
		  AND rp.is_deleted = 0
		  AND uc.is_deleted = 0
		  AND uc.is_enabled = 1
		  AND r.status = 1`
	return r.queryStrings(ctx, query, userID)
}

// AllPermissionCodes 实现 WorkerRepo（WorkerMapper.selectAllPermissions）。
func (r *MySQLWorkerRepo) AllPermissionCodes(ctx context.Context) ([]string, error) {
	const query = `
		SELECT code FROM t_acat_page WHERE is_deleted = 0 AND is_enabled = 1
		UNION
		SELECT code FROM t_acat_permission WHERE is_deleted = 0`
	return r.queryStrings(ctx, query)
}

// AllURLCodes 实现 WorkerRepo（WorkerMapper.selectAllUrlCodes）。
func (r *MySQLWorkerRepo) AllURLCodes(ctx context.Context) ([]string, error) {
	const query = `SELECT code FROM t_acat_page WHERE is_deleted = 0 AND is_enabled = 1`
	return r.queryStrings(ctx, query)
}

func (r *MySQLWorkerRepo) queryStrings(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询权限码失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]string, 0, 16)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("扫描权限码失败: %w", err)
		}
		out = append(out, code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历权限码失败: %w", err)
	}
	return out, nil
}

// MySQLPageRepo 是 PageRepo 的 MySQL 实现。
type MySQLPageRepo struct {
	db DBTX
}

// NewPageRepo 构造 MySQL 实现。
func NewPageRepo(db DBTX) *MySQLPageRepo { return &MySQLPageRepo{db: db} }

// ListByScope 实现 PageRepo（AdminPageMapper.selectByScope）。
func (r *MySQLPageRepo) ListByScope(ctx context.Context, scope int, i18nCode string) ([]domain.Page, error) {
	const query = `
		SELECT uc.id, uc.code, COALESCE(label.label_value, uc.name) AS name,
		       uc.type, uc.path, uc.icon, uc.parent_id,
		       uc.sort_order, uc.scope, uc.is_enabled, uc.frontend_module_code, uc.route_key,
		       uc.created_at, uc.updated_at
		FROM t_acat_page uc
		LEFT JOIN t_acat_i18n_label label
		       ON label.source_table = 'acat_user.t_acat_page'
		      AND label.source_field = 'name'
		      AND label.table_data_id = uc.id
		      AND label.i18n_code = ?
		      AND label.is_deleted = 0
		WHERE uc.scope = ? AND uc.is_deleted = 0
		ORDER BY uc.sort_order ASC, uc.id ASC`

	rows, err := r.db.QueryContext(ctx, query, i18nCode, scope)
	if err != nil {
		return nil, fmt.Errorf("查询页面失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Page, 0, 32)
	for rows.Next() {
		var (
			page     domain.Page
			path     sql.NullString
			icon     sql.NullString
			parentID sql.NullString
			module   sql.NullString
			routeKey sql.NullString
		)
		if err := rows.Scan(&page.ID, &page.Code, &page.Name, &page.Type, &path, &icon, &parentID,
			&page.SortOrder, &page.Scope, &page.IsEnabled, &module, &routeKey,
			&page.CreatedAt, &page.UpdatedAt); err != nil {
			return nil, fmt.Errorf("扫描页面失败: %w", err)
		}
		page.Path = path.String
		page.Icon = icon.String
		page.ParentID = parentID.String
		page.FrontendModuleCode = module.String
		page.RouteKey = routeKey.String
		out = append(out, page)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历页面失败: %w", err)
	}
	return out, nil
}

// MySQLFrontendModuleRepo 是 FrontendModuleRepo 的 MySQL 实现。
type MySQLFrontendModuleRepo struct {
	db DBTX
}

// NewFrontendModuleRepo 构造 MySQL 实现。
func NewFrontendModuleRepo(db DBTX) *MySQLFrontendModuleRepo {
	return &MySQLFrontendModuleRepo{db: db}
}

// ListEnabledByCodes 实现 FrontendModuleRepo
// （AdminFrontendModuleMapper.selectEnabledByCodes：每个 module_code 取 sort_order/updated_at 最优的一条）。
func (r *MySQLFrontendModuleRepo) ListEnabledByCodes(ctx context.Context, codes []string) ([]domain.FrontendModule, error) {
	if len(codes) == 0 {
		return []domain.FrontendModule{}, nil
	}
	placeholders := ""
	args := make([]any, 0, len(codes))
	for i, code := range codes {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += "?"
		args = append(args, code)
	}
	query := `
		SELECT module_code, name, release_version, contract_version, manifest_path,
		       fallback_version, fallback_manifest_path, status, sort_order
		FROM (
			SELECT module.*,
			       ROW_NUMBER() OVER (
			           PARTITION BY module.module_code
			           ORDER BY module.sort_order ASC, module.updated_at DESC, module.created_at DESC
			       ) AS rn
			FROM t_acat_frontend_module module
			WHERE module.module_code IN (` + placeholders + `)
			  AND module.status = 1 AND module.is_deleted = 0
		) ranked
		WHERE ranked.rn = 1
		ORDER BY ranked.sort_order ASC, ranked.module_code ASC`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询前端模块失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.FrontendModule, 0, len(codes))
	for rows.Next() {
		var (
			module       domain.FrontendModule
			fallbackVer  sql.NullString
			fallbackPath sql.NullString
		)
		if err := rows.Scan(&module.ModuleCode, &module.Name, &module.ReleaseVersion, &module.ContractVersion,
			&module.ManifestPath, &fallbackVer, &fallbackPath, &module.Status, &module.SortOrder); err != nil {
			return nil, fmt.Errorf("扫描前端模块失败: %w", err)
		}
		module.FallbackVersion = fallbackVer.String
		// NULL → nil，保证描述符输出 null。
		module.FallbackManifestPath = nullableString(fallbackPath)
		out = append(out, module)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历前端模块失败: %w", err)
	}
	return out, nil
}
