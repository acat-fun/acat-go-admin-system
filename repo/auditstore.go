package repo

import (
	"context"
	"fmt"
	"strings"
)

// AuditStoreKind 是审计日志存储类型（宿主配置项取值）。
type AuditStoreKind string

// 存储类型取值。
const (
	// AuditStoreKindMongo MongoDB（配置项不填时的默认值）。
	AuditStoreKindMongo AuditStoreKind = "mongo"
	// AuditStoreKindMySQL MySQL。
	AuditStoreKindMySQL AuditStoreKind = "mysql"
)

// ParseAuditStoreKind 解析宿主配置项：空串（未配置）按默认 Mongo，大小写不敏感；
// 其它取值报错，不做静默回落。
func ParseAuditStoreKind(raw string) (AuditStoreKind, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return AuditStoreKindMongo, nil
	case string(AuditStoreKindMongo):
		return AuditStoreKindMongo, nil
	case string(AuditStoreKindMySQL):
		return AuditStoreKindMySQL, nil
	default:
		return "", fmt.Errorf("repo: 审计存储类型非法: %q（可选 mongo/mysql，不填默认 mongo）", raw)
	}
}

// AuditStoreCloser 由自有连接的实现提供（MongoDB）；MySQL 实现复用宿主连接池，Close 为空操作。
type AuditStoreCloser interface {
	// Close 释放存储自身持有的连接。
	Close(ctx context.Context) error
}

// AuditStorePinger 由可探活的实现提供（MongoDB 与 MySQL 实现都实现），供宿主就绪探针使用；
// 内存测试替身不实现，探针按「无探活能力」跳过。
type AuditStorePinger interface {
	// Ping 探活底层存储。
	Ping(ctx context.Context) error
}

// AuditStoreOptions 是审计存储装配参数：按 Kind 选用对应实现。
type AuditStoreOptions struct {
	// Kind 存储类型；空串按 Mongo。
	Kind AuditStoreKind
	// Mongo 选中 Mongo 时使用。
	Mongo MongoAuditOptions
	// MySQL 选中 MySQL 时使用。
	MySQL MySQLAuditOptions
}

// NewAuditLogStore 按配置装配审计存储：
//
//   - 未配置 / mongo：MongoDB 实现（URI 必填；缺失直接报错，不静默退化，避免审计变成丢写）；
//   - mysql：MySQL 实现，并立即 EnsureSchema 幂等建表——指定 MySQL 才建表，选 Mongo 不建表。
//
// 两套实现共享同一套过滤/排序/分页/时间口径，换存储不改变领域契约。
func NewAuditLogStore(ctx context.Context, opts AuditStoreOptions) (AuditLogStore, error) {
	kind, err := ParseAuditStoreKind(string(opts.Kind))
	if err != nil {
		return nil, err
	}
	if kind == AuditStoreKindMySQL {
		store, err := NewMySQLAuditLogStore(opts.MySQL)
		if err != nil {
			return nil, err
		}
		if err := store.EnsureSchema(ctx); err != nil {
			return nil, err
		}
		return store, nil
	}
	if strings.TrimSpace(opts.Mongo.URI) == "" {
		return nil, fmt.Errorf("repo: 审计存储为 %s 但未配置 MongoDB 连接串", AuditStoreKindMongo)
	}
	return NewMongoAuditLogStore(opts.Mongo)
}
