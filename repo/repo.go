// Package repo 提供 admin-system 服务的数据访问实现。
//
// SQL 语句形状保持稳定（测试按字面断言）；
// 生效条件（is_deleted=0 等）必须一致，否则会出现契约差异。约定：
//   - 逻辑删除：所有读写都带 is_deleted = 0，删除一律 UPDATE ... SET is_deleted = 1；
//   - 幂等写：i18n 标签 upsert 用 ON DUPLICATE KEY UPDATE；
//   - 乐观锁：update 返回影响行数，由 service 决定 0 行是报错（前端模块 40901）还是忽略（字典/页面）；
//   - 主键：UUID v7（domain.NewID()）。
//
// 审计日志有两种存储实现（MongoDB 集合 / MySQL 表，见 NewAuditLogStore），对象存储通过
// storage.ObjectStorage 接口隔离且不在本包内；两者都提供内存实现供无 Mongo/MinIO 的环境使用。
package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

// DBTX 是 *sql.DB 与 *sql.Tx 的公共能力子集：repo 只通过它访问数据库，
// 因此 service 层把事务放进 context 后，同一用例内的多次调用自动落在同一事务里。
// Repository 不自行开启事务，事务边界一律由 service 层决定。
type DBTX interface {
	// ExecContext 执行不返回结果集的语句。
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	// QueryContext 执行返回结果集的查询。
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	// QueryRowContext 执行返回单行的查询。
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ErrNotFound 表示查询无结果。
var ErrNotFound = errors.New("repo: 记录不存在")

// ErrMultipleResults 表示期望单条结果但匹配到多条（如 (code, scope) 同码跨 scope 两行）。
var ErrMultipleResults = errors.New("repo: 期望单条结果但匹配到多条")

// DictRepo 提供字典定义与字典数据项的数据访问。
type DictRepo interface {
	// ListDicts 分页查询字典。
	ListDicts(ctx context.Context, filter domain.DictFilter, pageIndex, pageSize int) ([]domain.DictRecord, int64, error)
	// ListAllDicts 全量查询字典。
	ListAllDicts(ctx context.Context) ([]domain.DictRecord, error)
	// ListDictsForSelect 下拉用全量字典。
	ListDictsForSelect(ctx context.Context) ([]domain.DictRecord, error)
	// CountDataItemsByDictID 统计字典下未删除的数据项。
	CountDataItemsByDictID(ctx context.Context, dictID string) (int64, error)
	// FindDictByID 按主键查询字典（带 is_deleted=0）。
	FindDictByID(ctx context.Context, id string) (*domain.DictRecord, error)
	// FindDictByCode 按 code 查询单条字典。
	FindDictByCode(ctx context.Context, code string) (*domain.DictRecord, error)
	// CountDictsByCode 统计同 code 未删除字典数（新增字典的唯一性预检）。
	CountDictsByCode(ctx context.Context, code string) (int64, error)
	// InsertDict 新增字典。
	InsertDict(ctx context.Context, record domain.DictRecord) error
	// UpdateDict 更新字典（乐观锁），返回影响行数。
	UpdateDict(ctx context.Context, record domain.DictRecord) (int64, error)
	// SoftDeleteDict 软删除字典。
	SoftDeleteDict(ctx context.Context, id string) (int64, error)

	// ListDataItems 按字典分页查询数据项。
	ListDataItems(ctx context.Context, dictID string, filter domain.DictDataFilter, pageIndex, pageSize int) ([]domain.DictDataRecord, int64, error)
	// ListAllDataItems 查询字典下全部数据项。
	// isEnabled 为 nil 时不过滤启用状态。
	ListAllDataItems(ctx context.Context, dictID string, isEnabled *int) ([]domain.DictDataRecord, error)
	// FindDataItemByID 按主键查询数据项。
	FindDataItemByID(ctx context.Context, id string) (*domain.DictDataRecord, error)
	// ListChildDataItems 查询指定父级下的子数据项（删除数据项级联用，无 ORDER BY）。
	ListChildDataItems(ctx context.Context, dictID, parentID string) ([]domain.DictDataRecord, error)
	// InsertDataItem 新增数据项。
	InsertDataItem(ctx context.Context, record domain.DictDataRecord) error
	// UpdateDataItem 更新数据项（乐观锁），返回影响行数。
	UpdateDataItem(ctx context.Context, record domain.DictDataRecord) (int64, error)
	// UpdateDataItemByID 按主键更新数据项（不带乐观锁条件），返回影响行数。
	UpdateDataItemByID(ctx context.Context, record domain.DictDataRecord) (int64, error)
	// SoftDeleteDataItem 软删除数据项。
	SoftDeleteDataItem(ctx context.Context, id string) (int64, error)
}

// LabelRepo 提供 t_acat_i18n_label 的读写。
type LabelRepo interface {
	// ListNameLabels 查询来源对象的所有语言名称标签（ORDER BY i18n_code ASC）。
	ListNameLabels(ctx context.Context, sourceTable, tableDataID string) ([]domain.LabelRecord, error)
	// ResolveCurrentName 查询指定语言的名称标签，缺失返回空串。
	ResolveCurrentName(ctx context.Context, sourceTable, tableDataID, i18nCode string) (string, error)
	// SaveNameLabels 幂等写入名称标签（ON DUPLICATE KEY UPDATE），空值跳过。
	SaveNameLabels(ctx context.Context, sourceTable, tableDataID string, values []domain.I18nValue) error
	// DeleteNameLabels 软删除来源对象的名称标签。
	DeleteNameLabels(ctx context.Context, sourceTable, tableDataID string) (int64, error)
	// ListFrontendLabels 查询前端运行时字典（t_acat_dict ⋈ t_acat_dict_data ⋈ t_acat_i18n_label）。
	ListFrontendLabels(ctx context.Context, dictCode, i18nCode string) ([]domain.FrontendLabelRecord, error)
}

// PageRepo 提供页面与页面历史的数据访问。
type PageRepo interface {
	// ListPagesByScope 按 scope 查询页面并按 i18n 回退名称。
	ListPagesByScope(ctx context.Context, scope int, i18nCode string) ([]domain.PageRecord, error)
	// ListPagesByParentID 按父级查询子页面。
	ListPagesByParentID(ctx context.Context, parentID, i18nCode string) ([]domain.PageRecord, error)
	// FindPageByID 按主键查询页面。
	FindPageByID(ctx context.Context, id string) (*domain.PageRecord, error)
	// FindPageByCode 按 code 查询未删除页面。
	FindPageByCode(ctx context.Context, code string) (*domain.PageRecord, error)
	// FindDeletedPageByCode 按 code 查询已删除页面。
	FindDeletedPageByCode(ctx context.Context, code string) (*domain.PageRecord, error)
	// RestoreDeletedPage 恢复已删除页面（不带逻辑删除条件）。
	RestoreDeletedPage(ctx context.Context, record domain.PageRecord) (int64, error)
	// InsertPage 新增页面。
	InsertPage(ctx context.Context, record domain.PageRecord) error
	// UpdatePage 更新页面（乐观锁），返回影响行数。
	UpdatePage(ctx context.Context, record domain.PageRecord) (int64, error)
	// SoftDeletePage 软删除页面。
	SoftDeletePage(ctx context.Context, id string) (int64, error)
	// SelectPageIDsByPermissions 按权限码反查页面 id。
	SelectPageIDsByPermissions(ctx context.Context, permissions []string) ([]string, error)
	// InsertPageHistory 写入页面变更快照。
	InsertPageHistory(ctx context.Context, record domain.PageHistoryRecord) error
	// GrantPageToRootAndAdmin 为 root/admin 角色授权页面（幂等）。
	GrantPageToRootAndAdmin(ctx context.Context, pageID string) error
	// RestoreRolePermissionsByPageID 恢复该页面此前被软删的授权。
	RestoreRolePermissionsByPageID(ctx context.Context, pageID string) (int64, error)
	// DeleteRolePermissionsByPageID 软删除该页面的授权关联。
	DeleteRolePermissionsByPageID(ctx context.Context, pageID string) (int64, error)
}

// FrontendModuleRepo 提供前端模块的读写（管理端全量视图 + 启用过滤）。
type FrontendModuleRepo interface {
	// ListModules 查询前端模块。
	// moduleCode 非空时精确匹配；keyword 非空时对 name/module_code LIKE；status 非空时等值过滤。
	ListModules(ctx context.Context, moduleCode, keyword string, status *int) ([]domain.FrontendModuleRecord, error)
	// FindModuleByID 按主键查询前端模块。
	FindModuleByID(ctx context.Context, id string) (*domain.FrontendModuleRecord, error)
	// FindModuleByCodeAndVersion 按 (moduleCode, releaseVersion) 查询单条模块。
	FindModuleByCodeAndVersion(ctx context.Context, moduleCode, releaseVersion string) (*domain.FrontendModuleRecord, error)
	// InsertModule 新增前端模块。
	InsertModule(ctx context.Context, record domain.FrontendModuleRecord) error
	// UpdateModule 更新前端模块（乐观锁），返回影响行数。
	UpdateModule(ctx context.Context, record domain.FrontendModuleRecord) (int64, error)
	// DisableOtherEnabledVersions 停用同模块的其他启用版本。
	DisableOtherEnabledVersions(ctx context.Context, moduleCode, keepID string) error
	// ListEnabledModuleCodes 查询全部启用模块代码（无 ORDER BY，允许重复）。
	ListEnabledModuleCodes(ctx context.Context) ([]string, error)
	// CountEnabledModuleByCode 统计指定模块码的启用版本数（页面引用校验用）。
	CountEnabledModuleByCode(ctx context.Context, moduleCode string) (int64, error)
	// ListEnabledModulesByCodes 查询指定模块代码下的启用版本（每模块取排序最优的一条）。
	ListEnabledModulesByCodes(ctx context.Context, codes []string) ([]domain.FrontendModuleRecord, error)
}

// I18nTypeRepo 提供 t_acat_i18n_type 的数据访问。
type I18nTypeRepo interface {
	// ListTypesPaged 分页查询语言类型（keyword 同时 LIKE code/name，ORDER BY sort_order, code）。
	ListTypesPaged(ctx context.Context, keyword string, isEnabled *int, pageIndex, pageSize int) ([]domain.I18nTypeRecord, int64, error)
	// ListEnabledTypes 查询启用语言类型。
	ListEnabledTypes(ctx context.Context) ([]domain.I18nTypeRecord, error)
	// FindTypeByID 按主键查询语言类型。
	FindTypeByID(ctx context.Context, id string) (*domain.I18nTypeRecord, error)
	// CountTypesByCode 统计同 code 未删除语言类型数（唯一性预检）。
	CountTypesByCode(ctx context.Context, code string) (int64, error)
	// InsertType 新增语言类型。
	InsertType(ctx context.Context, record domain.I18nTypeRecord) error
	// UpdateType 更新语言类型（乐观锁），返回影响行数。
	UpdateType(ctx context.Context, record domain.I18nTypeRecord) (int64, error)
	// SoftDeleteType 软删除语言类型。
	SoftDeleteType(ctx context.Context, id string) (int64, error)
}

// FileRepo 提供 t_acat_file 的数据访问。
type FileRepo interface {
	// ListFiles 分页查询文件（ORDER BY created_at DESC）。
	// pathKeyword 非空时对 path/name 做 LIKE。
	ListFiles(ctx context.Context, fileType, pathKeyword string, pageIndex, pageSize int) ([]domain.FileRecord, int64, error)
	// FindFileByID 按主键查询文件。
	FindFileByID(ctx context.Context, id string) (*domain.FileRecord, error)
	// InsertFile 新增文件记录。
	InsertFile(ctx context.Context, record domain.FileRecord) error
	// SoftDeleteFile 软删除文件记录。
	SoftDeleteFile(ctx context.Context, id string) (int64, error)
}

// 编译期断言：确保实现满足接口。
var (
	_ DictRepo           = (*MySQLDictRepo)(nil)
	_ LabelRepo          = (*MySQLDictRepo)(nil)
	_ PageRepo           = (*MySQLPageRepo)(nil)
	_ FrontendModuleRepo = (*MySQLFrontendModuleRepo)(nil)
	_ I18nTypeRepo       = (*MySQLI18nTypeRepo)(nil)
	_ FileRepo           = (*MySQLFileRepo)(nil)
	_ AuditLogStore      = (*MemoryAuditLogStore)(nil)
)

// formatTime 把时间列转换为 时间文本（秒精度）。
func formatTime(value time.Time) string {
	return domain.FormatDateTime(value)
}

// wrapError 统一包装 SQL 错误并保留原始 cause（不吞异常）。
func wrapError(action string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", action, err)
}
