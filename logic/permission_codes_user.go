package logic

// 本文件是用户/角色/权限域的权限码常量（抽取自 acat-admin-user；
// SystemPermissions / MessageForbidden / RoleCodeRoot 在 permission_codes.go 统一声明）。

const (
	// ---- 系统管理 ----
	SystemUsers                       = "acat:admin:system:users"
	SystemPermissionsCreateRole       = "acat:admin:system:permissions:create-role"
	SystemPermissionsEditRole         = "acat:admin:system:permissions:edit-role"
	SystemPermissionsDeleteRole       = "acat:admin:system:permissions:delete-role"
	SystemPermissionsToggleRole       = "acat:admin:system:permissions:toggle-role"
	SystemPermissionsCreate           = "acat:admin:system:permissions:create"
	SystemPermissionsEdit             = "acat:admin:system:permissions:edit"
	SystemPermissionsDelete           = "acat:admin:system:permissions:delete"
	SystemPermissionsAssignPermission = "acat:admin:system:permissions:assign-permission"
	SystemUsersWorkers                = "acat:admin:system:users:workers"
	SystemUsersWorkersAdd             = "acat:admin:system:users:workers:add"
	SystemUsersWorkersEdit            = "acat:admin:system:users:workers:edit"
	SystemUsersWorkersDelete          = "acat:admin:system:users:workers:delete"
	SystemUsersWorkersToggle          = "acat:admin:system:users:workers:toggle"
	SystemUsersWorkersAssignRole      = "acat:admin:system:users:workers:assign-role"
	SystemUsersReaders                = "acat:read:admin:system:users:readers"
	SystemUsersReadersAdd             = "acat:read:admin:system:users:readers:add"
	SystemUsersReadersEdit            = "acat:read:admin:system:users:readers:edit"
	SystemUsersReadersDelete          = "acat:read:admin:system:users:readers:delete"
	SystemUsersReadersToggle          = "acat:read:admin:system:users:readers:toggle"
	SystemUsersReadersAssignRole      = "acat:read:admin:system:users:readers:assign-role"
	// RoleCodeAdmin admin 角色编码。
	RoleCodeAdmin = "admin"
)
