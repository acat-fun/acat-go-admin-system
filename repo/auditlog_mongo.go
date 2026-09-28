package repo

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

// ErrAuditLogPageSizeInvalid 表示审计日志分页大小非法，
// 由 HTTP 层映射为 500「服务器内部错误」。
var ErrAuditLogPageSizeInvalid = errors.New("repo: 审计日志 pageSize 必须大于 0")

// MongoAuditLogStore 是 AuditLogStore 的 MongoDB 实现，
// 数据落在 audit_logs 集合。
//
// 文档形状（字段名 = 契约字段名，BSON 类型按下表映射）：
//
//	_id            ObjectId          id 为 null 时自动生成 ObjectId
//	                                 （读路径同时兼容字符串形态的 _id）
//	type           string
//	userId         string|null
//	username       string|null
//	userType       string|null
//	action         string|null
//	detail         string|null
//	ip             string|null
//	userAgent      string|null
//	requestUri     string|null
//	requestMethod  string|null
//	requestParams  string|null
//	createBy       string|null
//	updateBy       string|null
//	createdAt      Date(ms, UTC)      LocalDateTime ←→ Date 按 options.Location 换算
//	updatedAt      Date(ms, UTC)|null
type MongoAuditLogStore struct {
	client           *mongo.Client
	database         string
	collection       string
	operationTimeout time.Duration
	// location 是时间换算时区：
	// BSON Date（瞬时）→ LocalDateTime 文本的换算时区，默认 UTC。
	location *time.Location
}

// MongoAuditOptions 配置 MongoDB 审计日志存储。
type MongoAuditOptions struct {
	// URI 连接串（可含库名）。
	URI string
	// Database 库名；为空时使用 URI 中的库名（driver 默认 test）。
	Database string
	// Collection 集合名；为空时取 domain.AuditLogCollection（audit_logs）。
	Collection string
	// ConnectTimeout 建连/选主超时，默认 5s。
	ConnectTimeout time.Duration
	// OperationTimeout 单次 Mongo 操作超时，默认 5s。
	OperationTimeout time.Duration
	// Location 时间展示时区；nil 表示 UTC。
	Location *time.Location
}

// NewMongoAuditLogStore 建立 MongoDB 客户端并返回审计日志存储实现。
//
// 官方 driver v2 的 mongo.Connect 是惰性的，不在建连时探测服务器：
// Mongo 暂时不可用时构造仍然成功，服务照常启动，首次读写时才报错。
func NewMongoAuditLogStore(opts MongoAuditOptions) (*MongoAuditLogStore, error) {
	if opts.URI == "" {
		return nil, errors.New("repo: MongoDB URI 不能为空")
	}
	clientOptions := options.Client().ApplyURI(opts.URI)
	if opts.ConnectTimeout > 0 {
		clientOptions = clientOptions.SetConnectTimeout(opts.ConnectTimeout)
		clientOptions = clientOptions.SetServerSelectionTimeout(opts.ConnectTimeout)
	}
	client, err := mongo.Connect(clientOptions)
	if err != nil {
		return nil, fmt.Errorf("连接 MongoDB 失败: %w", err)
	}
	collection := opts.Collection
	if collection == "" {
		collection = domain.AuditLogCollection
	}
	location := opts.Location
	if location == nil {
		location = time.UTC
	}
	return &MongoAuditLogStore{
		client:           client,
		database:         opts.Database,
		collection:       collection,
		operationTimeout: opts.OperationTimeout,
		location:         location,
	}, nil
}

// Close 关闭 MongoDB 客户端。
func (s *MongoAuditLogStore) Close(ctx context.Context) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Disconnect(ctx)
}

// Ping 探活 MongoDB（就绪探针使用）。
func (s *MongoAuditLogStore) Ping(ctx context.Context) error {
	opCtx, cancel := s.withTimeout(ctx)
	defer cancel()
	return s.client.Ping(opCtx, nil)
}

// List 实现 AuditLogStore：
//
//   - 过滤：见 domain.AuditLogQuery（Type 优先，逐级 else-if）；
//   - 排序：createdAt DESC；
//   - 分页：skip = (pageIndex-1)*pageSize，limit = pageSize；total = 过滤后总文档数
//     （与当前页无关）。
func (s *MongoAuditLogStore) List(ctx context.Context, query domain.AuditLogQuery) (domain.AuditLogPage, error) {
	skip, limit, err := auditLogPageWindow(query)
	if err != nil {
		return domain.AuditLogPage{}, err
	}
	filter := auditLogFilter(query)
	opCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	total, err := s.collectionRef().CountDocuments(opCtx, filter)
	if err != nil {
		return domain.AuditLogPage{}, fmt.Errorf("统计审计日志失败: %w", err)
	}
	findOptions := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetSkip(skip).
		SetLimit(limit)
	cursor, err := s.collectionRef().Find(opCtx, filter, findOptions)
	if err != nil {
		return domain.AuditLogPage{}, fmt.Errorf("查询审计日志失败: %w", err)
	}
	defer func() { _ = cursor.Close(opCtx) }()

	// pageSize 无上限；容量上限只用于避免超大预分配。
	capacity := int(limit)
	if capacity > 1000 {
		capacity = 1000
	}
	entries := make([]domain.AuditLog, 0, capacity)
	for cursor.Next(opCtx) {
		var document auditLogDocument
		if err := cursor.Decode(&document); err != nil {
			return domain.AuditLogPage{}, fmt.Errorf("解析审计日志文档失败: %w", err)
		}
		entries = append(entries, document.toDomain(s.location))
	}
	if err := cursor.Err(); err != nil {
		return domain.AuditLogPage{}, fmt.Errorf("遍历审计日志失败: %w", err)
	}
	return domain.AuditLogPage{Total: total, List: entries}, nil
}

// auditLogPageWindow 计算分页的 skip/limit。
//
// pageSize < 1 时报错（HTTP 层映射为 500）。
func auditLogPageWindow(query domain.AuditLogQuery) (int64, int64, error) {
	if query.PageSize < 1 {
		return 0, 0, ErrAuditLogPageSizeInvalid
	}
	pageIndex := query.PageIndex
	if pageIndex < 1 {
		pageIndex = 1
	}
	return int64(pageIndex-1) * int64(query.PageSize), int64(query.PageSize), nil
}

// DeleteBefore 实现 AuditLogStore：删除 createdAt 严格早于 before 的文档，
// 返回**删除后剩余总数**。
func (s *MongoAuditLogStore) DeleteBefore(ctx context.Context, before time.Time) (int64, error) {
	opCtx, cancel := s.withTimeout(ctx)
	defer cancel()
	filter := bson.D{{Key: "createdAt", Value: bson.D{{Key: "$lt", Value: before}}}}
	if _, err := s.collectionRef().DeleteMany(opCtx, filter); err != nil {
		return 0, fmt.Errorf("清理审计日志失败: %w", err)
	}
	return s.Count(ctx)
}

// Count 实现 AuditLogStore。
func (s *MongoAuditLogStore) Count(ctx context.Context) (int64, error) {
	opCtx, cancel := s.withTimeout(ctx)
	defer cancel()
	total, err := s.collectionRef().CountDocuments(opCtx, bson.D{})
	if err != nil {
		return 0, fmt.Errorf("统计审计日志失败: %w", err)
	}
	return total, nil
}

// Insert 实现 AuditLogStore。
//
// id 为空时生成 ObjectId；createdAt 为空时取当前时间。
func (s *MongoAuditLogStore) Insert(ctx context.Context, entry domain.AuditLog) error {
	document, err := newAuditLogDocument(entry, s.location)
	if err != nil {
		return err
	}
	opCtx, cancel := s.withTimeout(ctx)
	defer cancel()
	if _, err := s.collectionRef().InsertOne(opCtx, document); err != nil {
		return fmt.Errorf("写入审计日志失败: %w", err)
	}
	return nil
}

// collectionRef 返回审计日志集合句柄。
func (s *MongoAuditLogStore) collectionRef() *mongo.Collection {
	return s.client.Database(s.database).Collection(s.collection)
}

// withTimeout 为单次操作套上超时（0 表示不额外限制）。
func (s *MongoAuditLogStore) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.operationTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, s.operationTimeout)
}

// auditLogDocument 是 audit_logs 集合的 BSON 模型。
//
// 字段名与对外契约字段名一致；无 `omitempty` 的可空字段在写入时落 BSON null
// （即 `detail: null` / `requestParams: null` 这类文档形态）。
type auditLogDocument struct {
	// ID。
	ID            any        `bson:"_id,omitempty"`
	Type          string     `bson:"type"`
	UserID        *string    `bson:"userId"`
	Username      *string    `bson:"username"`
	UserType      *string    `bson:"userType"`
	Action        *string    `bson:"action"`
	Detail        *string    `bson:"detail"`
	IP            *string    `bson:"ip"`
	UserAgent     *string    `bson:"userAgent"`
	RequestURI    *string    `bson:"requestUri"`
	RequestMethod *string    `bson:"requestMethod"`
	RequestParams *string    `bson:"requestParams"`
	CreateBy      *string    `bson:"createBy"`
	UpdateBy      *string    `bson:"updateBy"`
	CreatedAt     *time.Time `bson:"createdAt"`
	UpdatedAt     *time.Time `bson:"updatedAt"`
}

// newAuditLogDocument 把领域对象转换为 BSON 文档（写入路径）。
func newAuditLogDocument(entry domain.AuditLog, location *time.Location) (auditLogDocument, error) {
	created, ok := domain.ParseAuditDateTime(domain.DerefString(entry.CreatedAt), location)
	if !ok {
		created = time.Now()
	}
	created = created.Truncate(time.Millisecond)
	var updated *time.Time
	if parsed, ok := domain.ParseAuditDateTime(domain.DerefString(entry.UpdatedAt), location); ok {
		parsed = parsed.Truncate(time.Millisecond)
		updated = &parsed
	}
	return auditLogDocument{
		ID:            auditLogIDValue(entry.ID),
		Type:          entry.Type,
		UserID:        entry.UserID,
		Username:      entry.Username,
		UserType:      entry.UserType,
		Action:        entry.Action,
		Detail:        entry.Detail,
		IP:            entry.IP,
		UserAgent:     entry.UserAgent,
		RequestURI:    entry.RequestURI,
		RequestMethod: entry.RequestMethod,
		RequestParams: entry.RequestParams,
		CreateBy:      entry.CreateBy,
		UpdateBy:      entry.UpdateBy,
		CreatedAt:     &created,
		UpdatedAt:     updated,
	}, nil
}

// toDomain 把 BSON 文档转换为领域对象（读路径）。
//
// 缺失/null 的可空字段 → nil；`_id` 兼容 ObjectId 与字符串。
func (d auditLogDocument) toDomain(location *time.Location) domain.AuditLog {
	entry := domain.AuditLog{
		ID:            auditLogIDString(d.ID),
		Type:          d.Type,
		UserID:        d.UserID,
		Username:      d.Username,
		UserType:      d.UserType,
		Action:        d.Action,
		Detail:        d.Detail,
		IP:            d.IP,
		UserAgent:     d.UserAgent,
		RequestURI:    d.RequestURI,
		RequestMethod: d.RequestMethod,
		RequestParams: d.RequestParams,
		CreateBy:      d.CreateBy,
		UpdateBy:      d.UpdateBy,
	}
	if d.CreatedAt != nil {
		entry.CreatedAt = stringPtr(domain.FormatAuditDateTime(*d.CreatedAt, location))
	}
	if d.UpdatedAt != nil {
		entry.UpdatedAt = stringPtr(domain.FormatAuditDateTime(*d.UpdatedAt, location))
	}
	return entry
}

// auditLogFilter 构造过滤条件（主分支互斥见 domain.AuditLogQuery；Keyword/RequestMethod 叠加 AND）。
func auditLogFilter(query domain.AuditLogQuery) bson.D {
	filter := bson.D{}
	switch {
	case query.Type != "":
		filter = append(filter, bson.E{Key: "type", Value: query.Type})
	case query.UserType != "":
		filter = append(filter, bson.E{Key: "userType", Value: query.UserType})
	case query.UserID != "":
		filter = append(filter, bson.E{Key: "userId", Value: query.UserID})
	case query.CreatedFrom != nil && query.CreatedTo != nil:
		filter = append(filter, bson.E{Key: "createdAt", Value: bson.D{
			{Key: "$gte", Value: *query.CreatedFrom},
			{Key: "$lte", Value: *query.CreatedTo},
		}})
	case query.CreatedFrom != nil:
		filter = append(filter, bson.E{Key: "createdAt", Value: bson.D{{Key: "$gte", Value: *query.CreatedFrom}}})
	case query.CreatedTo != nil:
		filter = append(filter, bson.E{Key: "createdAt", Value: bson.D{{Key: "$lte", Value: *query.CreatedTo}}})
	}
	if method := strings.TrimSpace(query.RequestMethod); method != "" {
		filter = append(filter, bson.E{Key: "requestMethod", Value: method})
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		pattern := regexp.QuoteMeta(keyword)
		regex := bson.D{{Key: "$regex", Value: pattern}, {Key: "$options", Value: "i"}}
		filter = append(filter, bson.E{Key: "$or", Value: bson.A{
			bson.D{{Key: "username", Value: regex}},
			bson.D{{Key: "requestUri", Value: regex}},
			bson.D{{Key: "detail", Value: regex}},
		}})
	}
	return filter
}

// auditLogIDValue 把领域字符串 id 转换为 BSON `_id`：
// 24 位十六进制串按 ObjectId 写入，其余保持字符串。
func auditLogIDValue(id string) any {
	if id == "" {
		return bson.NewObjectID()
	}
	if objectID, err := bson.ObjectIDFromHex(id); err == nil {
		return objectID
	}
	return id
}

// auditLogIDString 把 BSON `_id` 转换为字符串输出形态。
func auditLogIDString(id any) string {
	switch typed := id.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bson.ObjectID:
		return typed.Hex()
	default:
		return fmt.Sprintf("%v", typed)
	}
}

// stringPtr 返回字符串指针（空串同样保留）。
func stringPtr(value string) *string { return &value }

// 编译期断言：Mongo 实现满足 AuditLogStore。
var _ AuditLogStore = (*MongoAuditLogStore)(nil)
