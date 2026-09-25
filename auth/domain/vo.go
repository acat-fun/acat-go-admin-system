package domain

// I18nValue 是多语言值（字段名 i18n/value）。
//
// 注意：bootstrap 链路输出的 PageVO.i18nValue 始终为 null（无装配点），
// 契约保持该 null 以对齐前端预期。
type I18nValue struct {
	I18n  string `json:"i18n"`
	Value string `json:"value"`
}

// PageVO。
//
// CreatedAt/UpdatedAt 为时间文本，格式见 DateTimeLayout
// （如 "2026-08-01T09:00:57"）；数据库时间为空时输出 null。
type PageVO struct {
	ID                 string      `json:"id"`
	Code               string      `json:"code"`
	Name               string      `json:"name"`
	Type               int         `json:"type"`
	Path               *string     `json:"path"`
	Icon               *string     `json:"icon"`
	ParentID           *string     `json:"parentId"`
	SortOrder          int         `json:"sortOrder"`
	Scope              int         `json:"scope"`
	IsEnabled          int         `json:"isEnabled"`
	FrontendModuleCode *string     `json:"frontendModuleCode"`
	RouteKey           *string     `json:"routeKey"`
	I18nValue          []I18nValue `json:"i18nValue"`
	Children           []PageVO    `json:"children"`
	CreatedAt          *string     `json:"createdAt"`
	UpdatedAt          *string     `json:"updatedAt"`
}

// FrontendModuleDescriptor 是登录启动数据里的前端模块描述符。
//
// FallbackManifestURL 直接透传库列的可空值：库列为 NULL 时 JSON 输出 null。
type FrontendModuleDescriptor struct {
	ModuleCode          string  `json:"moduleCode"`
	Version             string  `json:"version"`
	ContractVersion     int     `json:"contractVersion"`
	ManifestURL         string  `json:"manifestUrl"`
	FallbackManifestURL *string `json:"fallbackManifestUrl"`
}

// Session。
type Session struct {
	UserID      string  `json:"userId"`
	AccountName string  `json:"accountName"`
	Username    string  `json:"username"`
	Email       *string `json:"email"`
	Avatar      *string `json:"avatar"`
	Bio         *string `json:"bio"`
	Gender      *int    `json:"gender"`
	Birthday    *string `json:"birthday"`
	Location    *string `json:"location"`
}

// Bootstrap。
type Bootstrap struct {
	Session         Session                    `json:"session"`
	Roles           []string                   `json:"roles"`
	Permissions     []string                   `json:"permissions"`
	URLs            []string                   `json:"urls"`
	Pages           []PageVO                   `json:"pages"`
	FrontendModules []FrontendModuleDescriptor `json:"frontendModules"`
}

// NewEmptyBootstrap 构造空启动数据，保证数组字段输出 [] 而不是 null
// 。
func NewEmptyBootstrap() Bootstrap {
	return Bootstrap{
		Roles:           []string{},
		Permissions:     []string{},
		URLs:            []string{},
		Pages:           []PageVO{},
		FrontendModules: []FrontendModuleDescriptor{},
	}
}
