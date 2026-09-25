package domain

import (
	"fmt"
	"strings"
	"time"
)

// 本文件对应 侧审计日志域（MongoDB）：
//   - AuditLogEntity (fun.acat.admin.system.entity.AuditLogEntity, collection audit_logs)
//   - AuditLogTypes  (fun.acat.admin.system.constants.AuditLogTypes)
//
// 真实存储实现是 repo.MongoAuditLogStore（MongoDB 集合 audit_logs）；
// repo.MemoryAuditLogStore 仅作为单元测试替身保留。

// AuditLogCollection 是 Mongo 集合名。
const AuditLogCollection = "audit_logs"

// 日志类型常量。
const (
	AuditLogTypeLogin     = "LOGIN"
	AuditLogTypeCreate    = "CREATE"
	AuditLogTypeUpdate    = "UPDATE"
	AuditLogTypePatch     = "PATCH"
	AuditLogTypeDelete    = "DELETE"
	AuditLogTypeOperation = "OPERATION"
)

// 审计日志身份/文案口径常量。
const (
	// AuditLogAnonymousUsername 未登录时的用户名。
	AuditLogAnonymousUsername = "anonymous"
	// AuditLogUserTypeWorker 管理端路径（含 "/admin/"）的用户类型。
	AuditLogUserTypeWorker = "WORKER"
	// AuditLogUserTypeApp 其余路径的默认用户类型。
	AuditLogUserTypeApp = "APP"
	// AuditLogAnonymousUserID 未登录时写入的 userId。
	AuditLogAnonymousUserID = "0"
	// AuditLogLoginRequestURI 登录日志固定写入的 requestUri。
	AuditLogLoginRequestURI = "/api/auth/login"
	// AuditLogLoginRequestMethod 登录日志固定写入的 requestMethod。
	AuditLogLoginRequestMethod = "POST"
	// AuditLogLoginActionSuffix 登录日志 action 后缀。
	AuditLogLoginActionSuffix = " 登录系统"
	// AuditLogRequestParamsMaxLength 请求参数摘要最大长度。
	AuditLogRequestParamsMaxLength = 2000
	// AuditLogRequestParamsTruncatedSuffix 超长截断后缀。
	AuditLogRequestParamsTruncatedSuffix = "..."
)

// AuditLogTypeFromHTTPMethod。
//
// PATCH→PATCH、DELETE→DELETE，其余（含 GET/未知方法）→ OPERATION。
func AuditLogTypeFromHTTPMethod(method string) string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "POST":
		return AuditLogTypeCreate
	case "PUT":
		return AuditLogTypeUpdate
	case "PATCH":
		return AuditLogTypePatch
	case "DELETE":
		return AuditLogTypeDelete
	default:
		return AuditLogTypeOperation
	}
}

// AuditLogTimeLayout 是审计日志时间文本格式：
// ISO-8601 口径，带毫秒时补 ".SSS"（BSON Date 精度为毫秒，见 AuditLogEntity.createdAt）。
const AuditLogTimeLayout = "2006-01-02T15:04:05.000"

// FormatAuditDateTime 把存储时间（BSON Date，UTC 瞬时）转换为 时间文本。
//
// DateToLocalDateTimeConverter 用 `ZoneId.systemDefault()` 做转换（无时区偏移输出），
// Jackson 再按 `ISO_LOCAL_DATE_TIME` 序列化：小数秒**去掉尾随零**、为 0 时整段省略。
// BSON Date 只有毫秒精度，因此实际形态是「无小数」或 1~3 位小数，例如：
//
//	690ms → 2026-09-14T23:22:50.69（不是 .690）
//	862ms → 2026-09-13T19:27:01.862
//	100ms → 2026-09-14T01:02:03.1
//	  0ms → 2026-09-14T01:02:03
func FormatAuditDateTime(value time.Time, loc *time.Location) string {
	if value.IsZero() {
		return ""
	}
	if loc == nil {
		loc = time.UTC
	}
	local := value.In(loc)
	text := local.Format(DateTimeLayout)
	if millis := local.Nanosecond() / int(time.Millisecond); millis != 0 {
		// ISO_LOCAL_DATE_TIME 的 appendFraction(minWidth=0)：去掉尾随零。
		text = fmt.Sprintf("%s.%s", text, strings.TrimRight(fmt.Sprintf("%03d", millis), "0"))
	}
	return text
}

// ParseAuditDateTime 解析 时间文本为瞬时（AuditLogStore 写入用）。
//
// 支持带毫秒（`2026-09-14T01:02:03.123`）与不带毫秒两种形态；loc 为 nil 时按 UTC。
func ParseAuditDateTime(value string, loc *time.Location) (time.Time, bool) {
	text := strings.TrimSpace(value)
	if text == "" {
		return time.Time{}, false
	}
	if loc == nil {
		loc = time.UTC
	}
	if parsed, err := time.ParseInLocation(AuditLogTimeLayout, text, loc); err == nil {
		return parsed, true
	}
	if parsed, err := time.ParseInLocation(DateTimeLayout, text, loc); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

// AuditLog。
//
// JSON 字段顺序与结构体声明顺序一致。
type AuditLog struct {
	ID            string  `json:"id"`
	Type          string  `json:"type"`
	UserID        *string `json:"userId"`
	Username      *string `json:"username"`
	UserType      *string `json:"userType"`
	Action        *string `json:"action"`
	Detail        *string `json:"detail"`
	IP            *string `json:"ip"`
	UserAgent     *string `json:"userAgent"`
	RequestURI    *string `json:"requestUri"`
	RequestMethod *string `json:"requestMethod"`
	RequestParams *string `json:"requestParams"`
	CreateBy      *string `json:"createBy"`
	UpdateBy      *string `json:"updateBy"`
	CreatedAt     *string `json:"createdAt"`
	UpdatedAt     *string `json:"updatedAt"`
}

// AuditLogQuery 是审计日志列表查询条件。
//
// 过滤分支与 AuditLogRepository 的派生查询一一对应（AuditLogRepository.java:17-23），
// 且保持 AuditLogAdminServiceImpl.list 的「if/else if」互斥优先级（AuditLogAdminServiceImpl.java:70-73）：
// Type → UserType → UserID → 时间区间 → 全量。
// 后两个分支是仓储等价能力（供清理预览/测试使用），不改变 list 的可观测行为。
type AuditLogQuery struct {
	// Type 非空时按 type 精确过滤（findByTypeOrderByCreatedAtDesc，优先级最高）。
	Type string
	// UserType 非空且 Type 为空时按 userType 精确过滤（findByUserTypeOrderByCreatedAtDesc）。
	UserType string
	// UserID 非空且前面条件为空时按 userId 精确过滤（findByUserIdOrderByCreatedAtDesc）。
	UserID string
	// CreatedFrom / CreatedTo 组成时间区间（findByCreatedAtBetweenOrderByCreatedAtDesc，
	// Spring Data Between 为闭区间：$gte 且 $lte）。
	// 只填其一时退化为 $gte / $lte 单边过滤（对应 GreaterThanEqual / LessThanEqual 派生查询）。
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	// PageIndex 1 基页码。
	PageIndex int
	// PageSize 每页条数，必须 > 0。
	PageSize int
}

// AuditLogPage 是审计日志分页结果。
type AuditLogPage struct {
	Total int64
	List  []AuditLog
}
