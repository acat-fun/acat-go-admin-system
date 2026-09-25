package notification

import (
	"context"
	"testing"
)

// fakeRepo 是 Repo 的内存实现。
type fakeRepo struct {
	items    []Item
	markRead int64
	failAll  error
}

func (f *fakeRepo) ListByUser(_ context.Context, userID string, offset, limit int) ([]Item, int64, error) {
	var mine []Item
	for _, item := range f.items {
		if item.BookID != "" && item.ID == "user-"+userID+"-"+item.ID {
			// 简化：fake 不按 user 过滤，按 ID 约定演示
		}
		mine = append(mine, item)
	}
	total := int64(len(mine))
	start, end := offset, offset+limit
	if start > len(mine) {
		start = len(mine)
	}
	if end > len(mine) {
		end = len(mine)
	}
	return mine[start:end], total, nil
}

func (f *fakeRepo) UnreadCount(_ context.Context, userID string) (int64, error) {
	if f.failAll != nil {
		return 0, f.failAll
	}
	var count int64
	for _, item := range f.items {
		if !item.IsRead {
			count++
		}
	}
	return count, nil
}

func (f *fakeRepo) MarkRead(_ context.Context, userID, id string) (int64, error) {
	for i := range f.items {
		if f.items[i].ID == id && !f.items[i].IsRead {
			f.items[i].IsRead = true
			f.markRead++
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeRepo) MarkAllRead(_ context.Context, userID string) (int64, error) {
	var affected int64
	for i := range f.items {
		if !f.items[i].IsRead {
			f.items[i].IsRead = true
			affected++
		}
	}
	return affected, nil
}

func TestListPaginatesAndNormalizes(t *testing.T) {
	items := make([]Item, 0, 25)
	for i := 0; i < 25; i++ {
		items = append(items, Item{ID: string(rune('a' + i)), Title: "t", IsRead: i%2 == 0})
	}
	repo := &fakeRepo{items: items}
	svc, err := New(Options{Repo: repo})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}

	page, err := svc.List(context.Background(), "u1", 0, 0)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	// 缺省参数归一：pageIndex=1 / pageSize=20。
	if page.PageIndex != 1 || page.PageSize != 20 {
		t.Fatalf("缺省分页参数 = %d/%d", page.PageIndex, page.PageSize)
	}
	if page.Total != 25 || len(page.List) != 20 {
		t.Fatalf("total/list = %d/%d", page.Total, len(page.List))
	}
	if page.HeadNodeTotal != nil {
		t.Fatalf("headNodeTotal 应为 null")
	}

	page2, _ := svc.List(context.Background(), "u1", 2, 20)
	if len(page2.List) != 5 {
		t.Fatalf("第二页应为 5 条，实际 %d", len(page2.List))
	}
}

func TestListEmptyReturnsEmptyArray(t *testing.T) {
	svc, _ := New(Options{Repo: &fakeRepo{}})
	page, err := svc.List(context.Background(), "u1", 1, 20)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.List == nil || len(page.List) != 0 {
		t.Fatalf("空列表应输出 []，实际 %v", page.List)
	}
}

func TestUnreadCount(t *testing.T) {
	repo := &fakeRepo{items: []Item{
		{ID: "1", IsRead: true}, {ID: "2"}, {ID: "3"},
	}}
	svc, _ := New(Options{Repo: repo})
	count, err := svc.UnreadCount(context.Background(), "u1")
	if err != nil || count != 2 {
		t.Fatalf("未读数 = %d, err=%v", count, err)
	}
}

func TestMarkReadRequiresTrue(t *testing.T) {
	svc, _ := New(Options{Repo: &fakeRepo{}})
	if err := svc.MarkRead(context.Background(), "u1", "n1", false); err == nil {
		t.Fatal("isRead=false 必须返回「不支持的操作」")
	}
	if err := svc.MarkAllRead(context.Background(), "u1", false); err == nil {
		t.Fatal("isRead=false 必须返回「不支持的操作」")
	}
}

func TestMarkReadIdempotent(t *testing.T) {
	repo := &fakeRepo{items: []Item{{ID: "n1", IsRead: true}}}
	svc, _ := New(Options{Repo: repo})
	// 已读通知再次标记：幂等成功。
	if err := svc.MarkRead(context.Background(), "u1", "n1", true); err != nil {
		t.Fatalf("幂等标记失败: %v", err)
	}
}

func TestMarkAllRead(t *testing.T) {
	repo := &fakeRepo{items: []Item{{ID: "1"}, {ID: "2"}, {ID: "3", IsRead: true}}}
	svc, _ := New(Options{Repo: repo})
	if err := svc.MarkAllRead(context.Background(), "u1", true); err != nil {
		t.Fatalf("全部已读失败: %v", err)
	}
	count, _ := svc.UnreadCount(context.Background(), "u1")
	if count != 0 {
		t.Fatalf("全部已读后未读应为 0，实际 %d", count)
	}
}

func TestNewRequiresRepo(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("缺 Repo 必须报错")
	}
}
