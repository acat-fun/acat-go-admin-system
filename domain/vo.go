package domain

// SelectVO。
//
// {"label":...,"value":...}；顺序固定 label 在前。
type SelectVO struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// SelectVOs 构造下拉选项列表，保证空切片输出 [] 而不是 null。
func SelectVOs(items []SelectVO) []SelectVO {
	if items == nil {
		return []SelectVO{}
	}
	return items
}

// I18nValue 是多语言值（字段名 i18n/value）。
type I18nValue struct {
	I18n  string `json:"i18n"`
	Value string `json:"value"`
}

// ResolveDefaultName。
// i18nValue 中存在 zh-CN 且值非空白时取该值，否则回退 name。
func ResolveDefaultName(fallback string, values []I18nValue) string {
	for _, value := range values {
		if value.I18n != DefaultI18nCode {
			continue
		}
		if TrimOrEmpty(&value.Value) == "" {
			continue
		}
		return value.Value
	}
	return fallback
}
