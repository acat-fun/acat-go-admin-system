package service

import (
	"context"
	"time"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

// ListAuditLogs。
// type 优先于 userType；createdAt DESC；pageIndex 1 基。
func (s *Service) ListAuditLogs(ctx context.Context, pageIndex, pageSize int, logType, userType string) (any, error) {
	page, err := s.audits.List(ctx, domain.AuditLogQuery{
		Type:      logType,
		UserType:  userType,
		PageIndex: pageIndex,
		PageSize:  pageSize,
	})
	if err != nil {
		return nil, err
	}
	return newPageData(page.List, page.Total, pageIndex, pageSize), nil
}

// CleanAuditLogs。
// 删除 now-days 之前的记录，返回**删除后剩余总数**（不是删除条数）。
func (s *Service) CleanAuditLogs(ctx context.Context, days int) (int64, error) {
	before := s.now().AddDate(0, 0, -days)
	return s.audits.DeleteBefore(ctx, before)
}

// nowWithClock 便于测试注入时间（保留 time 依赖）。
func (s *Service) nowWithClock() time.Time { return s.now() }
