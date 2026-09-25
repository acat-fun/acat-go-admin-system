// Package domain 定义 admin-system 服务的领域模型与对外契约结构。
//
// 常量、实体与 VO 的分工：
//   - AdminDictEntity / AdminDictDataEntity：字典与字典数据项契约结构；
//   - I18nTypeEntity：语言类型契约结构；
//   - AdminPageEntity / AdminPageHistoryEntity：页面与页面变更快照契约结构；
//   - AdminFrontendModuleEntity：前端模块契约结构；
//   - AdminFileEntity：文件契约结构。
//
// JSON 字段名为驼峰，ID 一律保持字符串。
package domain

import (
	"strings"
	"time"
)

// RootRoleID 是超级管理员角色的 id 与角色码（t_acat_role.id = code = "root"）。
//
// 超管判定统一口径：用户绑定 id 为 "root" 的角色即超管（会话 roles 快照含 root），
// 不再按登录 id 特判。
const RootRoleID = "root"

// RootLoginID 是超级管理员固定登录 id（worker 表 id="0"），
// 供兼容期代码与测试引用；超管判定请使用 RootRoleID 角色判定。
const RootLoginID = "0"

// IsRootRole 判断角色码列表是否包含超级管理员角色。
func IsRootRole(roleCodes []string) bool {
	for _, code := range roleCodes {
		if code == RootRoleID {
			return true
		}
	}
	return false
}

// DefaultI18nCode 是缺省语言码。
const DefaultI18nCode = "zh-CN"

// ShellModuleCode 是壳应用模块代码；该模块的页面不受前端模块启用状态过滤。
const ShellModuleCode = "shell"

// DateTimeLayout 是时间字段的 JSON 文本格式（无时区偏移）。
const DateTimeLayout = "2006-01-02T15:04:05"

// DateLayout 是日期文本格式。
const DateLayout = "2006-01-02"

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

// FormatDateTimePtr 把可空时间转换为 *string（零值 → nil。
func FormatDateTimePtr(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	formatted := value.Format(DateTimeLayout)
	return &formatted
}

// StringOrNil 把空串转为 nil（区分 null 与空串两种语义）。
func StringOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// DerefString 安全解引用字符串指针，nil 返回空串。
func DerefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// TrimOrEmpty 去除首尾空白，nil 安全。
func TrimOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
