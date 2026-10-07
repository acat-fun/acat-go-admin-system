package repo

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

func newAuditSQLMock(t *testing.T, table string) (*MySQLAuditLogStore, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	store, err := NewMySQLAuditLogStore(MySQLAuditOptions{DB: db, Table: table})
	if err != nil {
		t.Fatalf("构造 MySQL 审计存储失败: %v", err)
	}
	cleanup := func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("SQL 预期未满足: %v", err)
		}
		_ = db.Close()
	}
	return store, mock, cleanup
}

// TestMySQLAuditLogEnsureSchemaCreatesSharedStructure 校验建表语句：表名可配、幂等、
// 列清单与建表列顺序一致（列名与 Mongo 文档字段的对应见 TestAuditLogStorageStructuresMatch）。
func TestMySQLAuditLogEnsureSchemaCreatesSharedStructure(t *testing.T) {
	store, mock, cleanup := newAuditSQLMock(t, "")
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS `" + AuditLogTableDefault + "`")).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("建表失败: %v", err)
	}

	ddl := store.createTableSQL()
	if !strings.Contains(ddl, "CREATE TABLE IF NOT EXISTS `"+AuditLogTableDefault+"`") {
		t.Fatalf("建表语句应为幂等建表: %s", ddl)
	}
	last := -1
	for _, column := range auditLogSQLColumns {
		index := strings.Index(ddl, "`"+column+"`")
		if index < 0 {
			t.Fatalf("建表语句缺少列 %s: %s", column, ddl)
		}
		if index < last {
			t.Fatalf("建表列顺序与列清单不一致（%s）: %s", column, ddl)
		}
		last = index
	}
}

// TestMySQLAuditLogInsertMapsDomainFields 校验写入：列顺序固定、可空字段落 NULL、
// 时间按配置时区写墙钟文本（不与 DSN 时区耦合）。
func TestMySQLAuditLogInsertMapsDomainFields(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("加载时区失败: %v", err)
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	store, err := NewMySQLAuditLogStore(MySQLAuditOptions{DB: db, Table: "t_acat_audit_log", Location: shanghai})
	if err != nil {
		t.Fatalf("构造存储失败: %v", err)
	}

	created := "2026-09-14T09:02:03.456"
	userID := "e2e-dualread-0001"
	username := "e2e_dualread"
	userType := domain.AuditLogUserTypeWorker
	action := "e2e_dualread 登录系统"
	detail := "登录成功"
	ip := "192.168.250.151"
	agent := "curl/8.14.1"
	uri := "/api/admin/user/auth/login"
	method := "POST"
	params := `{"username":"e2e_dualread"}`
	resourceType := "pipeline"
	resourceID := "12"

	insertSQL := regexp.QuoteMeta(store.insertSQL())
	mock.ExpectExec(insertSQL).WithArgs(
		"audit-1", domain.AuditLogTypeLogin, userID, username, userType, action, detail, ip,
		agent, uri, method, params, nil, nil,
		"2026-09-14 09:02:03.456", nil, resourceType, resourceID,
	).WillReturnResult(sqlmock.NewResult(1, 1))

	if err := store.Insert(context.Background(), domain.AuditLog{
		ID: "audit-1", Type: domain.AuditLogTypeLogin, UserID: &userID, Username: &username,
		UserType: &userType, Action: &action, Detail: &detail, IP: &ip, UserAgent: &agent,
		RequestURI: &uri, RequestMethod: &method, RequestParams: &params,
		CreatedAt: &created, ResourceType: &resourceType, ResourceID: &resourceID,
	}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL 预期未满足: %v", err)
	}
}

// TestMySQLAuditLogInsertFillsIDAndCreatedAt 校验 id/createdAt 缺省补全（与 Mongo 实现同语义）。
func TestMySQLAuditLogInsertFillsIDAndCreatedAt(t *testing.T) {
	store, mock, cleanup := newAuditSQLMock(t, "")
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta(store.insertSQL())).
		WithArgs(sqlmock.AnyArg(), domain.AuditLogTypeOperation, nil, nil, nil, nil, nil, nil,
			nil, nil, nil, nil, nil, nil, sqlmock.AnyArg(), nil, nil, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := store.Insert(context.Background(), domain.AuditLog{Type: domain.AuditLogTypeOperation}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
}

// TestMySQLAuditLogListBuildsFiltersPagingAndTimeText 校验查询主分支 + AND 过滤、
// 排序分页、以及 DATETIME 两种驱动形态（time.Time / []byte）的时间文本还原。
func TestMySQLAuditLogListBuildsFiltersPagingAndTimeText(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("加载时区失败: %v", err)
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	store, err := NewMySQLAuditLogStore(MySQLAuditOptions{DB: db, Table: "t_acat_audit_log", Location: shanghai})
	if err != nil {
		t.Fatalf("构造存储失败: %v", err)
	}

	where := " WHERE resource_type = ? AND action = ? AND username LIKE ? ESCAPE '\\\\'"
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM `t_acat_audit_log`"+where)).
		WithArgs("pipeline", "deploy", "%ad\\%%").
		WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(2))
	mock.ExpectQuery(regexp.QuoteMeta(store.selectSQL()+where+" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?")).
		WithArgs("pipeline", "deploy", "%ad\\%%", 20, 20).
		WillReturnRows(sqlmock.NewRows(auditLogSQLColumns).
			AddRow("migrated-2", "OPERATION", "7", "admin", "WORKER", "deploy", "发布完成", "10.0.0.2",
				"", "/api/v1/releases", "POST", "", nil, nil,
				time.Date(2026, 9, 14, 9, 2, 3, 456_000_000, shanghai), nil, "pipeline", "12").
			AddRow("migrated-1", "CREATE", nil, nil, nil, "create", nil, nil,
				nil, nil, nil, nil, nil, nil,
				[]byte("2026-09-13 19:27:01.862"), []byte("2026-09-13 19:27:02.000"), nil, nil))

	page, err := store.List(context.Background(), domain.AuditLogQuery{
		ResourceType: "pipeline", Action: "deploy", Username: "ad%", PageIndex: 2, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != 2 || len(page.List) != 2 {
		t.Fatalf("分页结果异常: total=%d len=%d", page.Total, len(page.List))
	}
	first := page.List[0]
	if domain.DerefString(first.CreatedAt) != "2026-09-14T09:02:03.456" ||
		domain.DerefString(first.Username) != "admin" ||
		domain.DerefString(first.ResourceType) != "pipeline" ||
		domain.DerefString(first.ResourceID) != "12" {
		t.Fatalf("首行解析异常: %+v", first)
	}
	if domain.DerefString(first.UserAgent) != "" || first.CreateBy != nil || first.UpdatedAt != nil {
		t.Fatalf("可空字段解析异常: %+v", first)
	}
	second := page.List[1]
	if domain.DerefString(second.CreatedAt) != "2026-09-13T19:27:01.862" ||
		domain.DerefString(second.UpdatedAt) != "2026-09-13T19:27:02" {
		t.Fatalf("墙钟文本还原异常: %+v", second)
	}
	if second.UserID != nil || second.Username != nil || second.Detail != nil {
		t.Fatalf("NULL 列应还原为 nil: %+v", second)
	}
}

// TestMySQLAuditLogWhereClauseMatchesQueryContract 校验主分支互斥与附加 AND 条件。
func TestMySQLAuditLogWhereClauseMatchesQueryContract(t *testing.T) {
	store := &MySQLAuditLogStore{table: AuditLogTableDefault, location: time.UTC}
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		query     domain.AuditLogQuery
		wantWhere string
		wantArgs  []any
	}{
		{
			name:      "type 优先于 userType/userId/时间",
			query:     domain.AuditLogQuery{Type: "CREATE", UserType: "WORKER", UserID: "u1", CreatedFrom: &from},
			wantWhere: " WHERE type = ?",
			wantArgs:  []any{"CREATE"},
		},
		{
			name:      "userType 次优先",
			query:     domain.AuditLogQuery{UserType: "WORKER", UserID: "u1"},
			wantWhere: " WHERE user_type = ?",
			wantArgs:  []any{"WORKER"},
		},
		{
			name:      "userId 再次",
			query:     domain.AuditLogQuery{UserID: "u1"},
			wantWhere: " WHERE user_id = ?",
			wantArgs:  []any{"u1"},
		},
		{
			name:      "闭区间",
			query:     domain.AuditLogQuery{CreatedFrom: &from, CreatedTo: &to},
			wantWhere: " WHERE created_at >= ? AND created_at <= ?",
			wantArgs:  []any{"2026-09-01 00:00:00.000", "2026-09-30 00:00:00.000"},
		},
		{
			name:      "仅下界",
			query:     domain.AuditLogQuery{CreatedFrom: &from},
			wantWhere: " WHERE created_at >= ?",
			wantArgs:  []any{"2026-09-01 00:00:00.000"},
		},
		{
			name:      "无过滤",
			query:     domain.AuditLogQuery{},
			wantWhere: "",
			wantArgs:  []any{},
		},
		{
			name: "叠加 AND 条件",
			query: domain.AuditLogQuery{
				ResourceType: "pipeline", Action: "deploy", Username: " ad ", Keyword: "50%",
				RequestMethod: " POST ",
			},
			wantWhere: " WHERE resource_type = ? AND action = ?" +
				" AND username LIKE ? ESCAPE '\\\\'" +
				" AND request_method = ?" +
				" AND (username LIKE ? ESCAPE '\\\\' OR request_uri LIKE ? ESCAPE '\\\\' OR detail LIKE ? ESCAPE '\\\\')",
			wantArgs: []any{"pipeline", "deploy", "%ad%", "POST", "%50\\%%", "%50\\%%", "%50\\%%"},
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			where, args := store.whereClause(item.query)
			if where != item.wantWhere {
				t.Fatalf("WHERE 子句异常:\n got=%q\nwant=%q", where, item.wantWhere)
			}
			if len(args) != len(item.wantArgs) {
				t.Fatalf("参数个数异常: got=%v want=%v", args, item.wantArgs)
			}
			for index := range args {
				if args[index] != item.wantArgs[index] {
					t.Fatalf("第 %d 个参数异常: got=%v want=%v", index, args[index], item.wantArgs[index])
				}
			}
		})
	}
}

// TestMySQLAuditLogDeleteBeforeAndCount 校验清理返回删除后剩余总数（与 Mongo 实现一致）。
func TestMySQLAuditLogDeleteBeforeAndCount(t *testing.T) {
	store, mock, cleanup := newAuditSQLMock(t, "")
	defer cleanup()

	before := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM `" + AuditLogTableDefault + "` WHERE created_at < ?")).
		WithArgs("2026-09-01 00:00:00.000").
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM `" + AuditLogTableDefault + "`")).
		WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(7))

	remaining, err := store.DeleteBefore(context.Background(), before)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if remaining != 7 {
		t.Fatalf("应返回删除后剩余总数: %d", remaining)
	}
}

// TestMySQLAuditLogRejectsInvalidOptionsAndPageSize 校验装配参数与分页参数的防御。
func TestMySQLAuditLogRejectsInvalidOptionsAndPageSize(t *testing.T) {
	if _, err := NewMySQLAuditLogStore(MySQLAuditOptions{}); err == nil {
		t.Fatal("缺少数据库句柄应报错")
	}
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := NewMySQLAuditLogStore(MySQLAuditOptions{DB: db, Table: "t_acat_audit_log; DROP TABLE x"}); err == nil {
		t.Fatal("非法表名应报错")
	}

	store, _, cleanup := newAuditSQLMock(t, "")
	defer cleanup()
	if _, err := store.List(context.Background(), domain.AuditLogQuery{PageSize: 0}); !errors.Is(err, ErrAuditLogPageSizeInvalid) {
		t.Fatalf("pageSize<1 应返回 ErrAuditLogPageSizeInvalid: %v", err)
	}
}

// TestMySQLAuditLogCustomTableAndPing 校验自定义表名与探活。
func TestMySQLAuditLogCustomTableAndPing(t *testing.T) {
	store, mock, cleanup := newAuditSQLMock(t, "t_audit_custom")
	defer cleanup()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT 1")).WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("探活失败: %v", err)
	}
	if store.Table() != "t_audit_custom" {
		t.Fatalf("表名异常: %s", store.Table())
	}
	if !strings.Contains(store.selectSQL(), "FROM `t_audit_custom`") {
		t.Fatalf("查询语句未使用自定义表名: %s", store.selectSQL())
	}
}

// TestAuditLogStorageStructuresMatch 是「Mongo 与 MySQL 结构一致」的机器守卫：
// Mongo 文档字段集合 ↔ 列映射表 ↔ 列清单必须一一对应，且建表语句覆盖全部列。
func TestAuditLogStorageStructuresMatch(t *testing.T) {
	documentFields := make([]string, 0, len(auditLogSQLColumns))
	documentType := reflect.TypeOf(auditLogDocument{})
	for index := 0; index < documentType.NumField(); index++ {
		name := strings.Split(documentType.Field(index).Tag.Get("bson"), ",")[0]
		if name == "" || name == "-" {
			t.Fatalf("Mongo 文档字段缺少 bson tag: %s", documentType.Field(index).Name)
		}
		documentFields = append(documentFields, name)
	}
	if len(documentFields) != len(auditLogFieldMapping) {
		t.Fatalf("Mongo 文档字段数与列映射不一致: %v vs %v", documentFields, auditLogFieldMapping)
	}
	for _, field := range documentFields {
		if _, ok := auditLogFieldMapping[field]; !ok {
			t.Fatalf("Mongo 文档字段 %s 缺少 MySQL 列映射", field)
		}
	}
	if len(auditLogSQLColumns) != len(auditLogFieldMapping) {
		t.Fatalf("MySQL 列数与字段映射不一致: %v", auditLogSQLColumns)
	}
	columns := make(map[string]bool, len(auditLogSQLColumns))
	for _, column := range auditLogSQLColumns {
		columns[column] = true
	}
	for field, column := range auditLogFieldMapping {
		if !columns[column] {
			t.Fatalf("字段 %s 映射的列 %s 不在列清单中", field, column)
		}
	}
	// 列顺序必须与文档字段顺序一致（id/_id 打头）。
	for index, field := range documentFields {
		if auditLogFieldMapping[field] != auditLogSQLColumns[index] {
			t.Fatalf("第 %d 列与文档字段顺序不一致: 字段=%s 列=%s", index, field, auditLogSQLColumns[index])
		}
	}
}

// TestParseAuditStoreKindDefaultsAndRejects 校验配置项解析：不填默认 mongo，非法取值报错。
func TestParseAuditStoreKindDefaultsAndRejects(t *testing.T) {
	cases := map[string]AuditStoreKind{
		"":        AuditStoreKindMongo,
		"  ":      AuditStoreKindMongo,
		"mongo":   AuditStoreKindMongo,
		"MongoDB": "",
		"mysql":   AuditStoreKindMySQL,
		"MySQL":   AuditStoreKindMySQL,
	}
	for raw, want := range cases {
		kind, err := ParseAuditStoreKind(raw)
		if want == "" {
			if err == nil {
				t.Fatalf("取值 %q 应报错", raw)
			}
			continue
		}
		if err != nil || kind != want {
			t.Fatalf("取值 %q 解析异常: kind=%v err=%v", raw, kind, err)
		}
	}
}

// TestNewAuditLogStoreSelectsBackend 校验装配：不填走 Mongo（缺 URI 报错），
// 填 mysql 走 MySQL 并自动建表（存 Mongo 时不建 MySQL 表）。
func TestNewAuditLogStoreSelectsBackend(t *testing.T) {
	if _, err := NewAuditLogStore(context.Background(), AuditStoreOptions{}); err == nil {
		t.Fatal("默认 mongo 且缺 URI 应报错")
	}
	if _, err := NewAuditLogStore(context.Background(), AuditStoreOptions{Kind: AuditStoreKind("redis")}); err == nil {
		t.Fatal("非法存储类型应报错")
	}

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS `" + AuditLogTableDefault + "`")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	store, err := NewAuditLogStore(context.Background(), AuditStoreOptions{
		Kind:  AuditStoreKindMySQL,
		MySQL: MySQLAuditOptions{DB: db},
	})
	if err != nil {
		t.Fatalf("装配 MySQL 审计存储失败: %v", err)
	}
	if _, ok := store.(*MySQLAuditLogStore); !ok {
		t.Fatalf("应返回 MySQL 实现: %T", store)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("应在装配时建表: %v", err)
	}
}
