package domain

// 本文件定义管理接口的对外视图对象（VO）与实体响应结构。
//
// JSON 字段名：可空列用指针承载，Go 的 nil 序列化为 null。

// RoleSimpleVO 是角色简版视图（仅 code 有值）。
//
// id 与 name 恒为 null，只有 code 有值。
type RoleSimpleVO struct {
	ID   *string `json:"id"`
	Code string  `json:"code"`
	Name *string `json:"name"`
}

// UserVO。
type UserVO struct {
	ID        string         `json:"id"`
	Username  string         `json:"username"`
	Email     *string        `json:"email"`
	Avatar    *string        `json:"avatar"`
	Status    int            `json:"status"`
	Muted     int            `json:"muted"`
	AgeLevel  int            `json:"ageLevel"`
	Roles     []RoleSimpleVO `json:"roles"`
	CreatedAt string         `json:"createdAt"`
}

// WorkerRoleVO。
type WorkerRoleVO struct {
	ID              string  `json:"id"`
	Code            string  `json:"code"`
	Name            string  `json:"name"`
	Description     *string `json:"description"`
	Status          int     `json:"status"`
	PermissionCount *int    `json:"permissionCount"`
}

// WorkerVO。
type WorkerVO struct {
	ID        string         `json:"id"`
	Username  string         `json:"username"`
	Email     *string        `json:"email"`
	Avatar    *string        `json:"avatar"`
	Status    int            `json:"status"`
	Roles     []WorkerRoleVO `json:"roles"`
	CreatedAt string         `json:"createdAt"`
	UpdatedAt string         `json:"updatedAt"`
}

// AdminRoleEntity。
// （GET /roles?all=true 直接返回实体列表）。
//
// 只保留业务与契约字段：BaseEntity 的 createBy/updateBy/version 不参与前端契约。
type AdminRoleEntity struct {
	ID          string  `json:"id"`
	IsDeleted   int     `json:"isDeleted"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Status      int     `json:"status"`
}

// AdminPermissionEntity。
type AdminPermissionEntity struct {
	ID        string  `json:"id"`
	IsDeleted int     `json:"isDeleted"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	PageID    *string `json:"pageId"`
}

// PermissionVO。
// （扁平结构。
type PermissionVO struct {
	ID     string  `json:"id"`
	Code   string  `json:"code"`
	Name   string  `json:"name"`
	PageID *string `json:"pageId"`
}

// SelectVO。
type SelectVO struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
