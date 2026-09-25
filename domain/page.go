package domain

import (
	"regexp"
	"strings"
)

// 本文件对应 侧页面域实体与视图：
//   - AdminPageEntity        (fun.acat.admin.entity.AdminPageEntity, 表 t_acat_page)
//   - AdminPageHistoryEntity (fun.acat.admin.system.entity.AdminPageHistoryEntity, 表 t_acat_page_history)
//   - PageVO                 (fun.acat.admin.vo.PageVO)

// 页面类型（AdminPageEntity.TYPE_*）。
const (
	PageTypeNav    = 0
	PageTypeFolder = 1
	PageTypePage   = 2
)

// PageScopeAdmin 管理端范围（create/update 强制写 0）。
const PageScopeAdmin = 0

// AdminPageEntity 是页面实体。
type AdminPageEntity struct {
	ID        string  `json:"id"`
	IsDeleted *int    `json:"isDeleted"`
	CreateBy  *string `json:"createBy"`
	UpdateBy  *string `json:"updateBy"`
	CreatedAt *string `json:"createdAt"`
	UpdatedAt *string `json:"updatedAt"`
	Version   *int    `json:"version"`

	Code               string      `json:"code"`
	Name               string      `json:"name"`
	Type               *int        `json:"type"`
	Path               *string     `json:"path"`
	Icon               *string     `json:"icon"`
	ParentID           *string     `json:"parentId"`
	SortOrder          *int        `json:"sortOrder"`
	Scope              *int        `json:"scope"`
	IsEnabled          *int        `json:"isEnabled"`
	FrontendModuleCode *string     `json:"frontendModuleCode"`
	RouteKey           *string     `json:"routeKey"`
	I18nValue          []I18nValue `json:"i18nValue"`
}

// PageSavePayload。
//
// 只保留可写业务字段：code 由 path 派生，scope 强制 0，
// id/createBy/updateBy/version/isDeleted 不接受客户端传入（差异见 README）。
type PageSavePayload struct {
	Name               *string     `json:"name"`
	Type               *int        `json:"type"`
	Path               *string     `json:"path"`
	Icon               *string     `json:"icon"`
	ParentID           *string     `json:"parentId"`
	SortOrder          *int        `json:"sortOrder"`
	IsEnabled          *int        `json:"isEnabled"`
	FrontendModuleCode *string     `json:"frontendModuleCode"`
	RouteKey           *string     `json:"routeKey"`
	I18nValue          []I18nValue `json:"i18nValue"`
}

// PageVO。
type PageVO struct {
	ID                 string      `json:"id"`
	Code               string      `json:"code"`
	Name               string      `json:"name"`
	Type               *int        `json:"type"`
	Path               *string     `json:"path"`
	Icon               *string     `json:"icon"`
	ParentID           *string     `json:"parentId"`
	SortOrder          *int        `json:"sortOrder"`
	Scope              *int        `json:"scope"`
	IsEnabled          *int        `json:"isEnabled"`
	FrontendModuleCode *string     `json:"frontendModuleCode"`
	RouteKey           *string     `json:"routeKey"`
	I18nValue          []I18nValue `json:"i18nValue"`
	Children           []PageVO    `json:"children"`
	CreatedAt          *string     `json:"createdAt"`
	UpdatedAt          *string     `json:"updatedAt"`
}

// BuildPageTree。
// 根节点为 parentId == null 的节点；type==2（page）不挂载子节点，Children 为 null。
func BuildPageTree(list []PageVO) []PageVO {
	byParent := make(map[string][]PageVO)
	for _, item := range list {
		if item.ParentID == nil {
			continue
		}
		byParent[*item.ParentID] = append(byParent[*item.ParentID], item)
	}
	roots := make([]PageVO, 0, len(list))
	for _, item := range list {
		if item.ParentID != nil {
			continue
		}
		roots = append(roots, setPageChildren(item, byParent))
	}
	return roots
}

func setPageChildren(node PageVO, byParent map[string][]PageVO) PageVO {
	if node.Type != nil && *node.Type == PageTypePage {
		node.Children = nil
		return node
	}
	children := byParent[node.ID]
	if children == nil {
		children = []PageVO{}
	}
	nested := make([]PageVO, 0, len(children))
	for _, child := range children {
		nested = append(nested, setPageChildren(child, byParent))
	}
	node.Children = nested
	return node
}

// RouteKeyPattern 是远程页面注册键的正则。
var RouteKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$`)

// NormalizePermissionPath。
//
// 用于把页面 path 转成权限码路径段：去掉 /admin、/author、/app 前缀，去掉冒号，斜杠转冒号。
// 等价于正则替换链：
//
//	path.trim().replaceFirst("^/(admin|author|app)(/|$)", "")
//	     .replaceFirst("^/", "").replace(":", "").replace("/", ":")
func NormalizePermissionPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == "/" {
		return "home"
	}
	for _, scope := range []string{"/admin", "/author", "/app"} {
		if trimmed == scope {
			trimmed = ""
			break
		}
		if strings.HasPrefix(trimmed, scope+"/") {
			trimmed = trimmed[len(scope):]
			break
		}
	}
	trimmed = strings.TrimPrefix(trimmed, "/")
	trimmed = strings.ReplaceAll(trimmed, ":", "")
	trimmed = strings.ReplaceAll(trimmed, "/", ":")
	return trimmed
}

// GenerateAdminPermissionCode。
func GenerateAdminPermissionCode(path string) string {
	return "acat:admin:" + NormalizePermissionPath(path)
}
