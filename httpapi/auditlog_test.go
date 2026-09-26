package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/acat-fun/acat-go-admin-system/domain"
	"github.com/acat-fun/acat-go-common/result"
)

// TestAuditListPaginationSemantics 分页过滤与优先级
func TestAuditListPaginationSemantics(t *testing.T) {
	server := newTestServer(t)
	token := server.login(t, domain.RootLoginID, nil)
	created := "2026-09-01T10:00:00"
	if err := server.audits.Insert(context.Background(), domain.AuditLog{Type: "LOGIN", CreatedAt: &created}); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}

	cases := []struct {
		name      string
		query     string
		wantCode  int
		wantIndex int
		wantSize  int
	}{
		{name: "缺省", query: "", wantCode: http.StatusOK, wantIndex: 1, wantSize: 20},
		{name: "pageIndex=0 收敛为第 1 页但回显 0", query: "?pageIndex=0", wantCode: http.StatusOK, wantIndex: 0, wantSize: 20},
		{name: "pageIndex=-2", query: "?pageIndex=-2", wantCode: http.StatusOK, wantIndex: -2, wantSize: 20},
		{name: "pageSize=3", query: "?pageIndex=1&pageSize=3", wantCode: http.StatusOK, wantIndex: 1, wantSize: 3},
		{name: "pageSize 不设 100 上限", query: "?pageSize=1000", wantCode: http.StatusOK, wantIndex: 1, wantSize: 1000},
		{name: "pageSize 空串取默认", query: "?pageSize=", wantCode: http.StatusOK, wantIndex: 1, wantSize: 20},
		{name: "pageSize=0 → PageRequest 异常 → 500", query: "?pageSize=0", wantCode: http.StatusInternalServerError},
		{name: "pageSize=-1 → 500", query: "?pageSize=-1", wantCode: http.StatusInternalServerError},
		{name: "pageSize 非整数 → Spring int 绑定失败 → 500", query: "?pageSize=abc", wantCode: http.StatusInternalServerError},
		{name: "pageIndex 非整数 → 500", query: "?pageIndex=abc", wantCode: http.StatusInternalServerError},
		{name: "超出 int 范围 → 500", query: "?pageSize=2147483648", wantCode: http.StatusInternalServerError},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			recorder := server.do(t, http.MethodGet, PathAuditLogs+item.query, token, nil)
			if recorder.Code != item.wantCode {
				t.Fatalf("HTTP = %d, want %d (%s)", recorder.Code, item.wantCode, recorder.Body.String())
			}
			if item.wantCode != http.StatusOK {
				code, message, _ := decodeResult(t, recorder)
				if code != 500 || message != "服务器内部错误" {
					t.Fatalf("500 文案应与 一致: code=%d message=%q", code, message)
				}
				return
			}
			_, _, data := decodeResult(t, recorder)
			var page result.PageData[domain.AuditLog]
			if err := json.Unmarshal(data, &page); err != nil {
				t.Fatalf("分页结构异常: %v", err)
			}
			if page.PageIndex != item.wantIndex || page.PageSize != item.wantSize {
				t.Fatalf("分页回显异常: pageIndex=%d pageSize=%d", page.PageIndex, page.PageSize)
			}
			// 普通分页 headNodeTotal 必须为 null。
			if page.HeadNodeTotal != nil {
				t.Fatalf("普通分页 headNodeTotal 应为 null: %v", *page.HeadNodeTotal)
			}
		})
	}
}

// TestAuditCleanDaysBinding 锁定 days 的整型绑定语义（缺失/非法 → 500）。
func TestAuditCleanDaysBinding(t *testing.T) {
	server := newTestServer(t)
	token := server.login(t, domain.RootLoginID, nil)

	for _, item := range []struct {
		name  string
		query string
	}{
		{name: "缺失 days", query: ""},
		{name: "days 非整数", query: "?days=abc"},
		{name: "days 超 int 范围", query: "?days=2147483648"},
	} {
		t.Run(item.name, func(t *testing.T) {
			recorder := server.do(t, http.MethodDelete, PathAuditLogs+item.query, token, nil)
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("应 500，实际 %d (%s)", recorder.Code, recorder.Body.String())
			}
			code, message, _ := decodeResult(t, recorder)
			if code != 500 || message != "服务器内部错误" {
				t.Fatalf("500 文案应与 一致: code=%d message=%q", code, message)
			}
		})
	}

	// days=0：删除 createdAt < now 的记录，返回删除后剩余总数。
	recorder := server.do(t, http.MethodDelete, PathAuditLogs+"?days=0", token, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("days=0 应 200，实际 %d (%s)", recorder.Code, recorder.Body.String())
	}
}
