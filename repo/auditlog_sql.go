package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/acat-fun/acat-go-common/mysqlx"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

// AuditLogTableDefault 是 MySQL 审计表的默认表名。
const AuditLogTableDefault = "t_acat_audit_log"

// 时间列在 MySQL 中的墙钟文本形态（DATETIME(3)）：写入与比较都按此格式字符串下发，
// 不与宿主的 DSN 时区参数（parseTime/loc）耦合。
const auditLogSQLTimeLayout = "2006-01-02 15:04:05.000"

// auditLogTableNamePattern 限定表名形态，防止配置项拼出任意 SQL。
var auditLogTableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// MySQLAuditLogStore 是 AuditLogStore 的 MySQL 实现，表结构与 MongoDB 集合 audit_logs
// 一一对应（见 auditLogFieldMapping）：换存储不改变领域契约。
//
// 与 Mongo 实现的行为约定：
//   - 过滤：Type → UserType → UserID → 时间区间主分支互斥，Username/Action/ResourceType/
//     RequestMethod/Keyword 叠加 AND；Username 与 Keyword 为不区分大小写的包含匹配；
//   - 排序：created_at DESC, id DESC（同一毫秒的分页稳定）；
//   - 分页：LIMIT pageSize OFFSET (pageIndex-1)*pageSize；total 为过滤后总数，与当前页无关；
//   - 时间：文本 ↔ DATETIME(3) 按 Options.Location 换算，输出走 domain.FormatAuditDateTime；
//   - id：为空时生成 UUID v7（Mongo 侧生成 ObjectId，两者都是字符串主键）。
type MySQLAuditLogStore struct {
	db       DBTX
	table    string
	location *time.Location
}

// MySQLAuditOptions 配置 MySQL 审计日志存储。
type MySQLAuditOptions struct {
	// DB 数据库句柄（*sql.DB 或 *sql.Tx 均可）；必填。
	DB DBTX
	// Table 表名；为空时取 AuditLogTableDefault。
	Table string
	// Location 时间换算时区（与 Mongo 实现的 Option 同义）；nil 表示 UTC。
	Location *time.Location
}

// NewMySQLAuditLogStore 构造 MySQL 审计日志存储。
//
// 只校验参数、不建表：建表由 EnsureSchema 负责（NewAuditLogStore 在选中 MySQL 时自动调用）。
func NewMySQLAuditLogStore(opts MySQLAuditOptions) (*MySQLAuditLogStore, error) {
	if opts.DB == nil {
		return nil, errors.New("repo: MySQL 审计存储需要数据库句柄")
	}
	table := strings.TrimSpace(opts.Table)
	if table == "" {
		table = AuditLogTableDefault
	}
	if !auditLogTableNamePattern.MatchString(table) {
		return nil, fmt.Errorf("repo: 审计表名非法: %q", opts.Table)
	}
	location := opts.Location
	if location == nil {
		location = time.UTC
	}
	return &MySQLAuditLogStore{db: opts.DB, table: table, location: location}, nil
}

// Table 返回生效的表名。
func (s *MySQLAuditLogStore) Table() string { return s.table }

// EnsureSchema 幂等建表（CREATE TABLE IF NOT EXISTS），列与 Mongo 文档字段一一对应。
//
// 选中 MySQL 存储时必须先建表：由 NewAuditLogStore 自动调用，也可由宿主在迁移中显式调用。
func (s *MySQLAuditLogStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, s.createTableSQL()); err != nil {
		return fmt.Errorf("建审计日志表失败: %w", err)
	}
	return nil
}

// Ping 探活数据库（就绪探针使用）。
func (s *MySQLAuditLogStore) Ping(ctx context.Context) error {
	var one int
	if err := s.db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("审计日志存储探活失败: %w", err)
	}
	return nil
}

// Close 为空操作：连接池由宿主持有，存储不接管其生命周期。
func (s *MySQLAuditLogStore) Close(context.Context) error { return nil }

// Insert 实现 AuditLogStore：id 为空时生成 UUID v7，createdAt 为空时取当前时间。
func (s *MySQLAuditLogStore) Insert(ctx context.Context, entry domain.AuditLog) error {
	id := entry.ID
	if id == "" {
		id = domain.NewID()
	}
	created, ok := domain.ParseAuditDateTime(domain.DerefString(entry.CreatedAt), s.location)
	if !ok {
		created = time.Now()
	}
	var updated any
	if parsed, ok := domain.ParseAuditDateTime(domain.DerefString(entry.UpdatedAt), s.location); ok {
		updated = s.timeValue(parsed)
	}
	args := []any{
		id,
		entry.Type,
		nullableString(entry.UserID),
		nullableString(entry.Username),
		nullableString(entry.UserType),
		nullableString(entry.Action),
		nullableString(entry.Detail),
		nullableString(entry.IP),
		nullableString(entry.UserAgent),
		nullableString(entry.RequestURI),
		nullableString(entry.RequestMethod),
		nullableString(entry.RequestParams),
		nullableString(entry.CreateBy),
		nullableString(entry.UpdateBy),
		s.timeValue(created),
		updated,
		nullableString(entry.ResourceType),
		nullableString(entry.ResourceID),
	}
	if _, err := s.db.ExecContext(ctx, s.insertSQL(), args...); err != nil {
		return fmt.Errorf("写入审计日志失败: %w", err)
	}
	return nil
}

// List 实现 AuditLogStore：过滤主分支互斥、附加条件 AND，created_at DESC, id DESC，分页同 Mongo。
func (s *MySQLAuditLogStore) List(ctx context.Context, query domain.AuditLogQuery) (domain.AuditLogPage, error) {
	skip, limit, err := auditLogPageWindow(query)
	if err != nil {
		return domain.AuditLogPage{}, err
	}
	where, args := s.whereClause(query)

	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+s.quotedTable()+where, args...).Scan(&total); err != nil {
		return domain.AuditLogPage{}, fmt.Errorf("统计审计日志失败: %w", err)
	}

	listArgs := append(append([]any{}, args...), limit, skip)
	rows, err := s.db.QueryContext(ctx,
		s.selectSQL()+where+" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?", listArgs...)
	if err != nil {
		return domain.AuditLogPage{}, fmt.Errorf("查询审计日志失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	capacity := query.PageSize
	if capacity > 1000 {
		capacity = 1000
	}
	entries := make([]domain.AuditLog, 0, capacity)
	for rows.Next() {
		entry, err := s.scanRow(rows)
		if err != nil {
			return domain.AuditLogPage{}, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return domain.AuditLogPage{}, fmt.Errorf("遍历审计日志失败: %w", err)
	}
	return domain.AuditLogPage{Total: total, List: entries}, nil
}

// DeleteBefore 实现 AuditLogStore：删除 created_at 严格早于 before 的记录，
// 返回**删除后剩余总数**（与 Mongo 实现一致）。
func (s *MySQLAuditLogStore) DeleteBefore(ctx context.Context, before time.Time) (int64, error) {
	if _, err := s.db.ExecContext(ctx,
		"DELETE FROM "+s.quotedTable()+" WHERE created_at < ?", s.timeValue(before)); err != nil {
		return 0, fmt.Errorf("清理审计日志失败: %w", err)
	}
	return s.Count(ctx)
}

// Count 实现 AuditLogStore。
func (s *MySQLAuditLogStore) Count(ctx context.Context) (int64, error) {
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+s.quotedTable()).Scan(&total); err != nil {
		return 0, fmt.Errorf("统计审计日志失败: %w", err)
	}
	return total, nil
}

// scanRow 读取一行并转换为领域对象（时间列表按 Location 还原为文本）。
func (s *MySQLAuditLogStore) scanRow(rows *sql.Rows) (domain.AuditLog, error) {
	var (
		id            string
		logType       string
		userID        sql.NullString
		username      sql.NullString
		userType      sql.NullString
		action        sql.NullString
		detail        sql.NullString
		ip            sql.NullString
		userAgent     sql.NullString
		requestURI    sql.NullString
		requestMethod sql.NullString
		requestParams sql.NullString
		createBy      sql.NullString
		updateBy      sql.NullString
		createdAt     any
		updatedAt     any
		resourceType  sql.NullString
		resourceID    sql.NullString
	)
	if err := rows.Scan(&id, &logType, &userID, &username, &userType, &action, &detail, &ip,
		&userAgent, &requestURI, &requestMethod, &requestParams, &createBy, &updateBy,
		&createdAt, &updatedAt, &resourceType, &resourceID); err != nil {
		return domain.AuditLog{}, fmt.Errorf("扫描审计日志失败: %w", err)
	}
	entry := domain.AuditLog{
		ID:            id,
		Type:          logType,
		UserID:        mysqlx.NullString(userID),
		Username:      mysqlx.NullString(username),
		UserType:      mysqlx.NullString(userType),
		Action:        mysqlx.NullString(action),
		Detail:        mysqlx.NullString(detail),
		IP:            mysqlx.NullString(ip),
		UserAgent:     mysqlx.NullString(userAgent),
		RequestURI:    mysqlx.NullString(requestURI),
		RequestMethod: mysqlx.NullString(requestMethod),
		RequestParams: mysqlx.NullString(requestParams),
		CreateBy:      mysqlx.NullString(createBy),
		UpdateBy:      mysqlx.NullString(updateBy),
		ResourceType:  mysqlx.NullString(resourceType),
		ResourceID:    mysqlx.NullString(resourceID),
	}
	if parsed, ok := s.parseSQLTime(createdAt); ok {
		entry.CreatedAt = stringPtr(domain.FormatAuditDateTime(parsed, s.location))
	}
	if parsed, ok := s.parseSQLTime(updatedAt); ok {
		entry.UpdatedAt = stringPtr(domain.FormatAuditDateTime(parsed, s.location))
	}
	return entry, nil
}

// whereClause 构造 WHERE 子句（含前导空格；无条件下为空串）。
//
// 主分支互斥与 Mongo 实现逐条对齐；附加条件全部 AND 叠加。
func (s *MySQLAuditLogStore) whereClause(query domain.AuditLogQuery) (string, []any) {
	clauses := make([]string, 0, 8)
	args := make([]any, 0, 8)
	switch {
	case query.Type != "":
		clauses = append(clauses, "type = ?")
		args = append(args, query.Type)
	case query.UserType != "":
		clauses = append(clauses, "user_type = ?")
		args = append(args, query.UserType)
	case query.UserID != "":
		clauses = append(clauses, "user_id = ?")
		args = append(args, query.UserID)
	case query.CreatedFrom != nil && query.CreatedTo != nil:
		clauses = append(clauses, "created_at >= ?", "created_at <= ?")
		args = append(args, s.timeValue(*query.CreatedFrom), s.timeValue(*query.CreatedTo))
	case query.CreatedFrom != nil:
		clauses = append(clauses, "created_at >= ?")
		args = append(args, s.timeValue(*query.CreatedFrom))
	case query.CreatedTo != nil:
		clauses = append(clauses, "created_at <= ?")
		args = append(args, s.timeValue(*query.CreatedTo))
	}
	if resourceType := strings.TrimSpace(query.ResourceType); resourceType != "" {
		clauses = append(clauses, "resource_type = ?")
		args = append(args, resourceType)
	}
	if action := strings.TrimSpace(query.Action); action != "" {
		clauses = append(clauses, "action = ?")
		args = append(args, action)
	}
	if username := strings.TrimSpace(query.Username); username != "" {
		clauses = append(clauses, `username LIKE ? ESCAPE '\\'`)
		args = append(args, auditLogLikePattern(username))
	}
	if method := strings.TrimSpace(query.RequestMethod); method != "" {
		clauses = append(clauses, "request_method = ?")
		args = append(args, method)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		pattern := auditLogLikePattern(keyword)
		clauses = append(clauses, `(username LIKE ? ESCAPE '\\' OR request_uri LIKE ? ESCAPE '\\' OR detail LIKE ? ESCAPE '\\')`)
		args = append(args, pattern, pattern, pattern)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// createTableSQL 返回建表语句（表名与列清单来自共享契约）。
func (s *MySQLAuditLogStore) createTableSQL() string {
	ddl, err := AuditLogTableDDL(s.table)
	if err != nil {
		// 表名在构造时已校验，此处不会失败。
		panic(err)
	}
	return ddl
}

// AuditLogTableDDL 返回审计表的建表语句（与 EnsureSchema 使用同一份定义），
// 供宿主迁移脚本对照与防漂移测试使用。
func AuditLogTableDDL(table string) (string, error) {
	name := strings.TrimSpace(table)
	if name == "" {
		name = AuditLogTableDefault
	}
	if !auditLogTableNamePattern.MatchString(name) {
		return "", fmt.Errorf("repo: 审计表名非法: %q", table)
	}
	return auditLogTableDDL(name), nil
}

// auditLogTableDDL 生成建表语句：列与 Mongo 文档字段一一对应，索引覆盖审计页的常用筛选。
func auditLogTableDDL(table string) string {
	return "CREATE TABLE IF NOT EXISTS `" + table + "` (" + `
  ` + "`id`" + `             VARCHAR(64)  NOT NULL COMMENT '主键（Mongo _id 的字符串形态）',
  ` + "`type`" + `           VARCHAR(32)  NOT NULL COMMENT '日志类型：LOGIN/CREATE/UPDATE/PATCH/DELETE/OPERATION',
  ` + "`user_id`" + `        VARCHAR(64)      NULL COMMENT '操作人 id',
  ` + "`username`" + `       VARCHAR(191)     NULL COMMENT '操作人名称',
  ` + "`user_type`" + `      VARCHAR(32)      NULL COMMENT '用户类型：WORKER/APP',
  ` + "`action`" + `         VARCHAR(191)     NULL COMMENT '操作描述',
  ` + "`detail`" + `         TEXT             NULL COMMENT '脱敏详情',
  ` + "`ip`" + `             VARCHAR(64)      NULL COMMENT '来源 IP',
  ` + "`user_agent`" + `     VARCHAR(255)     NULL COMMENT 'User-Agent',
  ` + "`request_uri`" + `    VARCHAR(255)     NULL COMMENT '请求路径',
  ` + "`request_method`" + ` VARCHAR(16)      NULL COMMENT '请求方法',
  ` + "`request_params`" + ` TEXT             NULL COMMENT '请求参数摘要（截断）',
  ` + "`create_by`" + `      VARCHAR(64)      NULL COMMENT '创建人',
  ` + "`update_by`" + `      VARCHAR(64)      NULL COMMENT '更新人',
  ` + "`created_at`" + `     DATETIME(3)  NOT NULL COMMENT '创建时间（按存储时区的墙钟）',
  ` + "`updated_at`" + `     DATETIME(3)      NULL COMMENT '更新时间（按存储时区的墙钟）',
  ` + "`resource_type`" + `  VARCHAR(64)      NULL COMMENT '资源类型（宿主平台审计）',
  ` + "`resource_id`" + `    VARCHAR(64)      NULL COMMENT '资源 id（宿主平台审计）',
  PRIMARY KEY (` + "`id`" + `),
  KEY ` + "`idx_audit_created`" + ` (` + "`created_at`" + `, ` + "`id`" + `),
  KEY ` + "`idx_audit_type`" + ` (` + "`type`" + `, ` + "`created_at`" + `),
  KEY ` + "`idx_audit_resource`" + ` (` + "`resource_type`" + `, ` + "`resource_id`" + `),
  KEY ` + "`idx_audit_username`" + ` (` + "`username`" + `)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='审计日志（与 Mongo 集合 audit_logs 同构）'`
}

// insertSQL 返回插入语句（列顺序 = auditLogSQLColumns）。
func (s *MySQLAuditLogStore) insertSQL() string {
	columns := strings.Join(quotedColumns(auditLogSQLColumns), ", ")
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(auditLogSQLColumns)), ", ")
	return "INSERT INTO " + s.quotedTable() + " (" + columns + ") VALUES (" + placeholders + ")"
}

// selectSQL 返回查询语句（列顺序 = auditLogSQLColumns）。
func (s *MySQLAuditLogStore) selectSQL() string {
	return "SELECT " + strings.Join(quotedColumns(auditLogSQLColumns), ", ") + " FROM " + s.quotedTable()
}

// quotedTable 返回反引号包裹的表名。
func (s *MySQLAuditLogStore) quotedTable() string { return "`" + s.table + "`" }

// timeValue 把瞬时转换为存储时区的墙钟字符串（不与 DSN 时区参数耦合）。
func (s *MySQLAuditLogStore) timeValue(value time.Time) string {
	return value.In(s.location).Truncate(time.Millisecond).Format(auditLogSQLTimeLayout)
}

// parseSQLTime 把 DATETIME 列还原为瞬时：无论驱动是否开启 parseTime，都按存储时区解释墙钟。
func (s *MySQLAuditLogStore) parseSQLTime(value any) (time.Time, bool) {
	var text string
	switch typed := value.(type) {
	case nil:
		return time.Time{}, false
	case time.Time:
		text = typed.Format(auditLogSQLTimeLayout)
	case []byte:
		text = string(typed)
	case string:
		text = typed
	default:
		return time.Time{}, false
	}
	parsed, err := time.ParseInLocation(auditLogSQLTimeLayout, strings.TrimSpace(text), s.location)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// quotedColumns 返回反引号包裹的列清单。
func quotedColumns(columns []string) []string {
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted = append(quoted, "`"+column+"`")
	}
	return quoted
}

// auditLogLikePattern 构造 LIKE 的子串模式：转义 % _ \ 后两侧加通配符。
func auditLogLikePattern(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(value) + "%"
}

// nullableString 把可空文本转为驱动可写入的值：nil 保持 NULL，空串按空串写入（与 Mongo 的
// "字段存在但为空" 形态对齐）。
func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// auditLogSQLColumns 是审计表列清单（顺序与写入/读取语句一致；不含表名）。
var auditLogSQLColumns = []string{
	"id", "type", "user_id", "username", "user_type", "action", "detail", "ip",
	"user_agent", "request_uri", "request_method", "request_params",
	"create_by", "update_by", "created_at", "updated_at", "resource_type", "resource_id",
}

// auditLogFieldMapping 是 Mongo 文档字段（含 _id）到 MySQL 列的映射，
// 两套存储的「结构一致」由 TestAuditLogStorageStructuresMatch 按本表比对。
var auditLogFieldMapping = map[string]string{
	"_id":           "id",
	"type":          "type",
	"userId":        "user_id",
	"username":      "username",
	"userType":      "user_type",
	"action":        "action",
	"detail":        "detail",
	"ip":            "ip",
	"userAgent":     "user_agent",
	"requestUri":    "request_uri",
	"requestMethod": "request_method",
	"requestParams": "request_params",
	"createBy":      "create_by",
	"updateBy":      "update_by",
	"createdAt":     "created_at",
	"updatedAt":     "updated_at",
	"resourceType":  "resource_type",
	"resourceId":    "resource_id",
}

// 编译期断言：MySQL 实现满足 AuditLogStore。
var _ AuditLogStore = (*MySQLAuditLogStore)(nil)
