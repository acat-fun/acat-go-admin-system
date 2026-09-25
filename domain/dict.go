package domain

// 本文件定义字典域的实体与视图结构：
//   - AdminDictEntity     字典定义（表 t_acat_dict）
//   - AdminDictDataEntity 字典数据项（表 t_acat_dict_data）
//   - DictVO              字典视图（含数据项与数据项计数）
//   - DictDataVO          字典数据项视图（含 children 树）
//
// 字段顺序固定为审计字段在前（id/isDeleted/createBy/updateBy/createdAt/
// updatedAt/version），业务字段在后。
// 可空列一律用指针，保证 JSON 输出 null 而不是零值。

// AdminDictEntity 是字典定义实体。
//
// 该字段集同时也是请求收参契约，但主键与审计字段一律由服务端生成
// （不接受客户端指定 id/createBy/updateBy/createdAt/updatedAt/version）。
type AdminDictEntity struct {
	ID        string  `json:"id"`
	IsDeleted *int    `json:"isDeleted"`
	CreateBy  *string `json:"createBy"`
	UpdateBy  *string `json:"updateBy"`
	CreatedAt *string `json:"createdAt"`
	UpdatedAt *string `json:"updatedAt"`
	Version   *int    `json:"version"`

	Code        string      `json:"code"`
	Name        string      `json:"name"`
	IsEnabled   *int        `json:"isEnabled"`
	IsTree      *int        `json:"isTree"`
	Scope       *int        `json:"scope"`
	Description *string     `json:"description"`
	I18nValue   []I18nValue `json:"i18nValue"`
}

// AdminDictDataEntity 是字典数据项实体。
type AdminDictDataEntity struct {
	ID        string  `json:"id"`
	IsDeleted *int    `json:"isDeleted"`
	CreateBy  *string `json:"createBy"`
	UpdateBy  *string `json:"updateBy"`
	CreatedAt *string `json:"createdAt"`
	UpdatedAt *string `json:"updatedAt"`
	Version   *int    `json:"version"`

	DictID      string      `json:"dictId"`
	ParentID    *string     `json:"parentId"`
	Code        string      `json:"code"`
	Name        string      `json:"name"`
	Value       string      `json:"value"`
	AgeLevel    *int        `json:"ageLevel"`
	SortOrder   *int        `json:"sortOrder"`
	IsEnabled   *int        `json:"isEnabled"`
	Description *string     `json:"description"`
	I18nValue   []I18nValue `json:"i18nValue"`
}

// DictVO。
type DictVO struct {
	ID          string       `json:"id"`
	Code        string       `json:"code"`
	Name        string       `json:"name"`
	IsEnabled   *int         `json:"isEnabled"`
	IsTree      *int         `json:"isTree"`
	Scope       *int         `json:"scope"`
	Description *string      `json:"description"`
	I18nValue   []I18nValue  `json:"i18nValue"`
	DataItems   []DictDataVO `json:"dataItems"`
	DataCount   int          `json:"dataCount"`
	CreatedAt   *string      `json:"createdAt"`
	UpdatedAt   *string      `json:"updatedAt"`
}

// DictDataVO。
//
// Children 为 nil 时 JSON 输出 null；树形场景下无子节点输出 []。
type DictDataVO struct {
	ID          string       `json:"id"`
	DictID      string       `json:"dictId"`
	ParentID    *string      `json:"parentId"`
	Code        string       `json:"code"`
	Name        string       `json:"name"`
	Value       string       `json:"value"`
	AgeLevel    *int         `json:"ageLevel"`
	SortOrder   *int         `json:"sortOrder"`
	IsEnabled   *int         `json:"isEnabled"`
	Description *string      `json:"description"`
	I18nValue   []I18nValue  `json:"i18nValue"`
	Children    []DictDataVO `json:"children"`
	CreatedAt   *string      `json:"createdAt"`
}

// DictSavePayload。
//
// 只保留可写业务字段：主键与审计字段（id/createBy/updateBy/createdAt/
// updatedAt/version）一律服务端生成，不接受客户端传入。
type DictSavePayload struct {
	Code        *string     `json:"code"`
	Name        *string     `json:"name"`
	IsEnabled   *int        `json:"isEnabled"`
	IsTree      *int        `json:"isTree"`
	Scope       *int        `json:"scope"`
	Description *string     `json:"description"`
	I18nValue   []I18nValue `json:"i18nValue"`
}

// DictDataSavePayload。
//
// IsDeleted 参与批量保存语义，因此保留。
// ID 仅用于批量保存的“已有行”识别，创建路径忽略客户端 id。
type DictDataSavePayload struct {
	ID          *string     `json:"id"`
	IsDeleted   *int        `json:"isDeleted"`
	DictID      *string     `json:"dictId"`
	ParentID    *string     `json:"parentId"`
	Code        *string     `json:"code"`
	Name        *string     `json:"name"`
	Value       *string     `json:"value"`
	AgeLevel    *int        `json:"ageLevel"`
	SortOrder   *int        `json:"sortOrder"`
	IsEnabled   *int        `json:"isEnabled"`
	Description *string     `json:"description"`
	I18nValue   []I18nValue `json:"i18nValue"`
}

// DictDataDeleted 判断批量保存条目是否标记为删除（isDeleted != null && isDeleted == 1）。
func (p DictDataSavePayload) DictDataDeleted() bool {
	return p.IsDeleted != nil && *p.IsDeleted == 1
}

// BuildDictDataTree。
// 根节点为 parentId == null 的节点，子节点按输入顺序挂在对应父节点下；
// 无子节点的节点 Children 为空切片。
func BuildDictDataTree(list []DictDataVO) []DictDataVO {
	byParent := make(map[string][]DictDataVO)
	for _, item := range list {
		if item.ParentID == nil {
			continue
		}
		byParent[*item.ParentID] = append(byParent[*item.ParentID], item)
	}
	roots := make([]DictDataVO, 0, len(list))
	for _, item := range list {
		if item.ParentID != nil {
			continue
		}
		roots = append(roots, buildDictDataNode(item, byParent))
	}
	return roots
}

func buildDictDataNode(node DictDataVO, byParent map[string][]DictDataVO) DictDataVO {
	children := make([]DictDataVO, 0, len(byParent[node.ID]))
	for _, child := range byParent[node.ID] {
		children = append(children, buildDictDataNode(child, byParent))
	}
	node.Children = children
	return node
}

// SelectTreeNode。
type SelectTreeNode struct {
	Label    string           `json:"label"`
	Value    string           `json:"value"`
	Children []SelectTreeNode `json:"children"`
}

// MapDictDataToSelectTree。
func MapDictDataToSelectTree(tree []DictDataVO) []SelectTreeNode {
	out := make([]SelectTreeNode, 0, len(tree))
	for _, node := range tree {
		children := node.Children
		if children == nil {
			children = []DictDataVO{}
		}
		out = append(out, SelectTreeNode{
			Label:    node.Name,
			Value:    node.Value,
			Children: MapDictDataToSelectTree(children),
		})
	}
	return out
}
