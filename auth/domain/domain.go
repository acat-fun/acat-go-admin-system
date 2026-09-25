// Package domain 定义 admin-user 服务的领域模型与对外契约结构。
//
// 与 侧的对应关系：
//   - Worker    <- fun.acat.admin.entity.AdminWorkerEntity
//   - Page      <- fun.acat.admin.entity.AdminPageEntity
//   - Role      <- fun.acat.admin.entity.AdminRoleEntity
//   - FrontendModule <- fun.acat.admin.entity.AdminFrontendModuleEntity
//
// JSON 字段名为驼峰，ID 一律保持字符串。
package domain

import "time"

// RootRoleID 是超级管理员角色的 id 与角色码（t_acat_role.id = code = "root"）。
//
// 超管判定统一口径：用户绑定 id 为 "root" 的角色即超管（会话 roles 快照含 root），
// 不再按登录 id 特判。
const RootRoleID = "root"

// RootLoginID 是历史口径下超级管理员固定登录 id（worker 表 id="0"）。
//
// Deprecated: 超管判定已统一为角色判定；本常量仅供迁移期兼容引用，新代码不要使用。
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

// ShellModuleCode 是壳应用模块代码；该模块的页面不受前端模块启用状态过滤。
const ShellModuleCode = "shell"

// Worker 对应 t_acat_user_worker。
type Worker struct {
	ID       string
	Username string
	// Password 是 BCrypt 哈希（$2a$/$2b$/$2y$）。
	Password string
	Email    string
	Avatar   string
	// Status 0=禁用 1=正常。
	Status int
}

// Page 对应 t_acat_page（含 i18n 名称回退与审计时间）。
type Page struct {
	ID                 string
	Code               string
	Name               string
	Type               int
	Path               string
	Icon               string
	ParentID           string
	SortOrder          int
	Scope              int
	IsEnabled          int
	FrontendModuleCode string
	RouteKey           string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Role 对应 t_acat_role（含管理接口需要的描述与时间字段）。
type Role struct {
	ID          string
	Code        string
	Name        string
	Description *string
	Status      int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// FrontendModule 对应 t_acat_frontend_module。
//
// FallbackManifestPath 对应可空列 fallback_manifest_path（NULL → nil），
// 保证描述符里的 fallbackManifestUrl 与 一样输出 null 而不是 ""。
type FrontendModule struct {
	ModuleCode           string
	Name                 string
	ReleaseVersion       string
	ContractVersion      int
	ManifestPath         string
	FallbackVersion      string
	FallbackManifestPath *string
	Status               int
	SortOrder            int
}
