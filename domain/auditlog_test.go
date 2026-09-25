package domain

import (
	"testing"
	"time"
)

// TestFormatAuditDateTimeMatchesJacksonISO 锁定小数秒的格式化行为：
// 纳秒为 0 → 秒精度；否则按 3/6/9 位补零（BSON Date 只有毫秒，实际为 ".SSS"）。
func TestFormatAuditDateTimeMatchesJacksonISO(t *testing.T) {
	cases := []struct {
		name  string
		value time.Time
		want  string
	}{
		{
			name:  "无毫秒",
			value: time.Date(2026, 9, 14, 1, 2, 3, 0, time.UTC),
			want:  "2026-09-14T01:02:03",
		},
		{
			name:  "毫秒",
			value: time.Date(2026, 9, 13, 19, 27, 1, 862_000_000, time.UTC),
			want:  "2026-09-13T19:27:01.862",
		},
		{
			name:  "去掉尾随零（690ms → .69，UAT 实测口径）",
			value: time.Date(2026, 9, 14, 15, 22, 50, 690_000_000, time.UTC),
			want:  "2026-09-14T15:22:50.69",
		},
		{
			name:  "去掉尾随零（100ms → .1）",
			value: time.Date(2026, 9, 13, 19, 27, 1, 100_000_000, time.UTC),
			want:  "2026-09-13T19:27:01.1",
		},
		{
			name:  "零值输出空串",
			value: time.Time{},
			want:  "",
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := FormatAuditDateTime(item.value, time.UTC); got != item.want {
				t.Fatalf("FormatAuditDateTime = %q, want %q", got, item.want)
			}
		})
	}
}

// TestFormatAuditDateTimeUsesLocation 按本地时区格式化
// 这里用注入的 Location 还原（UTC+8 → 文本 +8 小时）。
func TestFormatAuditDateTimeUsesLocation(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("加载时区失败: %v", err)
	}
	value := time.Date(2026, 9, 14, 1, 2, 3, 0, time.UTC)
	if got := FormatAuditDateTime(value, shanghai); got != "2026-09-14T09:02:03" {
		t.Fatalf("时区换算异常: %q", got)
	}
}

// TestParseAuditDateTime 支持带/不带毫秒。
func TestParseAuditDateTime(t *testing.T) {
	withMillis, ok := ParseAuditDateTime("2026-09-14T01:02:03.456", time.UTC)
	if !ok || !withMillis.Equal(time.Date(2026, 9, 14, 1, 2, 3, 456_000_000, time.UTC)) {
		t.Fatalf("带毫秒解析异常: %v ok=%v", withMillis, ok)
	}
	plain, ok := ParseAuditDateTime("2026-09-14T01:02:03", nil)
	if !ok || !plain.Equal(time.Date(2026, 9, 14, 1, 2, 3, 0, time.UTC)) {
		t.Fatalf("秒精度解析异常: %v ok=%v", plain, ok)
	}
	if _, ok := ParseAuditDateTime("not-a-time", time.UTC); ok {
		t.Fatal("非法文本应返回 false")
	}
	if _, ok := ParseAuditDateTime("", time.UTC); ok {
		t.Fatal("空串应返回 false")
	}
}

// TestAuditLogTypeFromHTTPMethod 锁定 AuditLogTypes.fromHttpMethod（含大写归一）。
func TestAuditLogTypeFromHTTPMethod(t *testing.T) {
	cases := map[string]string{
		"POST":    AuditLogTypeCreate,
		"post":    AuditLogTypeCreate,
		"PUT":     AuditLogTypeUpdate,
		"PATCH":   AuditLogTypePatch,
		"DELETE":  AuditLogTypeDelete,
		"GET":     AuditLogTypeOperation,
		"":        AuditLogTypeOperation,
		"WS_SEND": AuditLogTypeOperation,
	}
	for method, want := range cases {
		if got := AuditLogTypeFromHTTPMethod(method); got != want {
			t.Errorf("AuditLogTypeFromHTTPMethod(%q) = %q, want %q", method, got, want)
		}
	}
}
