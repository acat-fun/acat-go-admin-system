package domain

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestNewIDIsUUIDv7(t *testing.T) {
	id := NewID()
	if len(id) != 36 {
		t.Fatalf("长度 = %d（%s）", len(id), id)
	}
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !pattern.MatchString(id) {
		t.Fatalf("不是 UUID v7 形态: %s", id)
	}
}

func TestNewIDAtEncodesTimestamp(t *testing.T) {
	when := time.Date(2026, 9, 14, 1, 2, 3, 0, time.UTC)
	id := NewIDAt(when)
	// 48bit 毫秒时间戳应位于前 12 个十六进制字符（去掉连字符后比较）。
	compact := strings.ReplaceAll(id, "-", "")
	millis := uint64(when.UnixMilli())
	expected := hex12(millis)
	if compact[:12] != expected {
		t.Fatalf("时间戳前缀 = %s，期望 %s", compact[:12], expected)
	}
}

func hex12(value uint64) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 12)
	for index := 11; index >= 0; index-- {
		out[index] = digits[value&0xf]
		value >>= 4
	}
	return string(out)
}

func TestIDsAreUnique(t *testing.T) {
	seen := map[string]struct{}{}
	for index := 0; index < 500; index++ {
		id := NewID()
		if _, dup := seen[id]; dup {
			t.Fatalf("重复 id: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestBuildDictDataTreeOrdersChildrenAndKeepsLeavesEmpty(t *testing.T) {
	parent := "root-1"
	list := []DictDataVO{
		{ID: "root-1", Code: "r", Name: "根", Value: "1"},
		{ID: "child-1", ParentID: &parent, Code: "c1", Name: "子1", Value: "2"},
		{ID: "child-2", ParentID: &parent, Code: "c2", Name: "子2", Value: "3"},
	}
	tree := BuildDictDataTree(list)
	if len(tree) != 1 || len(tree[0].Children) != 2 {
		t.Fatalf("树结构异常: %+v", tree)
	}
	if tree[0].Children[0].ID != "child-1" || tree[0].Children[0].Children == nil {
		t.Fatalf("叶子节点 children 应为空数组: %+v", tree[0].Children[0])
	}
	// 孤儿节点（父级不在集合内）不会出现在树中。
	orphanParent := "ghost"
	orphanTree := BuildDictDataTree([]DictDataVO{{ID: "x", ParentID: &orphanParent}})
	if len(orphanTree) != 0 {
		t.Fatalf("孤儿节点应被丢弃: %+v", orphanTree)
	}
}

func TestMapDictDataToSelectTree(t *testing.T) {
	tree := []DictDataVO{{
		ID: "p", Name: "根", Value: "1",
		Children: []DictDataVO{{ID: "c", Name: "子", Value: "2"}},
	}}
	options := MapDictDataToSelectTree(tree)
	if len(options) != 1 || options[0].Label != "根" || options[0].Value != "1" {
		t.Fatalf("映射异常: %+v", options)
	}
	if len(options[0].Children) != 1 || options[0].Children[0].Label != "子" {
		t.Fatalf("子节点映射异常: %+v", options[0].Children)
	}
}

func TestLegacyFrontendAlias(t *testing.T) {
	if got := LegacyFrontendAlias("acat.read.admin.system.base-config.files.file.businessType"); got != "file.businessType" {
		t.Fatalf("别名 = %q", got)
	}
	if got := LegacyFrontendAlias("acat.read.admin.system.base-config.files"); got != "" {
		t.Fatalf("前缀本身不应产生别名: %q", got)
	}
	if got := LegacyFrontendAlias("plain.code"); got != "" {
		t.Fatalf("未命中前缀应返回空串: %q", got)
	}
}

func TestOrderedMapPreservesInsertionOrderAndPutSemantics(t *testing.T) {
	m := NewOrderedMap()
	m.Put("b", "1")
	m.Put("a", "2")
	m.Put("b", "3") // 已存在：保留首次位置、覆盖值
	m.PutIfAbsent("a", "9")
	m.PutIfAbsent("c", "4")

	if keys := m.Keys(); len(keys) != 3 || keys[0] != "b" || keys[1] != "a" || keys[2] != "c" {
		t.Fatalf("键顺序异常: %v", keys)
	}
	if value, _ := m.Get("b"); value != "3" {
		t.Fatalf("b 的值 = %q", value)
	}
	if value, _ := m.Get("a"); value != "2" {
		t.Fatalf("PutIfAbsent 不应覆盖: %q", value)
	}

	encoded, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if string(encoded) != `{"b":"3","a":"2","c":"4"}` {
		t.Fatalf("JSON 顺序异常: %s", encoded)
	}
}

func TestIsValidManifestPath(t *testing.T) {
	if !IsValidManifestPath("system", "1.0.0", "/admin-remotes/system/1.0.0/mf-manifest.json") {
		t.Fatal("合法路径应通过")
	}
	cases := [][3]string{
		{"content", "1.0.0", "/admin-remotes/system/1.0.0/mf-manifest.json"},
		{"../system", "1.0.0", "/admin-remotes/../system/1.0.0/mf-manifest.json"},
		{"system", "1.0.0", "https://evil.example/mf-manifest.json"},
		{"System", "1.0.0", "/admin-remotes/System/1.0.0/mf-manifest.json"},
	}
	for _, item := range cases {
		if IsValidManifestPath(item[0], item[1], item[2]) {
			t.Errorf("非法路径应被拒绝: %v", item)
		}
	}
}

func TestAllowFileType(t *testing.T) {
	for _, value := range []string{"other", "avatar", "book_cover", "comic_cover", "comic_page", "author_sample"} {
		if !AllowFileType(value) {
			t.Errorf("%s 应在白名单内", value)
		}
	}
	for _, value := range []string{"", "OTHER", "book-cover", "video"} {
		if AllowFileType(value) {
			t.Errorf("%s 不应在白名单内", value)
		}
	}
}

func TestResolveDefaultNameFallsBack(t *testing.T) {
	values := []I18nValue{{I18n: "en", Value: "Name"}}
	if got := ResolveDefaultName("回退", values); got != "回退" {
		t.Fatalf("无 zh-CN 应回退，实际 %q", got)
	}
	values = append(values, I18nValue{I18n: "zh-CN", Value: "中文"})
	if got := ResolveDefaultName("回退", values); got != "中文" {
		t.Fatalf("应取 zh-CN，实际 %q", got)
	}
}

func TestFormatDateTimeTruncationSemantics(t *testing.T) {
	value := time.Date(2026, 9, 14, 1, 2, 3, 0, time.Local)
	if got := FormatDateTime(value); got != "2026-09-14T01:02:03" {
		t.Fatalf("格式 = %q", got)
	}
	if got := FormatDateTime(time.Time{}); got != "" {
		t.Fatalf("零值应输出空串，实际 %q", got)
	}
	if FormatDateTimePtr(time.Time{}) != nil {
		t.Fatal("零值指针应为 nil")
	}
	if Now().Nanosecond() != 0 {
		t.Fatal("Now() 应截断到秒")
	}
}

func TestSelectVOsNeverNil(t *testing.T) {
	if SelectVOs(nil) == nil {
		t.Fatal("空切片应输出 []")
	}
}
