package domain

import "time"

// 本文件定义 repo 层返回的“数据库行”读模型。
//
// 实体结构体只承担 HTTP 契约，数据库行则由 Record 类型承载，两层职责分离：
//   - *Record：数据库行（时间用 time.Time，未删除标记不暴露）
//   - 实体/VO：HTTP 契约（时间已格式化为 时间文本）
// 字段语义与 DB 列一一对应。

// DictRecord 是 t_acat_dict 的一行。
type DictRecord struct {
	ID          string
	Code        string
	Name        string
	IsEnabled   int
	IsTree      int
	Scope       int
	Description *string
	// IsBuiltin 为 1 表示随版本发布的内置字典（禁止删除）。
	IsBuiltin int
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int
	// CreateBy/UpdateBy 仅写入路径使用。
	CreateBy *string
	UpdateBy *string
}

// DictDataRecord 是 t_acat_dict_data 的一行。
type DictDataRecord struct {
	ID          string
	DictID      string
	ParentID    *string
	Code        string
	Name        string
	Value       string
	AgeLevel    int
	SortOrder   int
	IsEnabled   int
	Description *string
	// Color 是数据项标签颜色，空串表示不指定。
	Color string
	// IsBuiltin 为 1 表示随版本发布的内置数据项（禁止删除）。
	IsBuiltin int
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int
	// CreateBy/UpdateBy 仅写入路径使用。
	CreateBy *string
	UpdateBy *string
}

// DictFilter 是字典列表查询条件。
type DictFilter struct {
	// Name 非空白时按 name LIKE %Name%。
	Name string
	// IsEnabled / IsTree / Scope 非 nil 时等值过滤。
	IsEnabled *int
	IsTree    *int
	Scope     *int
}

// DictDataFilter 是字典数据项查询条件。
type DictDataFilter struct {
	// Name 非空白时匹配 (name LIKE %Name% OR code LIKE %Name%)。
	Name string
}

// LabelRecord 是 t_acat_i18n_label 的一行（仅名称标签所需字段）。
type LabelRecord struct {
	I18nCode   string
	LabelValue string
}

// I18nTypeRecord 是 t_acat_i18n_type 的一行。
type I18nTypeRecord struct {
	ID        string
	Code      string
	Name      string
	SortOrder int
	IsEnabled int
	// IsBuiltin 为 1 表示随版本发布的内置语言类型（禁止删除）。
	IsBuiltin int
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int
}

// FrontendLabelRecord 是前端运行时代码字典的一行（dict_data.code → label_value）。
type FrontendLabelRecord struct {
	// Code 是 t_acat_dict_data.code（Map 的 key）。
	Code string
	// LabelValue 是当前语言的展示文案（Map 的 value）。
	LabelValue string
}

// PageRecord 是 t_acat_page 的一行（name 已按 i18n 回退）。
type PageRecord struct {
	ID                 string
	Code               string
	Name               string
	Type               int
	Path               *string
	Icon               *string
	ParentID           *string
	SortOrder          int
	Scope              int
	IsEnabled          int
	FrontendModuleCode *string
	RouteKey           *string
	// PermissionCode 是权限判定码，空串表示按 Code 判定。
	PermissionCode string
	// IsBuiltin 为 1 表示随版本发布的内置页面（禁止删除）。
	IsBuiltin int
	// Description 是页面说明。
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
	// IsDeleted 仅读取已软删除行（页面恢复路径）时使用。
	IsDeleted int
	// CreateBy/UpdateBy 仅写入变更快照时使用。
	CreateBy *string
	UpdateBy *string
}

// PageHistoryRecord 是 t_acat_page_history 的写入载荷。
type PageHistoryRecord struct {
	PageID             string
	Code               string
	Name               string
	Type               *int
	Path               *string
	Icon               *string
	ParentID           *string
	SortOrder          *int
	Scope              *int
	IsEnabled          *int
	FrontendModuleCode *string
	RouteKey           *string
	IsDeleted          *int
	CreateBy           *string
	UpdateBy           *string
	CreatedAt          *time.Time
	UpdatedAt          time.Time
	Version            *int
}

// FrontendModuleRecord 是 t_acat_frontend_module 的一行。
type FrontendModuleRecord struct {
	ID                   string
	ModuleCode           string
	Name                 string
	ReleaseVersion       string
	ContractVersion      int
	ManifestPath         string
	FallbackVersion      *string
	FallbackManifestPath *string
	Status               int
	SortOrder            int
	Version              int
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// FileRecord 是 t_acat_file 的一行。
type FileRecord struct {
	ID        string
	Name      string
	Path      string
	Type      string
	FileType  string
	Size      int64
	CreatedAt time.Time
	UpdatedAt time.Time
}
