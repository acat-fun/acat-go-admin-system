package repo

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

// javaLoginDocument 是既有写入形态的 golden 文档。
var javaLoginDocument = bson.D{
	{Key: "_id", Value: bson.NewObjectIDFromTimestamp(time.Date(2026, 9, 13, 19, 27, 1, 0, time.UTC))},
	{Key: "type", Value: "LOGIN"},
	{Key: "userId", Value: "e2e-dualread-0001"},
	{Key: "username", Value: "e2e_dualread"},
	{Key: "userType", Value: "WORKER"},
	{Key: "action", Value: "e2e_dualread 登录系统"},
	{Key: "detail", Value: nil},
	{Key: "ip", Value: "192.168.250.151"},
	{Key: "userAgent", Value: "curl/8.14.1"},
	{Key: "requestUri", Value: "/api/admin/user/auth/login"},
	{Key: "requestMethod", Value: "POST"},
	{Key: "requestParams", Value: nil},
	{Key: "createdAt", Value: bson.NewDateTimeFromTime(time.Date(2026, 9, 13, 19, 27, 1, 862_000_000, time.UTC))},
}

// TestAuditLogDocumentDecodesLegacyRow 校验能读出既有形态的行
func TestAuditLogDocumentDecodesLegacyRow(t *testing.T) {
	raw, err := bson.Marshal(javaLoginDocument)
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}
	var document auditLogDocument
	if err := bson.Unmarshal(raw, &document); err != nil {
		t.Fatalf("unmarshal 失败: %v", err)
	}
	entry := document.toDomain(time.UTC)

	if entry.ID != document.ID.(bson.ObjectID).Hex() || len(entry.ID) != 24 {
		t.Fatalf("_id 应转换为 24 位十六进制字符串: %q", entry.ID)
	}
	if entry.Type != "LOGIN" || domain.DerefString(entry.UserID) != "e2e-dualread-0001" ||
		domain.DerefString(entry.Username) != "e2e_dualread" ||
		domain.DerefString(entry.UserType) != "WORKER" {
		t.Fatalf("身份字段解析异常: %+v", entry)
	}
	if domain.DerefString(entry.Action) != "e2e_dualread 登录系统" ||
		domain.DerefString(entry.RequestURI) != "/api/admin/user/auth/login" ||
		domain.DerefString(entry.RequestMethod) != "POST" {
		t.Fatalf("请求字段解析异常: %+v", entry)
	}
	// 缺失的 createBy/updateBy/updatedAt → null。
	if entry.CreateBy != nil || entry.UpdateBy != nil || entry.UpdatedAt != nil {
		t.Fatalf("未写入的审计字段应为 null: %+v", entry)
	}
	// BSON Date → 时间文本：Jackson ISO_LOCAL_DATE_TIME（去尾随零）。
	if domain.DerefString(entry.CreatedAt) != "2026-09-13T19:27:01.862" {
		t.Fatalf("createdAt 文本异常: %q", domain.DerefString(entry.CreatedAt))
	}
}

// TestAuditLogDocumentDecodesUnknownFieldsAndStringID 容忍历史/异构文档：
// 字符串 _id、缺失的可空字段、额外的未知字段都不应导致失败。
func TestAuditLogDocumentDecodesUnknownFieldsAndStringID(t *testing.T) {
	raw, err := bson.Marshal(bson.D{
		{Key: "_id", Value: "legacy-string-id"},
		{Key: "type", Value: "CREATE"},
		{Key: "extraField", Value: "ignored"},
		{Key: "createdAt", Value: bson.NewDateTimeFromTime(time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))},
	})
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}
	var document auditLogDocument
	if err := bson.Unmarshal(raw, &document); err != nil {
		t.Fatalf("unmarshal 失败: %v", err)
	}
	entry := document.toDomain(time.UTC)
	if entry.ID != "legacy-string-id" {
		t.Fatalf("字符串 _id 应原样返回: %q", entry.ID)
	}
	if entry.UserID != nil || entry.Username != nil || entry.CreatedAt == nil {
		t.Fatalf("缺失字段应为 nil: %+v", entry)
	}
	if domain.DerefString(entry.CreatedAt) != "2026-09-14T00:00:00" {
		t.Fatalf("无毫秒时应输出秒精度: %q", domain.DerefString(entry.CreatedAt))
	}
}

// TestNewAuditLogDocumentFieldNames 校验写出的文档字段名与格式
func TestNewAuditLogDocumentFieldNames(t *testing.T) {
	userID := "0"
	username := "root"
	userType := "WORKER"
	action := "POST /api/admin/system/dicts"
	detail := "DictAdminController.createDict"
	ip := "10.0.0.1"
	agent := "curl/8.14.1"
	uri := "/api/admin/system/dicts"
	method := "POST"
	params := "{\"code\":\"book_tag\"}"
	created := "2026-09-14T01:02:03.456"

	document, err := newAuditLogDocument(domain.AuditLog{
		Type:          domain.AuditLogTypeCreate,
		UserID:        &userID,
		Username:      &username,
		UserType:      &userType,
		Action:        &action,
		Detail:        &detail,
		IP:            &ip,
		UserAgent:     &agent,
		RequestURI:    &uri,
		RequestMethod: &method,
		RequestParams: &params,
		CreatedAt:     &created,
	}, time.UTC)
	if err != nil {
		t.Fatalf("构造文档失败: %v", err)
	}
	raw, err := bson.Marshal(document)
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}
	var stored bson.D
	if err := bson.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal 失败: %v", err)
	}

	keys := make([]string, 0, len(stored))
	values := map[string]any{}
	for _, element := range stored {
		keys = append(keys, element.Key)
		values[element.Key] = element.Value
	}
	// 字段集合 = AuditLogEntity 的 16 个字段（id → _id）。
	wantKeys := []string{
		"_id", "type", "userId", "username", "userType", "action", "detail", "ip",
		"userAgent", "requestUri", "requestMethod", "requestParams", "createBy", "updateBy",
		"createdAt", "updatedAt",
	}
	if len(keys) != len(wantKeys) {
		t.Fatalf("字段集合异常: %v", keys)
	}
	for index, key := range wantKeys {
		if keys[index] != key {
			t.Fatalf("字段顺序/名称与 实体不一致: %v", keys)
		}
	}
	if _, ok := values["_id"].(bson.ObjectID); !ok {
		t.Fatalf("_id 应为 ObjectId: %T", values["_id"])
	}
	if values["createBy"] != nil || values["updateBy"] != nil || values["updatedAt"] != nil {
		t.Fatalf("审计字段应写 null: %+v", values)
	}
	createdAt, ok := values["createdAt"].(bson.DateTime)
	if !ok {
		t.Fatalf("createdAt 应为 BSON Date: %T", values["createdAt"])
	}
	if got := createdAt.Time().UTC().Format(time.RFC3339Nano); got != "2026-09-14T01:02:03.456Z" {
		t.Fatalf("createdAt 值异常: %s", got)
	}
}

// TestNewAuditLogDocumentParsesMillisInConfiguredZone 校验时区换算：
func TestNewAuditLogDocumentParsesMillisInConfiguredZone(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("加载时区失败: %v", err)
	}
	created := "2026-09-15T00:02:03.456"
	document, err := newAuditLogDocument(domain.AuditLog{Type: "LOGIN", CreatedAt: &created}, shanghai)
	if err != nil {
		t.Fatalf("构造文档失败: %v", err)
	}
	if got := document.CreatedAt.UTC().Format(time.RFC3339Nano); got != "2026-09-14T16:02:03.456Z" {
		t.Fatalf("时区换算异常: %s", got)
	}
	// 回读（同一时区）应还原出原文本。
	if back := document.toDomain(shanghai); domain.DerefString(back.CreatedAt) != created {
		t.Fatalf("回读文本异常: %q", domain.DerefString(back.CreatedAt))
	}
}

// TestAuditLogFilterDerivedQueries 校验四个派生查询的过滤条件与优先级。
func TestAuditLogFilterDerivedQueries(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		query domain.AuditLogQuery
		want  bson.D
	}{
		{
			name:  "type 优先于 userType",
			query: domain.AuditLogQuery{Type: "CREATE", UserType: "WORKER", UserID: "u1"},
			want:  bson.D{{Key: "type", Value: "CREATE"}},
		},
		{
			name:  "userType 过滤",
			query: domain.AuditLogQuery{UserType: "WORKER", UserID: "u1"},
			want:  bson.D{{Key: "userType", Value: "WORKER"}},
		},
		{
			name:  "userId 过滤",
			query: domain.AuditLogQuery{UserID: "u1"},
			want:  bson.D{{Key: "userId", Value: "u1"}},
		},
		{
			name:  "时间区间（Between 闭区间）",
			query: domain.AuditLogQuery{CreatedFrom: &from, CreatedTo: &to},
			want: bson.D{{Key: "createdAt", Value: bson.D{
				{Key: "$gte", Value: from},
				{Key: "$lte", Value: to},
			}}},
		},
		{
			name:  "仅下界",
			query: domain.AuditLogQuery{CreatedFrom: &from},
			want:  bson.D{{Key: "createdAt", Value: bson.D{{Key: "$gte", Value: from}}}},
		},
		{
			name:  "仅上界",
			query: domain.AuditLogQuery{CreatedTo: &to},
			want:  bson.D{{Key: "createdAt", Value: bson.D{{Key: "$lte", Value: to}}}},
		},
		{
			name:  "无过滤（findAll）",
			query: domain.AuditLogQuery{},
			want:  bson.D{},
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := auditLogFilter(item.query); !bsonEqual(got, item.want) {
				t.Fatalf("过滤条件异常:\n got=%v\nwant=%v", got, item.want)
			}
		})
	}
}

// TestAuditLogPageWindowMatchesSpringPageRequest 校验分页窗口与 PageRequest 语义一致。
func TestAuditLogPageWindowMatchesSpringPageRequest(t *testing.T) {
	skip, limit, err := auditLogPageWindow(domain.AuditLogQuery{PageIndex: 3, PageSize: 20})
	if err != nil || skip != 40 || limit != 20 {
		t.Fatalf("第 3 页窗口异常: skip=%d limit=%d err=%v", skip, limit, err)
	}
	skip, limit, err = auditLogPageWindow(domain.AuditLogQuery{PageIndex: 0, PageSize: 20})
	if err != nil || skip != 0 || limit != 20 {
		t.Fatalf("pageIndex=0 应收敛为第 1 页: skip=%d limit=%d err=%v", skip, limit, err)
	}
	if _, _, err := auditLogPageWindow(domain.AuditLogQuery{PageIndex: 1, PageSize: 0}); err == nil {
		t.Fatal("pageSize=0 应报错")
	}
}

// TestAuditLogIDRoundTrip 校验 _id 的两种形态。
func TestAuditLogIDRoundTrip(t *testing.T) {
	generated, ok := auditLogIDValue("").(bson.ObjectID)
	if !ok {
		t.Fatal("空 id 应生成 ObjectId")
	}
	if auditLogIDString(generated) != generated.Hex() {
		t.Fatalf("ObjectId 应输出十六进制串: %v", generated)
	}
	hex := "6aa6f9057bf73c1f6b662f39"
	if value, ok := auditLogIDValue(hex).(bson.ObjectID); !ok || value.Hex() != hex {
		t.Fatalf("24 位十六进制串应按 ObjectId 写入: %T", auditLogIDValue(hex))
	}
	if value := auditLogIDValue("legacy-id"); value != "legacy-id" {
		t.Fatalf("非 ObjectId 字符串应原样写入: %v", value)
	}
	if got := auditLogIDString(nil); got != "" {
		t.Fatalf("nil _id 应返回空串: %q", got)
	}
}

// bsonEqual 比较两个 BSON 文档/值（bson.D 的 DeepEqual 对 time.Time 的单调时钟敏感）。
func bsonEqual(left, right any) bool {
	leftRaw, leftErr := bson.Marshal(bson.D{{Key: "v", Value: left}})
	rightRaw, rightErr := bson.Marshal(bson.D{{Key: "v", Value: right}})
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(leftRaw) == string(rightRaw)
}
