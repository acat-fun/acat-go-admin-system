package domain

import (
	"strings"
	"time"
)

// DateTimeLayout 是时间字段的 JSON 文本格式（无时区偏移）。
const DateTimeLayout = "2006-01-02T15:04:05"

// DefaultI18nCode 是缺省语言码。
const DefaultI18nCode = "zh-CN"

// Now 返回当前时间并截断到秒。
//
// MySQL DATETIME(0) 列会对小数秒四舍五入，若直接写入带纳秒的 time.Now()，
// 会出现“响应里的 createdAt 比库里少 1 秒”的偏差；统一截断到秒后两者一致。
func Now() time.Time {
	return time.Now().Truncate(time.Second)
}

// FormatDateTime 把数据库时间转换为 JSON 文本（秒精度，无时区偏移）。
func FormatDateTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(DateTimeLayout)
}

// ReaderUser 对应 t_acat_user（APP/读者用户，acat-admin 侧独立管理）。
type ReaderUser struct {
	ID       string
	Username string
	// Password 是 MD5 十六进制小写。
	Password string
	Email    *string
	Avatar   *string
	Status   int
	Muted    int
	AgeLevel int
	// CreatedAt/UpdatedAt 由 myInsert/myUpdate 填充。
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AdminWorker 对应 t_acat_user_worker 的“管理视图”：
// 相比认证用的 Worker 多了时间字段与可空列（email/avatar）。
type AdminWorker struct {
	ID       string
	Username string
	// Password 是 BCrypt 哈希（$2a$/$2b$/$2y$）。
	Password  string
	Email     *string
	Avatar    *string
	Status    int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AdminPermission 对应 t_acat_permission（纯权限码注册表）。
type AdminPermission struct {
	ID        string
	Code      string
	Name      string
	PageID    *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ReaderCreateDTO。
type ReaderCreateDTO struct {
	Username string  `json:"username"`
	Password string  `json:"password"`
	Email    *string `json:"email"`
	// AgeLevel 年龄分级（8/12/16/18/19 等）；为空时按数据库默认 16。
	AgeLevel *int `json:"ageLevel"`
}

// ReaderUpdateDTO。
type ReaderUpdateDTO struct {
	Username *string `json:"username"`
	Email    *string `json:"email"`
	Status   *int    `json:"status"`
	AgeLevel *int    `json:"ageLevel"`
}

// WorkerCreateDTO。
type WorkerCreateDTO struct {
	Username string   `json:"username"`
	Password string   `json:"password"`
	Email    *string  `json:"email"`
	RoleIDs  []string `json:"roleIds"`
}

// WorkerUpdateDTO。
//
// 注意：RoleIDs 为 nil 表示“不改角色”，为空数组表示“清空角色”。
type WorkerUpdateDTO struct {
	Username *string  `json:"username"`
	Email    *string  `json:"email"`
	Password *string  `json:"password"`
	Status   *int     `json:"status"`
	RoleIDs  []string `json:"roleIds"`
}

// RoleAssignParam。
type RoleAssignParam struct {
	RoleIDs []string `json:"roleIds"`
}

// PermissionAssignParam。
type PermissionAssignParam struct {
	PermissionIDs []string `json:"permissionIds"`
}

// StatusParam。
type StatusParam struct {
	Status *int `json:"status"`
}

// RoleSaveDTO 对应直接以 Entity 作为请求体的角色新增/修改载荷
// 。
//
// 仅取业务字段：id/status/isDeleted 等由服务端决定（status 固定 1、id 由 UUID v7 生成）。
type RoleSaveDTO struct {
	Code        *string     `json:"code"`
	Name        *string     `json:"name"`
	Description *string     `json:"description"`
	Status      *int        `json:"status"`
	I18nValue   []I18nValue `json:"i18nValue"`
}

// PermissionSaveDTO 对应直接以 Entity 作为请求体的权限新增/修改载荷
// 。
type PermissionSaveDTO struct {
	Code      *string     `json:"code"`
	Name      *string     `json:"name"`
	PageID    *string     `json:"pageId"`
	I18nValue []I18nValue `json:"i18nValue"`
}

// ResolveDefaultName。
// i18nValue 中存在 zh-CN 且值非空白时取该值，否则回退 name。
func ResolveDefaultName(fallback *string, values []I18nValue) *string {
	for _, value := range values {
		if value.I18n != DefaultI18nCode {
			continue
		}
		if strings.TrimSpace(value.Value) == "" {
			continue
		}
		resolved := value.Value
		return &resolved
	}
	return fallback
}
