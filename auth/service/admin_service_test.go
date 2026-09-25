package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
	"github.com/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-common/apperr"
)

func businessMessage(t *testing.T, err error) string {
	t.Helper()
	business, ok := apperr.IsBusiness(err)
	if !ok {
		t.Fatalf("应为业务失败，实际 %v", err)
	}
	return business.Message
}

func seedRole(f *adminFixture, id, code, name string, status int) *domain.Role {
	role := &domain.Role{ID: id, Code: code, Name: name, Status: status, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	f.roles.roles[id] = role
	return role
}

// ---- 读者用户 ----

func TestCreateReaderUsesMD5AndRejectsDuplicateUsername(t *testing.T) {
	f := newAdminFixture(t, "reader-1")
	ctx := context.Background()

	vo, err := f.svc.CreateReader(ctx, domain.ReaderCreateDTO{
		Username: "reader", Password: "secret", Email: ptr("reader@acat.com"),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if vo.ID != "reader-1" || vo.Status != 1 || vo.Username != "reader" {
		t.Errorf("UserVO = %+v", vo)
	}
	if vo.Roles == nil || len(vo.Roles) != 0 {
		t.Errorf("新建用户 roles 必须为空数组: %+v", vo.Roles)
	}
	sum := md5.Sum([]byte("secret"))
	if f.readers.users["reader-1"].Password != hex.EncodeToString(sum[:]) {
		t.Errorf("密码必须为 MD5 十六进制小写，实际 %q", f.readers.users["reader-1"].Password)
	}

	_, err = f.svc.CreateReader(ctx, domain.ReaderCreateDTO{Username: "reader", Password: "other"})
	if got := businessMessage(t, err); got != MessageUsernameExists {
		t.Errorf("重复用户名提示 = %q", got)
	}
}

func TestCreateReaderAgeLevelDefaultsToSixteen(t *testing.T) {
	f := newAdminFixture(t, "reader-1")
	if _, err := f.svc.CreateReader(context.Background(), domain.ReaderCreateDTO{Username: "r", Password: "p"}); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if f.readers.users["reader-1"].AgeLevel != 16 {
		t.Errorf("isAdult 默认应为 0")
	}

	f2 := newAdminFixture(t, "reader-2")
	if _, err := f2.svc.CreateReader(context.Background(), domain.ReaderCreateDTO{Username: "r", Password: "p", AgeLevel: ptrInt(18)}); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if f2.readers.users["reader-2"].AgeLevel != 18 {
		t.Errorf("isAdult 应写入 1")
	}
}

func TestUpdateReaderOnlyAppliesNonNullFields(t *testing.T) {
	f := newAdminFixture(t)
	email := "old@acat.com"
	f.readers.users["u1"] = &domain.ReaderUser{ID: "u1", Username: "old", Email: &email,
		Status: 1, Muted: 1, AgeLevel: 18, CreatedAt: time.Now(), UpdatedAt: time.Now()}

	vo, err := f.svc.UpdateReader(context.Background(), "u1", domain.ReaderUpdateDTO{Username: ptr("new")})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if vo.Username != "new" || vo.Email == nil || *vo.Email != "old@acat.com" {
		t.Errorf("仅应更新 username: %+v", vo)
	}
	if vo.Status != 1 || vo.Muted != 1 || vo.AgeLevel != 18 {
		t.Errorf("未传字段不应被覆盖: %+v", vo)
	}

	_, err = f.svc.UpdateReader(context.Background(), "missing", domain.ReaderUpdateDTO{})
	if got := businessMessage(t, err); got != MessageUserNotFound {
		t.Errorf("不存在用户提示 = %q", got)
	}
}

func TestUpdateReaderStatusAndMute(t *testing.T) {
	f := newAdminFixture(t)
	f.readers.users["u1"] = &domain.ReaderUser{ID: "u1", Username: "reader", Status: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}

	if err := f.svc.UpdateReaderStatus(context.Background(), "u1", 0); err != nil {
		t.Fatalf("改状态失败: %v", err)
	}
	if f.readers.users["u1"].Status != 0 {
		t.Errorf("status = %d", f.readers.users["u1"].Status)
	}
	if err := f.svc.UpdateReaderMute(context.Background(), "u1", 1); err != nil {
		t.Fatalf("改禁言失败: %v", err)
	}
	if f.readers.users["u1"].Muted != 1 {
		t.Errorf("muted = %d", f.readers.users["u1"].Muted)
	}
	if err := f.svc.UpdateReaderStatus(context.Background(), "missing", 1); businessMessage(t, err) != MessageUserNotFound {
		t.Errorf("不存在用户应业务失败")
	}
}

func TestDeleteReaderSoftDeletes(t *testing.T) {
	f := newAdminFixture(t)
	if err := f.svc.DeleteReader(context.Background(), "u1"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if len(f.readers.softDeleted) != 1 || f.readers.softDeleted[0] != "u1" {
		t.Errorf("软删调用 = %v", f.readers.softDeleted)
	}
}

func TestAssignReaderRolesKeepsInputOrderAndDropsUnknown(t *testing.T) {
	f := newAdminFixture(t)
	seedRole(f, "r1", "editor", "编辑", 1)
	seedRole(f, "r2", "viewer", "访客", 1)

	if err := f.svc.AssignReaderRoles(context.Background(), "u1", []string{"r2", "ghost", "r1"}); err != nil {
		t.Fatalf("分配角色失败: %v", err)
	}
	if len(f.readers.roleReset) != 1 || f.readers.roleReset[0] != "u1" {
		t.Errorf("必须先软删全部角色关联: %v", f.readers.roleReset)
	}
	got := f.readers.roleInserted["u1"]
	if len(got) != 2 || got[0] != "r2" || got[1] != "r1" {
		t.Errorf("有效角色 = %v, 期望保持输入顺序并丢弃不存在 id", got)
	}

	// 空列表只软删，不插入。
	f.readers.roleInserted = map[string][]string{}
	if err := f.svc.AssignReaderRoles(context.Background(), "u2", nil); err != nil {
		t.Fatalf("清空角色失败: %v", err)
	}
	if len(f.readers.roleInserted) != 0 {
		t.Errorf("空角色列表不应插入: %v", f.readers.roleInserted)
	}
}

func TestListReadersBuildsPageDataAndRoleVO(t *testing.T) {
	f := newAdminFixture(t)
	f.readers.users["u1"] = &domain.ReaderUser{ID: "u1", Username: "reader", Status: 1, AgeLevel: 18, CreatedAt: time.Date(2026, 9, 14, 1, 2, 3, 0, time.Local)}
	f.readers.roles["u1"] = []string{"editor", "viewer"}

	page, err := f.svc.ListReaders(context.Background(), 1, 10, "read")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if f.readers.lastKeyword != "read" {
		t.Errorf("keyword 未透传: %q", f.readers.lastKeyword)
	}
	if page.Total != 1 || page.PageIndex != 1 || page.PageSize != 10 || len(page.List) != 1 {
		t.Fatalf("PageData = %+v", page)
	}
	vo := page.List[0]
	if vo.CreatedAt != "2026-09-14T01:02:03" {
		t.Errorf("createdAt = %q", vo.CreatedAt)
	}
	if len(vo.Roles) != 2 {
		t.Fatalf("roles = %+v", vo.Roles)
	}
	if vo.Roles[0].Code != "editor" || vo.Roles[0].ID != nil || vo.Roles[0].Name != nil {
		t.Errorf("RoleSimpleVO = %+v", vo.Roles[0])
	}
}

func TestListReadersNormalizesPage(t *testing.T) {
	f := newAdminFixture(t)
	page, err := f.svc.ListReaders(context.Background(), 0, 0, "")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.PageIndex != 1 || page.PageSize != 10 {
		t.Errorf("分页归一化 = %d/%d", page.PageIndex, page.PageSize)
	}
}

// ---- 工作人员 ----

func TestCreateWorkerRequiresAddPermissionAndHashesBCrypt(t *testing.T) {
	f := newAdminFixture(t, "w1")
	seedRole(f, "r1", "editor", "编辑", 1)
	ctx := context.Background()

	// 非 root 且有 add 权限：允许创建。
	actor := actorWith("u9", logic.SystemUsersWorkersAdd)
	vo, err := f.svc.CreateWorker(ctx, actor, domain.WorkerCreateDTO{
		Username: "worker", Password: "pw", Email: ptr("w@acat.fun"), RoleIDs: []string{"r1"},
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if vo.ID != "w1" || vo.Status != 1 || vo.CreatedAt == "" || vo.UpdatedAt == "" {
		t.Errorf("WorkerVO = %+v", vo)
	}
	if len(vo.Roles) != 1 || vo.Roles[0].Code != "editor" || vo.Roles[0].PermissionCount != nil {
		t.Errorf("roles = %+v", vo.Roles)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(f.workers.workers["w1"].Password), []byte("pw")); err != nil {
		t.Errorf("密码必须为 BCrypt 哈希: %v", err)
	}
	if got := f.workers.roleInserted["w1"]; len(got) != 1 || got[0] != "r1" {
		t.Errorf("创建后角色分配 = %v", got)
	}

	// 无 add 权限：403。
	_, err = f.svc.CreateWorker(ctx, actorWith("u9"), domain.WorkerCreateDTO{Username: "x", Password: "p"})
	forbidden, ok := apperr.As(err)
	if !ok || forbidden.HTTPStatus != apperr.StatusForbidden {
		t.Fatalf("缺少 workers:add 权限应 403，实际 %v", err)
	}

	// 重复用户名。
	_, err = f.svc.CreateWorker(ctx, actor, domain.WorkerCreateDTO{Username: "worker", Password: "p"})
	if got := businessMessage(t, err); got != MessageUsernameExists {
		t.Errorf("重复用户名提示 = %q", got)
	}
}

func TestCreateWorkerNonRootCannotAssignRootOrAdminRole(t *testing.T) {
	f := newAdminFixture(t, "w1")
	seedRole(f, "r0", logic.RoleCodeRoot, "超级管理员", 1)
	seedRole(f, "r1", logic.RoleCodeAdmin, "管理员", 1)

	_, err := f.svc.CreateWorker(context.Background(), actorWith("u9", logic.SystemUsersWorkersAdd),
		domain.WorkerCreateDTO{Username: "worker", Password: "pw", RoleIDs: []string{"r1"}})
	if got := businessMessage(t, err); got != "只有超级管理员才能分配 管理员 角色" {
		t.Errorf("提示 = %q", got)
	}

	// root 可以分配 admin 角色。
	if _, err := f.svc.CreateWorker(context.Background(), actorWith(domain.RootLoginID),
		domain.WorkerCreateDTO{Username: "worker", Password: "pw", RoleIDs: []string{"r1"}}); err != nil {
		t.Errorf("root 创建失败: %v", err)
	}
}

func TestUpdateWorkerSelfBypassAndRootProtection(t *testing.T) {
	f := newAdminFixture(t)
	now := time.Now()
	f.workers.workers["u1"] = &domain.AdminWorker{ID: "u1", Username: "self", Status: 1, CreatedAt: now, UpdatedAt: now}
	f.workers.workers["u2"] = &domain.AdminWorker{ID: "u2", Username: "other", Status: 1, CreatedAt: now, UpdatedAt: now}
	f.workers.workers[domain.RootLoginID] = &domain.AdminWorker{ID: domain.RootLoginID, Username: "root", Status: 1, CreatedAt: now, UpdatedAt: now}
	ctx := context.Background()

	// 自操作绕过：无任何权限也能改自己。
	if _, err := f.svc.UpdateWorker(ctx, actorWith("u1"), "u1", domain.WorkerUpdateDTO{Username: ptr("self2")}); err != nil {
		t.Fatalf("自操作应放行: %v", err)
	}
	if f.workers.workers["u1"].Username != "self2" {
		t.Errorf("username 未更新")
	}

	// 改他人且无 edit 权限：403。
	_, err := f.svc.UpdateWorker(ctx, actorWith("u1"), "u2", domain.WorkerUpdateDTO{Username: ptr("x")})
	if forbidden, ok := apperr.As(err); !ok || forbidden.HTTPStatus != apperr.StatusForbidden {
		t.Fatalf("操作他人应 403，实际 %v", err)
	}

	// 有 edit 权限但操作 root：不可编辑超级管理员。
	_, err = f.svc.UpdateWorker(ctx, actorWith("u9", logic.SystemUsersWorkersEdit), domain.RootLoginID,
		domain.WorkerUpdateDTO{Username: ptr("x")})
	if got := businessMessage(t, err); got != MessageWorkerRootEdit {
		t.Errorf("提示 = %q", got)
	}

	// root 自己可以编辑 root。
	if _, err := f.svc.UpdateWorker(ctx, actorWith(domain.RootLoginID), domain.RootLoginID,
		domain.WorkerUpdateDTO{Email: ptr("root@acat.fun")}); err != nil {
		t.Errorf("root 编辑自身失败: %v", err)
	}

	// 不存在的工作人员。
	_, err = f.svc.UpdateWorker(ctx, actorWith(domain.RootLoginID), "ghost", domain.WorkerUpdateDTO{})
	if got := businessMessage(t, err); got != MessageWorkerNotExist {
		t.Errorf("提示 = %q", got)
	}
}

func TestUpdateWorkerPasswordOnlyWhenNonEmpty(t *testing.T) {
	f := newAdminFixture(t)
	now := time.Now()
	hashed, _ := bcrypt.GenerateFromPassword([]byte("old"), bcrypt.MinCost)
	f.workers.workers["u1"] = &domain.AdminWorker{ID: "u1", Username: "self", Password: string(hashed), Status: 1, CreatedAt: now, UpdatedAt: now}
	ctx := context.Background()

	empty := ""
	if _, err := f.svc.UpdateWorker(ctx, actorWith("u1"), "u1", domain.WorkerUpdateDTO{Password: &empty}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if f.workers.workers["u1"].Password != string(hashed) {
		t.Errorf("空密码不应重置密码")
	}

	if _, err := f.svc.UpdateWorker(ctx, actorWith("u1"), "u1", domain.WorkerUpdateDTO{Password: ptr("new")}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(f.workers.workers["u1"].Password), []byte("new")); err != nil {
		t.Errorf("密码未更新: %v", err)
	}
}

func TestUpdateWorkerRoleIDsNilKeepsRolesEmptyClears(t *testing.T) {
	f := newAdminFixture(t)
	now := time.Now()
	f.workers.workers["u1"] = &domain.AdminWorker{ID: "u1", Username: "self", Status: 1, CreatedAt: now, UpdatedAt: now}
	seedRole(f, "r1", "editor", "编辑", 1)
	ctx := context.Background()

	// RoleIDs = nil：不动角色。
	if _, err := f.svc.UpdateWorker(ctx, actorWith("u1"), "u1", domain.WorkerUpdateDTO{Username: ptr("self2")}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if len(f.workers.roleReset) != 0 {
		t.Errorf("roleIds 为 nil 不应重置角色: %v", f.workers.roleReset)
	}

	// RoleIDs = []：清空角色。
	if _, err := f.svc.UpdateWorker(ctx, actorWith("u1"), "u1", domain.WorkerUpdateDTO{RoleIDs: []string{}}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if len(f.workers.roleReset) != 1 || f.workers.roleReset[0] != "u1" {
		t.Errorf("空 roleIds 应清空角色: %v", f.workers.roleReset)
	}
	if len(f.workers.roleInserted) != 0 {
		t.Errorf("空 roleIds 不应插入: %v", f.workers.roleInserted)
	}
}

func TestUpdateWorkerStatusAndDeleteProtectRoot(t *testing.T) {
	f := newAdminFixture(t)
	now := time.Now()
	f.workers.workers[domain.RootLoginID] = &domain.AdminWorker{ID: domain.RootLoginID, Username: "root", Status: 1, CreatedAt: now, UpdatedAt: now}
	f.workers.workers["u1"] = &domain.AdminWorker{ID: "u1", Username: "worker", Status: 1, CreatedAt: now, UpdatedAt: now}
	ctx := context.Background()

	if err := f.svc.UpdateWorkerStatus(ctx, actorWith(domain.RootLoginID), domain.RootLoginID, 0); businessMessage(t, err) != MessageWorkerRootDisable {
		t.Errorf("root 禁用提示 = %v", err)
	}
	if err := f.svc.DeleteWorker(ctx, actorWith(domain.RootLoginID), domain.RootLoginID); businessMessage(t, err) != MessageWorkerRootDelete {
		t.Errorf("root 删除提示 = %v", err)
	}

	// 非 root 有 edit/delete 权限但目标是 root：先判权限通过，再返回 root 保护提示。
	if err := f.svc.UpdateWorkerStatus(ctx, actorWith("u9", logic.SystemUsersWorkersEdit), domain.RootLoginID, 0); businessMessage(t, err) != MessageWorkerRootDisable {
		t.Errorf("非 root 禁用 root 提示 = %v", err)
	}
	// 无权限直接 403。
	if err := f.svc.DeleteWorker(ctx, actorWith("u9"), "u1"); apperr.HTTPStatusOf(err) != apperr.StatusForbidden {
		t.Errorf("无 delete 权限应 403，实际 %v", err)
	}
	if err := f.svc.DeleteWorker(ctx, actorWith("u9", logic.SystemUsersWorkersDelete), "u1"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if len(f.workers.softDeleted) != 1 || f.workers.softDeleted[0] != "u1" {
		t.Errorf("软删 = %v", f.workers.softDeleted)
	}

	if err := f.svc.UpdateWorkerStatus(ctx, actorWith(domain.RootLoginID), "ghost", 1); businessMessage(t, err) != MessageWorkerNotExist {
		t.Errorf("不存在提示 = %v", err)
	}
}

func TestAssignWorkerRolesRequiresPermissionAndValidatesRootOnly(t *testing.T) {
	f := newAdminFixture(t)
	seedRole(f, "r1", "editor", "编辑", 1)
	seedRole(f, "r0", logic.RoleCodeAdmin, "管理员", 1)
	ctx := context.Background()

	// 非 root 无 assign-role 权限：403。
	if err := f.svc.AssignWorkerRoles(ctx, actorWith("u9"), "w1", []string{"r1"}); apperr.HTTPStatusOf(err) != apperr.StatusForbidden {
		t.Fatalf("应 403，实际 %v", err)
	}
	// 有权限但分配 admin 角色：业务失败。
	err := f.svc.AssignWorkerRoles(ctx, actorWith("u9", logic.SystemUsersWorkersAssignRole), "w1", []string{"r0"})
	if got := businessMessage(t, err); got != "只有超级管理员才能分配 管理员 角色" {
		t.Errorf("提示 = %q", got)
	}
	// root 任意分配。
	if err := f.svc.AssignWorkerRoles(ctx, actorWith(domain.RootLoginID), "w1", []string{"r0", "ghost"}); err != nil {
		t.Fatalf("root 分配失败: %v", err)
	}
	if got := f.workers.roleInserted["w1"]; len(got) != 1 || got[0] != "r0" {
		t.Errorf("有效角色 = %v", got)
	}
	// 自操作绕过：自己给自己分配角色（无权限）。
	if err := f.svc.AssignWorkerRoles(ctx, actorWith("w1"), "w1", []string{"r1"}); err != nil {
		t.Errorf("自操作应放行: %v", err)
	}
}

func TestListWorkersMapsEnabledRolesOnly(t *testing.T) {
	f := newAdminFixture(t)
	now := time.Now()
	f.workers.workers["w1"] = &domain.AdminWorker{ID: "w1", Username: "worker", Status: 1, CreatedAt: now, UpdatedAt: now}
	seedRole(f, "r1", "editor", "编辑", 1)
	seedRole(f, "r2", "viewer", "访客", 1)
	// RoleCodes 只返回启用角色（基础设施层已过滤），这里模拟 editor 启用、viewer 禁用。
	f.workers.roles["w1"] = []string{"editor"}

	page, err := f.svc.ListWorkers(context.Background(), 1, 10, "")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != 1 || len(page.List) != 1 {
		t.Fatalf("PageData = %+v", page)
	}
	if len(page.List[0].Roles) != 1 || page.List[0].Roles[0].Code != "editor" {
		t.Errorf("roles = %+v", page.List[0].Roles)
	}
	if page.List[0].UpdatedAt != now.Format(domain.DateTimeLayout) {
		t.Errorf("updatedAt = %q", page.List[0].UpdatedAt)
	}
}

// ---- 角色 ----

func TestCreateRoleOnlyRootCanCreateAdminRole(t *testing.T) {
	f := newAdminFixture(t, "role-1")
	ctx := context.Background()

	_, err := f.svc.CreateRole(ctx, actorWith("u9"), domain.RoleSaveDTO{Code: ptr(logic.RoleCodeAdmin), Name: ptr("管理员")})
	if got := businessMessage(t, err); got != MessageRoleOnlyRootCreateAdmin {
		t.Errorf("提示 = %q", got)
	}

	vo, err := f.svc.CreateRole(ctx, actorWith(domain.RootLoginID), domain.RoleSaveDTO{
		Code: ptr("editor"), Name: ptr("fallback"),
		I18nValue: []domain.I18nValue{{I18n: "zh-CN", Value: "编辑"}, {I18n: "en-US", Value: "Editor"}},
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if vo.ID != "role-1" || vo.Name != "编辑" || vo.Status != 1 {
		t.Errorf("WorkerRoleVO = %+v", vo)
	}
	if len(f.roles.insertedRoles) != 1 || f.roles.insertedRoles[0].Name != "编辑" {
		t.Errorf("入库角色 = %+v", f.roles.insertedRoles)
	}
}

func TestUpdateRoleProtectsRootAndAdminCode(t *testing.T) {
	f := newAdminFixture(t)
	seedRole(f, "r0", logic.RoleCodeRoot, "超级管理员", 1)
	seedRole(f, "r1", logic.RoleCodeAdmin, "管理员", 1)
	seedRole(f, "r2", "editor", "编辑", 1)
	ctx := context.Background()

	_, err := f.svc.UpdateRole(ctx, actorWith("u9"), "r0", domain.RoleSaveDTO{Code: ptr("root-x")})
	if got := businessMessage(t, err); got != MessageRoleOnlyRootEditAdminCode {
		t.Errorf("提示 = %q", got)
	}
	// 不改 code 时不受限制。
	vo, err := f.svc.UpdateRole(ctx, actorWith("u9"), "r2", domain.RoleSaveDTO{Name: ptr("资深编辑"), Description: ptr("d")})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if vo.Name != "资深编辑" || vo.Description == nil || *vo.Description != "d" {
		t.Errorf("WorkerRoleVO = %+v", vo)
	}
	// root 可以改 admin 角色的 code 字段（但实现不落库 code）。
	if _, err := f.svc.UpdateRole(ctx, actorWith(domain.RootLoginID), "r1", domain.RoleSaveDTO{Code: ptr("admin"), Name: ptr("管理员2")}); err != nil {
		t.Fatalf("root 更新失败: %v", err)
	}
	if f.roles.roles["r1"].Code != logic.RoleCodeAdmin {
		t.Errorf("code 不应被修改: %s", f.roles.roles["r1"].Code)
	}
	_, err = f.svc.UpdateRole(ctx, actorWith(domain.RootLoginID), "ghost", domain.RoleSaveDTO{})
	if got := businessMessage(t, err); got != MessageRoleNotExist {
		t.Errorf("不存在提示 = %q", got)
	}
}

func TestDeleteRoleRules(t *testing.T) {
	f := newAdminFixture(t)
	seedRole(f, "r0", logic.RoleCodeRoot, "超级管理员", 1)
	seedRole(f, "r1", logic.RoleCodeAdmin, "管理员", 1)
	seedRole(f, "r2", "editor", "编辑", 1)
	ctx := context.Background()

	if err := f.svc.DeleteRole(ctx, actorWith(domain.RootLoginID), "r0"); businessMessage(t, err) != MessageRoleRootUndeletable {
		t.Errorf("root 角色删除提示 = %v", err)
	}
	if err := f.svc.DeleteRole(ctx, actorWith("u9"), "r1"); businessMessage(t, err) != MessageRoleOnlyRootDeleteAdmin {
		t.Errorf("admin 角色非 root 删除提示 = %v", err)
	}
	if err := f.svc.DeleteRole(ctx, actorWith("u9"), "r2"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if len(f.roles.userRelDeleted) != 1 || len(f.roles.permReset) == 0 || len(f.roles.softDeleted) != 1 {
		t.Errorf("删除必须清理用户关联与权限关联: %+v", f.roles)
	}
}

func TestUpdateRoleStatusRules(t *testing.T) {
	f := newAdminFixture(t)
	seedRole(f, "r0", logic.RoleCodeRoot, "超级管理员", 1)
	seedRole(f, "r2", "editor", "编辑", 1)
	ctx := context.Background()

	if err := f.svc.UpdateRoleStatus(ctx, "r0", ptrInt(0)); businessMessage(t, err) != MessageRoleRootUndisabled {
		t.Errorf("root 禁用提示 = %v", err)
	}
	if err := f.svc.UpdateRoleStatus(ctx, "r2", nil); businessMessage(t, err) != MessageRoleStatusInvalid {
		t.Errorf("空状态提示 = %v", err)
	}
	if err := f.svc.UpdateRoleStatus(ctx, "r2", ptrInt(2)); businessMessage(t, err) != MessageRoleStatusInvalid {
		t.Errorf("非法状态提示 = %v", err)
	}
	if err := f.svc.UpdateRoleStatus(ctx, "r2", ptrInt(0)); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	if f.roles.roles["r2"].Status != 0 {
		t.Errorf("status = %d", f.roles.roles["r2"].Status)
	}
}

func TestListRolesVariants(t *testing.T) {
	f := newAdminFixture(t)
	seedRole(f, "r1", "editor", "编辑", 1)
	seedRole(f, "r2", "viewer", "访客", 1)

	options, err := f.svc.ListRolesForSelect(context.Background())
	if err != nil || len(options) != 2 {
		t.Fatalf("options=%v err=%v", options, err)
	}
	if options[0].Label != "编辑 (editor)" || options[0].Value != "r1" {
		t.Errorf("SelectVO = %+v", options[0])
	}

	all, err := f.svc.ListRolesAll(context.Background())
	if err != nil || len(all) != 2 {
		t.Fatalf("all=%v err=%v", all, err)
	}
	if all[0].Code != "editor" || all[0].IsDeleted != 0 || all[0].CreatedAt == "" {
		t.Errorf("AdminRoleEntity = %+v", all[0])
	}

	page, err := f.svc.ListRolesPage(context.Background(), 2, 1)
	if err != nil {
		t.Fatalf("分页失败: %v", err)
	}
	if page.Total != 2 || page.PageIndex != 2 || len(page.List) != 1 || page.List[0].Code != "viewer" {
		t.Errorf("PageData = %+v", page)
	}
}

func TestListRolePermissionIDsAndAssignPermissions(t *testing.T) {
	f := newAdminFixture(t)
	seedRole(f, "r1", "editor", "编辑", 1)
	f.roles.permID["r1"] = []string{"p1"}
	f.roles.pages["page-1"] = true
	f.permissions.permissions["btn-1"] = &domain.AdminPermission{ID: "btn-1", Code: "c1", Name: "n1"}
	ctx := context.Background()

	ids, err := f.svc.ListRolePermissionIDs(ctx, "r1")
	if err != nil || len(ids) != 1 || ids[0] != "p1" {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	if _, err := f.svc.ListRolePermissionIDs(ctx, "ghost"); businessMessage(t, err) != MessageRoleNotExist {
		t.Errorf("不存在角色提示 = %v", err)
	}

	// 页面 + 按钮 + 不存在 id + 重复 id。
	err = f.svc.AssignRolePermissions(ctx, "r1", []string{"page-1", "btn-1", "ghost", "page-1", ""})
	if err != nil {
		t.Fatalf("分配权限失败: %v", err)
	}
	if len(f.roles.permReset) != 1 || f.roles.permReset[0] != "r1" {
		t.Errorf("必须先软删全部权限关联: %v", f.roles.permReset)
	}
	if len(f.roles.permWrites) != 2 {
		t.Fatalf("写入分组 = %+v", f.roles.permWrites)
	}
	if f.roles.permWrites[0].ResourceType != 0 || len(f.roles.permWrites[0].IDs) != 1 || f.roles.permWrites[0].IDs[0] != "page-1" {
		t.Errorf("页面分组 = %+v", f.roles.permWrites[0])
	}
	if f.roles.permWrites[1].ResourceType != 1 || len(f.roles.permWrites[1].IDs) != 1 || f.roles.permWrites[1].IDs[0] != "btn-1" {
		t.Errorf("按钮分组 = %+v", f.roles.permWrites[1])
	}

	// 空列表只软删。
	f.roles.permWrites = nil
	if err := f.svc.AssignRolePermissions(ctx, "r1", nil); err != nil {
		t.Fatalf("清空权限失败: %v", err)
	}
	if len(f.roles.permWrites) != 0 || len(f.roles.permReset) != 2 {
		t.Errorf("空列表应只软删: writes=%+v resets=%v", f.roles.permWrites, f.roles.permReset)
	}
	if err := f.svc.AssignRolePermissions(ctx, "ghost", []string{"p1"}); businessMessage(t, err) != MessageRoleNotExist {
		t.Errorf("不存在角色提示 = %v", err)
	}
}

// ---- 权限 ----

func TestCreatePermissionValidatesAndRestoresDeleted(t *testing.T) {
	f := newAdminFixture(t, "perm-1")
	ctx := context.Background()

	if _, err := f.svc.CreatePermission(ctx, domain.PermissionSaveDTO{}); businessMessage(t, err) != MessagePermissionCodeRequired {
		t.Errorf("空 code 提示 = %v", err)
	}
	if _, err := f.svc.CreatePermission(ctx, domain.PermissionSaveDTO{Code: ptr("c1")}); businessMessage(t, err) != MessagePermissionNameRequired {
		t.Errorf("空 name 提示 = %v", err)
	}

	vo, err := f.svc.CreatePermission(ctx, domain.PermissionSaveDTO{Code: ptr(" c1 "), Name: ptr(" 新增 "), PageID: ptr("page-1")})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if vo.ID != "perm-1" || vo.Code != "c1" || vo.Name != "新增" || vo.IsDeleted != 0 {
		t.Errorf("AdminPermissionEntity = %+v", vo)
	}

	// code 重复（未删）。
	if _, err := f.svc.CreatePermission(ctx, domain.PermissionSaveDTO{Code: ptr("c1"), Name: ptr("x")}); businessMessage(t, err) != MessagePermissionCodeExists {
		t.Errorf("重复 code 提示 = %v", err)
	}

	// 已软删同 code：恢复。
	f.permissions.deleted["old"] = &domain.AdminPermission{ID: "old", Code: "c2", Name: "旧", CreatedAt: time.Now()}
	restored, err := f.svc.CreatePermission(ctx, domain.PermissionSaveDTO{Code: ptr("c2"), Name: ptr("恢复"), PageID: ptr("page-2")})
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if restored.ID != "old" || restored.Name != "恢复" || restored.PageID == nil || *restored.PageID != "page-2" {
		t.Errorf("恢复结果 = %+v", restored)
	}
	if len(f.permissions.restored) != 1 || f.permissions.restored[0] != "old" {
		t.Errorf("恢复调用 = %v", f.permissions.restored)
	}
}

func TestCreatePermissionUsesI18nName(t *testing.T) {
	f := newAdminFixture(t, "perm-1")
	vo, err := f.svc.CreatePermission(context.Background(), domain.PermissionSaveDTO{
		Code: ptr("c1"), Name: ptr("fallback"),
		I18nValue: []domain.I18nValue{{I18n: "zh-CN", Value: "中文名"}},
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if vo.Name != "中文名" {
		t.Errorf("name = %q", vo.Name)
	}
}

func TestUpdatePermissionValidatesAndKeepsPageID(t *testing.T) {
	f := newAdminFixture(t)
	pageID := "page-1"
	f.permissions.permissions["p1"] = &domain.AdminPermission{ID: "p1", Code: "c1", Name: "旧", PageID: &pageID, CreatedAt: time.Now()}
	f.permissions.permissions["p2"] = &domain.AdminPermission{ID: "p2", Code: "c2", Name: "其他", CreatedAt: time.Now()}
	ctx := context.Background()

	vo, err := f.svc.UpdatePermission(ctx, "p1", domain.PermissionSaveDTO{Code: ptr("c1"), Name: ptr("新")})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if vo.Name != "新" || vo.PageID == nil || *vo.PageID != "page-1" {
		t.Errorf("pageId 为空时不应清空: %+v", vo)
	}
	if _, err := f.svc.UpdatePermission(ctx, "p1", domain.PermissionSaveDTO{Code: ptr("c2"), Name: ptr("新")}); businessMessage(t, err) != MessagePermissionCodeExists {
		t.Errorf("重复 code 提示 = %v", err)
	}
	if _, err := f.svc.UpdatePermission(ctx, "ghost", domain.PermissionSaveDTO{Code: ptr("x"), Name: ptr("y")}); businessMessage(t, err) != MessagePermissionNotExist {
		t.Errorf("不存在提示 = %v", err)
	}
}

func TestListPermissionsAndDelete(t *testing.T) {
	f := newAdminFixture(t)
	pageID := "page-1"
	f.permissions.permissions["p1"] = &domain.AdminPermission{ID: "p1", Code: "c1", Name: "字典", PageID: &pageID, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	ctx := context.Background()

	list, err := f.svc.ListPermissions(ctx)
	if err != nil || len(list) != 1 || list[0].Code != "c1" {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if list[0].CreatedAt == "" || list[0].PageID == nil {
		t.Errorf("实体字段 = %+v", list[0])
	}
	vos, err := f.svc.ListPermissionsAsVO(ctx)
	if err != nil || len(vos) != 1 || vos[0].PageID == nil {
		t.Fatalf("vos=%v err=%v", vos, err)
	}
	if err := f.svc.DeletePermission(ctx, "p1"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if len(f.permissions.removed) != 1 {
		t.Errorf("软删调用 = %v", f.permissions.removed)
	}
	if err := f.svc.DeletePermission(ctx, "ghost"); businessMessage(t, err) != MessagePermissionNotExist {
		t.Errorf("不存在提示 = %v", err)
	}
}

func TestAdminReposMissingReturnsInfrastructureError(t *testing.T) {
	// 管理数据访问未注入时返回普通 error（httpapi 转 503），而不是业务失败。
	svc, err := New(Options{Tx: passthroughTx{}, Workers: newFakeWorkerRepo(), Pages: &fakePageRepo{}, Modules: &fakeModuleRepo{},
		Satoken: actorLogic()})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	_, err = svc.ListReaders(context.Background(), 1, 10, "")
	if _, isBusiness := apperr.IsBusiness(err); err == nil || isBusiness {
		t.Fatalf("未注入依赖应返回基础设施错误，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "未注入") {
		t.Errorf("错误信息 = %v", err)
	}
	if _, err := svc.ListPermissions(context.Background()); err == nil {
		t.Errorf("权限数据访问未注入应报错")
	}
}

func ptr(value string) *string { return &value }

func ptrInt(value int) *int { return &value }
