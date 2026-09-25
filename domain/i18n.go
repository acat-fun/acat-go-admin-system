package domain

import (
	"bytes"
	"encoding/json"
)

// 本文件定义国际化域的结构：语言类型（表 t_acat_i18n_type）
// 与名称标签（表 t_acat_i18n_label）。

// I18nTypeEntity 是国际化语言类型实体。
type I18nTypeEntity struct {
	ID        string  `json:"id"`
	IsDeleted *int    `json:"isDeleted"`
	CreateBy  *string `json:"createBy"`
	UpdateBy  *string `json:"updateBy"`
	CreatedAt *string `json:"createdAt"`
	UpdatedAt *string `json:"updatedAt"`
	Version   *int    `json:"version"`

	Code      string `json:"code"`
	Name      string `json:"name"`
	SortOrder *int   `json:"sortOrder"`
	IsEnabled *int   `json:"isEnabled"`
}

// I18nLabelEntity 是国际化标签实体（标签表读写使用）。
type I18nLabelEntity struct {
	ID          string `json:"id"`
	I18nCode    string `json:"i18nCode"`
	LabelValue  string `json:"labelValue"`
	SourceTable string `json:"sourceTable"`
	SourceField string `json:"sourceField"`
	TableDataID string `json:"tableDataId"`
}

// 标签来源常量与前端国际化字典编码。
const (
	// I18nFieldName 是标签来源字段名固定值。
	I18nFieldName = "name"
	// I18nTableDict 字典表（含库名前缀）。
	I18nTableDict = "acat_user.t_acat_dict"
	// I18nTableDictData 字典数据项表。
	I18nTableDictData = "acat_user.t_acat_dict_data"
	// I18nTablePage 页面表。
	I18nTablePage = "acat_user.t_acat_page"
	// I18nTableRole 角色表。
	I18nTableRole = "acat_user.t_acat_role"
	// I18nTablePermission 权限表。
	I18nTablePermission = "acat_user.t_acat_permission"

	// FrontendAdminDictCode 管理端前端国际化字典编码。
	FrontendAdminDictCode = "i18n_admin_label"
	// FrontendAppDictCode 用户端前端国际化字典编码。
	FrontendAppDictCode = "i18n_app_label"
)

// frontendNamespacePrefixes 是前端运行时字典的命名空间前缀列表。
//
// 完整编码去掉这些前缀后的剩余部分也作为 key 输出（历史别名兼容）。
var frontendNamespacePrefixes = []string{
	"acat.read.admin.system.base-config.files",
	"acat.read.admin.system.base-config.dicts",
	"acat.read.admin.system.base-config.i18n-types",
	"acat.read.admin.system.base-config.pages",
	"acat.read.admin.system.base-config.permissions",
	"acat.read.admin.system.user-management.workers",
	"acat.read.admin.system.user-management.users",
	"acat.read.admin.content.book-governance.books",
	"acat.read.admin.content.book-governance.book-reviews",
	"acat.read.admin.content.book-governance.chapter-reviews",
	"acat.read.admin.content.comic.works",
	"acat.read.admin.content.comic.reviews",
	"acat.read.admin.content.comic.chapter-reviews",
	"acat.read.admin.content.operation-config.files",
	"acat.read.admin.content.operation-config.system-configs",
	"acat.read.admin.content.operation-config.seo-configs",
	"acat.read.admin.content.interaction-governance.reports",
	"acat.read.admin.content.interaction-governance.takedown-requests",
	"acat.read.admin.content.interaction-governance.sensitive-words",
	"acat.read.admin.content.operation-config.recommendations",
	"acat.read.admin.content.operation-config.campaigns",
	"acat.read.admin.content.operation-config.announcements",
	"acat.read.admin.dashboard.overview.index",
	"acat.read.admin.shared.common.components",
	"acat.read.app.navigation.main.nav",
	"acat.read.app.account.auth.login",
	"acat.read.app.account.auth.register",
	"acat.read.app.home.overview.index",
	"acat.read.app.content.book.detail",
	"acat.read.app.content.comic.detail",
	"acat.read.app.read.reader.reader",
	"acat.read.app.author.center.dashboard",
	"acat.read.app.user.profile.index",
	"acat.read.app.search.catalog.index",
	"acat.read.app.ranking.catalog.index",
	"acat.read.app.user.bookshelf.index",
	"acat.read.app.shared.common.components",
}

// LegacyFrontendAlias。
// 命中前缀且前缀后仍有内容时返回去掉前缀（含点）的剩余部分，否则返回空串。
func LegacyFrontendAlias(code string) string {
	for _, prefix := range frontendNamespacePrefixes {
		namespace := prefix + "."
		if len(code) > len(namespace) && code[:len(namespace)] == namespace {
			return code[len(namespace):]
		}
	}
	return ""
}

// OrderedMap 是按插入顺序序列化的 string→string 映射。
//
// 用途：侧 `/api/admin/system/i18n/**/frontend-labels` 返回 LinkedHashMap，
// JSON key 顺序即 SQL 的 `data.sort_order ASC, data.code ASC` 顺序；
// Go 原生 map 的 JSON 编码会按键排序，这里用有序结构保持键序稳定。
type OrderedMap struct {
	keys   []string
	values map[string]string
}

// NewOrderedMap 构造空的有序映射。
func NewOrderedMap() *OrderedMap {
	return &OrderedMap{values: make(map[string]string)}
}

// Put 写入键值；已存在的键保留首次插入位置。
func (m *OrderedMap) Put(key, value string) {
	if _, exists := m.values[key]; !exists {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

// PutIfAbsent 仅在键不存在时写入。
func (m *OrderedMap) PutIfAbsent(key, value string) {
	if _, exists := m.values[key]; exists {
		return
	}
	m.keys = append(m.keys, key)
	m.values[key] = value
}

// Get 读取键值。
func (m *OrderedMap) Get(key string) (string, bool) {
	if m == nil {
		return "", false
	}
	value, ok := m.values[key]
	return value, ok
}

// Len 返回键数量。
func (m *OrderedMap) Len() int {
	if m == nil {
		return 0
	}
	return len(m.keys)
}

// Keys 返回插入顺序的键副本。
func (m *OrderedMap) Keys() []string {
	if m == nil {
		return nil
	}
	out := make([]string, len(m.keys))
	copy(out, m.keys)
	return out
}

// MarshalJSON 按插入顺序输出 JSON 对象；空映射输出 {}。
func (m *OrderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for index, key := range m.keys {
		if index > 0 {
			buf.WriteByte(',')
		}
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		encodedValue, err := json.Marshal(m.values[key])
		if err != nil {
			return nil, err
		}
		buf.Write(encodedKey)
		buf.WriteByte(':')
		buf.Write(encodedValue)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
