package repo

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

// AuditLogStore 是审计日志存储的可注入接口。
//
// 生产实现是 MongoAuditLogStore（MongoDB 集合 audit_logs）；
// MemoryAuditLogStore 只用于单元测试与无 Mongo 的本地测试场景，**不在 main 装配中使用**。
type AuditLogStore interface {
	// List 分页查询审计日志。
	List(ctx context.Context, query domain.AuditLogQuery) (domain.AuditLogPage, error)
	// DeleteBefore 删除 createdAt 早于 before 的记录，返回**删除后**的记录总数。
	DeleteBefore(ctx context.Context, before time.Time) (int64, error)
	// Count 返回当前记录总数。
	Count(ctx context.Context) (int64, error)
	// Insert 写入一条审计日志。
	Insert(ctx context.Context, log domain.AuditLog) error
}

// MemoryAuditLogStore 是线程安全的内存审计日志实现，**仅作测试替身**（进程重启即丢数据）。
//
// 语义与 MongoAuditLogStore 一致：
//   - 过滤：Type → UserType → UserID → 时间区间；
//   - 排序：createdAt DESC；
//   - 分页：pageIndex 1 基 → offset = (pageIndex-1)*pageSize；pageSize<1 与 Mongo 实现一样报错；
//   - clean：删除 createdAt < before 的记录，返回删除后的剩余总数。
type MemoryAuditLogStore struct {
	mu   sync.RWMutex
	logs []domain.AuditLog
	// now 便于测试注入固定时间。
	now func() time.Time
}

// NewMemoryAuditLogStore 构造内存实现。
func NewMemoryAuditLogStore() *MemoryAuditLogStore {
	return &MemoryAuditLogStore{logs: make([]domain.AuditLog, 0, 32), now: time.Now}
}

// List 实现 AuditLogStore。
func (s *MemoryAuditLogStore) List(_ context.Context, query domain.AuditLogQuery) (domain.AuditLogPage, error) {
	if query.PageSize < 1 {
		return domain.AuditLogPage{}, ErrAuditLogPageSizeInvalid
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	filtered := make([]domain.AuditLog, 0, len(s.logs))
	for _, entry := range s.logs {
		if !matchesAuditLogQuery(entry, query) {
			continue
		}
		filtered = append(filtered, entry)
	}
	// createdAt DESC；缺失时间视为零值（排最后），同值保持稳定顺序。
	sort.SliceStable(filtered, func(i, j int) bool {
		return parseAuditTime(filtered[i].CreatedAt).After(parseAuditTime(filtered[j].CreatedAt))
	})

	pageIndex := query.PageIndex
	if pageIndex < 1 {
		pageIndex = 1
	}
	from := (pageIndex - 1) * query.PageSize
	if from > len(filtered) {
		from = len(filtered)
	}
	to := from + query.PageSize
	if to > len(filtered) {
		to = len(filtered)
	}
	window := make([]domain.AuditLog, 0, to-from)
	window = append(window, filtered[from:to]...)
	return domain.AuditLogPage{Total: int64(len(filtered)), List: window}, nil
}

// matchesAuditLogQuery 判断一条记录是否命中查询条件（分支互斥优先级同仓储查询）。
func matchesAuditLogQuery(entry domain.AuditLog, query domain.AuditLogQuery) bool {
	created := parseAuditTime(entry.CreatedAt)
	switch {
	case query.Type != "":
		return entry.Type == query.Type
	case query.UserType != "":
		return entry.UserType != nil && *entry.UserType == query.UserType
	case query.UserID != "":
		return entry.UserID != nil && *entry.UserID == query.UserID
	case query.CreatedFrom != nil && query.CreatedTo != nil:
		// 时间区间为闭区间：两端都可取等。
		return !created.IsZero() && !created.Before(*query.CreatedFrom) && !created.After(*query.CreatedTo)
	case query.CreatedFrom != nil:
		return !created.IsZero() && !created.Before(*query.CreatedFrom)
	case query.CreatedTo != nil:
		return !created.IsZero() && !created.After(*query.CreatedTo)
	default:
		return true
	}
}

// DeleteBefore 实现 AuditLogStore。
func (s *MemoryAuditLogStore) DeleteBefore(_ context.Context, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]domain.AuditLog, 0, len(s.logs))
	for _, entry := range s.logs {
		created := parseAuditTime(entry.CreatedAt)
		if !created.IsZero() && created.Before(before) {
			continue
		}
		kept = append(kept, entry)
	}
	s.logs = kept
	return int64(len(kept)), nil
}

// Count 实现 AuditLogStore。
func (s *MemoryAuditLogStore) Count(_ context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.logs)), nil
}

// Insert 实现 AuditLogStore。
func (s *MemoryAuditLogStore) Insert(_ context.Context, log domain.AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if log.ID == "" {
		log.ID = domain.NewID()
	}
	if log.CreatedAt == nil {
		created := domain.FormatAuditDateTime(s.now(), nil)
		log.CreatedAt = &created
	}
	s.logs = append(s.logs, log)
	return nil
}

// parseAuditTime 解析审计日志的 createdAt 文本。
func parseAuditTime(value *string) time.Time {
	if value == nil || strings.TrimSpace(*value) == "" {
		return time.Time{}
	}
	if parsed, ok := domain.ParseAuditDateTime(*value, time.UTC); ok {
		return parsed
	}
	return time.Time{}
}
