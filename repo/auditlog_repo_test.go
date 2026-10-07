package repo

import (
	"context"
	"testing"
	"time"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

func auditLog(logType, userType, createdAt string) domain.AuditLog {
	userTypeCopy := userType
	createdCopy := createdAt
	return domain.AuditLog{
		ID:        domain.NewID(),
		Type:      logType,
		UserType:  &userTypeCopy,
		CreatedAt: &createdCopy,
	}
}

func TestMemoryAuditLogListSortsAndPages(t *testing.T) {
	store := NewMemoryAuditLogStore()
	ctx := context.Background()
	entries := []domain.AuditLog{
		auditLog("LOGIN", "WORKER", "2026-09-01T10:00:00"),
		auditLog("CREATE", "WORKER", "2026-09-03T10:00:00"),
		auditLog("CREATE", "APP", "2026-09-02T10:00:00"),
	}
	for _, entry := range entries {
		if err := store.Insert(ctx, entry); err != nil {
			t.Fatalf("Insert 失败: %v", err)
		}
	}

	page, err := store.List(ctx, domain.AuditLogQuery{PageIndex: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if page.Total != 3 || len(page.List) != 2 {
		t.Fatalf("分页结果异常: total=%d len=%d", page.Total, len(page.List))
	}
	// createdAt DESC：09-03 → 09-02 → 09-01。
	if *page.List[0].CreatedAt != "2026-09-03T10:00:00" || *page.List[1].CreatedAt != "2026-09-02T10:00:00" {
		t.Fatalf("排序异常: %v/%v", *page.List[0].CreatedAt, *page.List[1].CreatedAt)
	}

	second, err := store.List(ctx, domain.AuditLogQuery{PageIndex: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(second.List) != 1 || *second.List[0].CreatedAt != "2026-09-01T10:00:00" {
		t.Fatalf("第二页异常: %+v", second.List)
	}
}

func TestMemoryAuditLogTypeBeatsUserType(t *testing.T) {
	store := NewMemoryAuditLogStore()
	ctx := context.Background()
	_ = store.Insert(ctx, auditLog("LOGIN", "WORKER", "2026-09-01T10:00:00"))
	_ = store.Insert(ctx, auditLog("CREATE", "APP", "2026-09-02T10:00:00"))

	page, err := store.List(ctx, domain.AuditLogQuery{Type: "CREATE", UserType: "WORKER", PageIndex: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if page.Total != 1 || page.List[0].Type != "CREATE" {
		t.Fatalf("type 过滤异常: %+v", page)
	}

	page, err = store.List(ctx, domain.AuditLogQuery{UserType: "APP", PageIndex: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if page.Total != 1 || *page.List[0].UserType != "APP" {
		t.Fatalf("userType 过滤异常: %+v", page)
	}
}

func TestMemoryAuditLogCleanReturnsRemaining(t *testing.T) {
	store := NewMemoryAuditLogStore()
	ctx := context.Background()
	_ = store.Insert(ctx, auditLog("LOGIN", "WORKER", "2026-08-01T10:00:00"))
	_ = store.Insert(ctx, auditLog("LOGIN", "WORKER", "2026-08-20T10:00:00"))
	_ = store.Insert(ctx, auditLog("LOGIN", "WORKER", "2026-09-10T10:00:00"))

	before := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	remaining, err := store.DeleteBefore(ctx, before)
	if err != nil {
		t.Fatalf("DeleteBefore 失败: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("剩余数应为 1，实际 %d", remaining)
	}
	total, err := store.Count(ctx)
	if err != nil || total != 1 {
		t.Fatalf("Count = %d, err = %v", total, err)
	}
}

func TestMemoryAuditLogInsertFillsDefaults(t *testing.T) {
	store := NewMemoryAuditLogStore()
	ctx := context.Background()
	if err := store.Insert(ctx, domain.AuditLog{Type: "OPERATION"}); err != nil {
		t.Fatalf("Insert 失败: %v", err)
	}
	page, err := store.List(ctx, domain.AuditLogQuery{PageIndex: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(page.List) != 1 || page.List[0].ID == "" || page.List[0].CreatedAt == nil {
		t.Fatalf("默认字段未填充: %+v", page.List)
	}
	if _, err := time.ParseInLocation(domain.DateTimeLayout, *page.List[0].CreatedAt, time.Local); err != nil {
		t.Fatalf("createdAt 格式异常: %v", err)
	}
}

// 编译期确认内存实现满足接口（真实 Mongo 实现的接入点）。
var _ AuditLogStore = (*MemoryAuditLogStore)(nil)

// TestMemoryAuditLogAdditionalFilters 校验资源/操作/操作人过滤为叠加 AND 条件
// （与 Mongo/MySQL 实现同语义）。
func TestMemoryAuditLogAdditionalFilters(t *testing.T) {
	store := NewMemoryAuditLogStore()
	ctx := context.Background()
	userID := "7"
	username := "Admin"
	action := "deploy"
	resourceType := "pipeline"
	resourceID := "12"
	other := "8"
	if err := store.Insert(ctx, domain.AuditLog{
		ID: "a1", Type: "OPERATION", UserID: &userID, Username: &username,
		Action: &action, ResourceType: &resourceType, ResourceID: &resourceID,
		CreatedAt: stringPtr("2026-09-14T09:02:03.456"),
	}); err != nil {
		t.Fatalf("Insert 失败: %v", err)
	}
	if err := store.Insert(ctx, domain.AuditLog{
		ID: "a2", Type: "CREATE", UserID: &other,
		CreatedAt: stringPtr("2026-09-14T09:02:04.000"),
	}); err != nil {
		t.Fatalf("Insert 失败: %v", err)
	}

	cases := []struct {
		name  string
		query domain.AuditLogQuery
		want  int
	}{
		{name: "资源类型精确", query: domain.AuditLogQuery{ResourceType: "pipeline"}, want: 1},
		{name: "资源类型不匹配", query: domain.AuditLogQuery{ResourceType: "server"}, want: 0},
		{name: "动作精确", query: domain.AuditLogQuery{Action: "deploy"}, want: 1},
		{name: "操作人包含且忽略大小写", query: domain.AuditLogQuery{Username: "adm"}, want: 1},
		{name: "操作人不匹配", query: domain.AuditLogQuery{Username: "root"}, want: 0},
		{name: "叠加 AND 全中", query: domain.AuditLogQuery{ResourceType: "pipeline", Action: "deploy", Username: "ADM"}, want: 1},
		{name: "叠加 AND 有一项不中", query: domain.AuditLogQuery{ResourceType: "pipeline", Action: "create"}, want: 0},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			page, err := store.List(ctx, domain.AuditLogQuery{
				Type: item.query.Type, UserType: item.query.UserType, UserID: item.query.UserID,
				Username: item.query.Username, Action: item.query.Action,
				ResourceType: item.query.ResourceType, Keyword: item.query.Keyword,
				PageIndex: 1, PageSize: 10,
			})
			if err != nil {
				t.Fatalf("List 失败: %v", err)
			}
			if int(page.Total) != item.want {
				t.Fatalf("命中数异常: got=%d want=%d", page.Total, item.want)
			}
		})
	}
}
