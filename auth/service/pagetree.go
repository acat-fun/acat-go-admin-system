package service

import (
	"time"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
)

// BuildPageTree。
//   - 根节点是 parentId 为空的页面；
//   - type==2（page）不挂载子节点；
//   - 其它节点的子节点按输入顺序（SQL 已按 sort_order, id 排序）挂载。
func BuildPageTree(pages []domain.Page) []domain.PageVO {
	byParent := make(map[string][]domain.Page)
	for _, page := range pages {
		if page.ParentID == "" {
			continue
		}
		byParent[page.ParentID] = append(byParent[page.ParentID], page)
	}
	out := make([]domain.PageVO, 0, len(pages))
	for _, page := range pages {
		if page.ParentID != "" {
			continue
		}
		out = append(out, buildPageNode(page, byParent))
	}
	return out
}

func buildPageNode(page domain.Page, byParent map[string][]domain.Page) domain.PageVO {
	node := toPageVO(page)
	if page.Type == 2 {
		// page 类型不挂载子节点。
		node.Children = nil
		return node
	}
	children := byParent[page.ID]
	node.Children = make([]domain.PageVO, 0, len(children))
	for _, child := range children {
		node.Children = append(node.Children, buildPageNode(child, byParent))
	}
	return node
}

// toPageVO 按契约字段映射页面实体，并保持可空字段的 null 语义。
func toPageVO(page domain.Page) domain.PageVO {
	return domain.PageVO{
		ID:                 page.ID,
		Code:               page.Code,
		Name:               page.Name,
		Type:               page.Type,
		Path:               nullable(page.Path),
		Icon:               nullable(page.Icon),
		ParentID:           nullable(page.ParentID),
		SortOrder:          page.SortOrder,
		Scope:              page.Scope,
		IsEnabled:          page.IsEnabled,
		FrontendModuleCode: nullable(page.FrontendModuleCode),
		RouteKey:           nullable(page.RouteKey),
		// i18nValue 保持 nil。
		I18nValue: nil,
		CreatedAt: nullableDateTime(page.CreatedAt),
		UpdatedAt: nullableDateTime(page.UpdatedAt),
	}
}

// nullableDateTime。
// 有值时输出 ISO 时间文本（如 "2026-08-01T09:00:57"），零值输出 null。
func nullableDateTime(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	formatted := domain.FormatDateTime(value)
	return &formatted
}
