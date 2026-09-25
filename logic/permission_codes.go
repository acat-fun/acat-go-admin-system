// Package logic 提供管理端权限判定与权限码常量。
//
// Go 侧当前没有 URL 级权限拦截器，
// 因此由 httpapi 显式调用本包的 Checker/Actor 完成同样的判定：
// root（loginID=="0"）直接放行，其余读会话 permissions 快照，不通过返回 403。
//
// 与 admin-user 的 internal/logic 完全同构（迁移期两服务各自持有副本，
// 待公共库提供通用的管理端权限判定后再合并）。
package logic

// 权限码常量注册表。
//
// 本服务只登记 admin-system 域实际用到的常量（侧常量类是全平台共用的，
// Go 侧按服务裁剪，避免出现本服务无法判定的悬挂常量）。
//
// 新增权限的完整流程：1) 在此登记常量；2) 在 deploy/sql 的 seed 中补对应
// 权限码（页面码入 t_acat_page、按钮码入 t_acat_permission）；3) 前端 permissionRegistry.ts 登记。
const (
	// ---- 字典 ----
	SystemDicts       = "acat:admin:system:dicts"        // GET /dicts、GET /dicts/{id}/data
	SystemDictsCreate = "acat:admin:system:dicts:create" // POST /dicts、POST /dicts/{id}/data
	SystemDictsEdit   = "acat:admin:system:dicts:edit"   // PUT /dicts/{id} 等
	SystemDictsDelete = "acat:admin:system:dicts:delete" // DELETE /dicts/{id} 等

	// ---- 国际化语言类型 ----
	SystemI18nTypes       = "acat:admin:system:i18n-types"        // 页面码（本服务仅登录校验，无注解）
	SystemI18nTypesCreate = "acat:admin:system:i18n-types:create" // POST /i18n/types
	SystemI18nTypesEdit   = "acat:admin:system:i18n-types:edit"   // PUT /i18n/types/{id}
	SystemI18nTypesDelete = "acat:admin:system:i18n-types:delete" // DELETE /i18n/types/{id}

	// ---- 页面 ----
	SystemPages       = "acat:admin:system:pages"        // GET /pages（与 permissions 取 OR）
	SystemPagesCreate = "acat:admin:system:pages:create" // POST /pages
	SystemPagesEdit   = "acat:admin:system:pages:edit"   // PUT /pages/{id}
	SystemPagesDelete = "acat:admin:system:pages:delete" // DELETE /pages/{id}

	// ---- 权限（pages 列表的 OR 分支）----
	SystemPermissions = "acat:admin:system:permissions"

	// ---- 前端模块 ----
	SystemFrontendModules        = "acat:admin:system:frontend-modules"         // 类级：GET /frontend-modules
	SystemFrontendModulesCreate  = "acat:admin:system:frontend-modules:create"  // POST
	SystemFrontendModulesEdit    = "acat:admin:system:frontend-modules:edit"    // PUT /{id}
	SystemFrontendModulesPublish = "acat:admin:system:frontend-modules:publish" // PUT /{id}/publication

	// ---- 审计日志（类级）----
	SystemAuditLogs = "acat:admin:system:audit-logs"

	// ---- 文件（类级 + 方法级）----
	SystemFiles       = "acat:admin:system:files"
	SystemFilesUpload = "acat:admin:system:files:upload"
	SystemFilesDelete = "acat:admin:system:files:delete"
)

// 与
const (
	// MessageForbidden 无权限统一提示（Sa-Token NotPermissionException 的 Go 口径）。
	MessageForbidden = "无操作权限"
	// RoleCodeRoot root 角色编码。
	RoleCodeRoot = "root"
)
