package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
	"github.com/acat-fun/acat-go-admin-system/auth/repo"
	"github.com/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/satoken"
)

// passthroughTx 让不关心事务的测试直接执行用例函数；真实提交/回滚由 tx_test.go 覆盖。
type passthroughTx struct{}

func (passthroughTx) Within(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// 本文件提供管理接口数据访问的内存 fake（沿用 service_test.go 的 fakeWorkerRepo 模式）。

// ---- 读者用户 ----

type fakeReaderRepo struct {
	users map[string]*domain.ReaderUser
	roles map[string][]string

	listCalls     int
	lastKeyword   string
	softDeleted   []string
	roleReset     []string
	roleInserted  map[string][]string
	insertedUser  *domain.ReaderUser
	updateCounter int
	// resolveCode 把角色 id 解析为角色编码（t_acat_user_role 存 id，RoleCodes 返回 code）。
	resolveCode func(id string) string
}

func newFakeReaderRepo() *fakeReaderRepo {
	return &fakeReaderRepo{
		users:        map[string]*domain.ReaderUser{},
		roles:        map[string][]string{},
		roleInserted: map[string][]string{},
	}
}

func (f *fakeReaderRepo) List(_ context.Context, keyword string, offset, limit int) ([]domain.ReaderUser, int64, error) {
	f.listCalls++
	f.lastKeyword = keyword
	ids := make([]string, 0, len(f.users))
	for id := range f.users {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]domain.ReaderUser, 0, len(ids))
	for _, id := range ids {
		user := f.users[id]
		if keyword != "" && !strings.Contains(user.Username, keyword) && !strings.Contains(deref(user.Email), keyword) {
			continue
		}
		out = append(out, *user)
	}
	total := int64(len(out))
	if offset > len(out) {
		return []domain.ReaderUser{}, total, nil
	}
	out = out[offset:]
	if limit < len(out) {
		out = out[:limit]
	}
	return out, total, nil
}

func (f *fakeReaderRepo) FindByUsername(_ context.Context, username string) (*domain.ReaderUser, error) {
	for _, user := range f.users {
		if user.Username == username {
			return user, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (f *fakeReaderRepo) FindByID(_ context.Context, id string) (*domain.ReaderUser, error) {
	user, ok := f.users[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return user, nil
}

func (f *fakeReaderRepo) Insert(_ context.Context, user *domain.ReaderUser) error {
	f.insertedUser = user
	copied := *user
	f.users[user.ID] = &copied
	return nil
}

func (f *fakeReaderRepo) Update(_ context.Context, user *domain.ReaderUser) (int64, error) {
	f.updateCounter++
	copied := *user
	f.users[user.ID] = &copied
	return 1, nil
}

func (f *fakeReaderRepo) SoftDelete(_ context.Context, id string) (int64, error) {
	f.softDeleted = append(f.softDeleted, id)
	return 1, nil
}

func (f *fakeReaderRepo) RoleCodes(_ context.Context, userID string) ([]string, error) {
	return f.roles[userID], nil
}

func (f *fakeReaderRepo) SoftDeleteRoles(_ context.Context, userID string) (int64, error) {
	f.roleReset = append(f.roleReset, userID)
	delete(f.roles, userID)
	return 1, nil
}

func (f *fakeReaderRepo) InsertRoles(_ context.Context, userID string, roleIDs []string) (int64, error) {
	f.roleInserted[userID] = roleIDs
	// 真实实现会写入关联表，RoleCodes 立即可见（返回角色编码而不是 id）。
	codes := make([]string, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		codes = append(codes, f.resolve(roleID))
	}
	f.roles[userID] = codes
	return 1, nil
}

// resolve 把角色 id 解析为编码；未注入解析器时原样返回（测试可直接塞编码）。
func (f *fakeReaderRepo) resolve(id string) string {
	if f.resolveCode == nil {
		return id
	}
	return f.resolveCode(id)
}

// ---- 工作人员 ----

type fakeAdminWorkerRepo struct {
	workers map[string]*domain.AdminWorker
	roles   map[string][]string

	softDeleted  []string
	roleReset    []string
	roleInserted map[string][]string
	lastKeyword  string
	// resolveCode 把角色 id 解析为角色编码（t_acat_user_role 存 id，RoleCodes 返回 code）。
	resolveCode func(id string) string
}

func newFakeAdminWorkerRepo() *fakeAdminWorkerRepo {
	return &fakeAdminWorkerRepo{
		workers:      map[string]*domain.AdminWorker{},
		roles:        map[string][]string{},
		roleInserted: map[string][]string{},
	}
}

func (f *fakeAdminWorkerRepo) List(_ context.Context, keyword string, offset, limit int) ([]domain.AdminWorker, int64, error) {
	f.lastKeyword = keyword
	ids := make([]string, 0, len(f.workers))
	for id := range f.workers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]domain.AdminWorker, 0, len(ids))
	for _, id := range ids {
		worker := f.workers[id]
		if keyword != "" && !strings.Contains(worker.Username, keyword) && !strings.Contains(deref(worker.Email), keyword) {
			continue
		}
		out = append(out, *worker)
	}
	total := int64(len(out))
	if offset > len(out) {
		return []domain.AdminWorker{}, total, nil
	}
	out = out[offset:]
	if limit < len(out) {
		out = out[:limit]
	}
	return out, total, nil
}

func (f *fakeAdminWorkerRepo) FindByUsername(_ context.Context, username string) (*domain.AdminWorker, error) {
	for _, worker := range f.workers {
		if worker.Username == username {
			return worker, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (f *fakeAdminWorkerRepo) FindByID(_ context.Context, id string) (*domain.AdminWorker, error) {
	worker, ok := f.workers[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return worker, nil
}

func (f *fakeAdminWorkerRepo) Insert(_ context.Context, worker *domain.AdminWorker) error {
	copied := *worker
	f.workers[worker.ID] = &copied
	return nil
}

func (f *fakeAdminWorkerRepo) Update(_ context.Context, worker *domain.AdminWorker) (int64, error) {
	copied := *worker
	f.workers[worker.ID] = &copied
	return 1, nil
}

func (f *fakeAdminWorkerRepo) SoftDelete(_ context.Context, id string) (int64, error) {
	f.softDeleted = append(f.softDeleted, id)
	return 1, nil
}

func (f *fakeAdminWorkerRepo) RoleCodes(_ context.Context, userID string) ([]string, error) {
	return f.roles[userID], nil
}

func (f *fakeAdminWorkerRepo) SoftDeleteRoles(_ context.Context, userID string) (int64, error) {
	f.roleReset = append(f.roleReset, userID)
	delete(f.roles, userID)
	return 1, nil
}

func (f *fakeAdminWorkerRepo) InsertRoles(_ context.Context, userID string, roleIDs []string) (int64, error) {
	f.roleInserted[userID] = roleIDs
	// 真实实现会写入关联表，RoleCodes 立即可见（返回角色编码而不是 id）。
	codes := make([]string, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		codes = append(codes, f.resolve(roleID))
	}
	f.roles[userID] = codes
	return 1, nil
}

// resolve 把角色 id 解析为编码；未注入解析器时原样返回（测试可直接塞编码）。
func (f *fakeAdminWorkerRepo) resolve(id string) string {
	if f.resolveCode == nil {
		return id
	}
	return f.resolveCode(id)
}

// ---- 角色 ----

type permissionWrite struct {
	RoleID       string
	IDs          []string
	ResourceType int
}

type fakeRoleRepo struct {
	roles  map[string]*domain.Role
	pages  map[string]bool
	permID map[string][]string

	permReset      []string
	permWrites     []permissionWrite
	userRelDeleted []string
	softDeleted    []string
	insertedRoles  []domain.Role
}

func newFakeRoleRepo() *fakeRoleRepo {
	return &fakeRoleRepo{
		roles:  map[string]*domain.Role{},
		pages:  map[string]bool{},
		permID: map[string][]string{},
	}
}

func (f *fakeRoleRepo) List(context.Context) ([]domain.Role, error) {
	ids := make([]string, 0, len(f.roles))
	for id := range f.roles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]domain.Role, 0, len(ids))
	for _, id := range ids {
		out = append(out, *f.roles[id])
	}
	return out, nil
}

func (f *fakeRoleRepo) ListPage(_ context.Context, offset, limit int) ([]domain.Role, int64, error) {
	all, _ := f.List(context.Background())
	total := int64(len(all))
	if offset > len(all) {
		return []domain.Role{}, total, nil
	}
	all = all[offset:]
	if limit < len(all) {
		all = all[:limit]
	}
	return all, total, nil
}

func (f *fakeRoleRepo) FindByID(_ context.Context, id string) (*domain.Role, error) {
	role, ok := f.roles[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return role, nil
}

func (f *fakeRoleRepo) FindByIDs(_ context.Context, ids []string) ([]domain.Role, error) {
	out := make([]domain.Role, 0, len(ids))
	for _, id := range ids {
		if role, ok := f.roles[id]; ok {
			out = append(out, *role)
		}
	}
	return out, nil
}

func (f *fakeRoleRepo) Insert(_ context.Context, role *domain.Role) error {
	copied := *role
	f.roles[role.ID] = &copied
	f.insertedRoles = append(f.insertedRoles, copied)
	return nil
}

func (f *fakeRoleRepo) Update(_ context.Context, role *domain.Role) (int64, error) {
	copied := *role
	f.roles[role.ID] = &copied
	return 1, nil
}

func (f *fakeRoleRepo) SoftDelete(_ context.Context, id string) (int64, error) {
	f.softDeleted = append(f.softDeleted, id)
	return 1, nil
}

func (f *fakeRoleRepo) SoftDeleteUserRelations(_ context.Context, roleID string) (int64, error) {
	f.userRelDeleted = append(f.userRelDeleted, roleID)
	return 1, nil
}

func (f *fakeRoleRepo) SoftDeletePermissions(_ context.Context, roleID string) (int64, error) {
	f.permReset = append(f.permReset, roleID)
	f.permID[roleID] = nil
	return 1, nil
}

func (f *fakeRoleRepo) PermissionIDs(_ context.Context, roleID string) ([]string, error) {
	return f.permID[roleID], nil
}

func (f *fakeRoleRepo) ExistingPageIDs(_ context.Context, ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if f.pages[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *fakeRoleRepo) InsertPermissions(_ context.Context, roleID string, ids []string, resourceType int) (int64, error) {
	f.permWrites = append(f.permWrites, permissionWrite{RoleID: roleID, IDs: ids, ResourceType: resourceType})
	return 1, nil
}

// ---- 权限 ----

type fakePermissionRepo struct {
	permissions map[string]*domain.AdminPermission
	deleted     map[string]*domain.AdminPermission

	restored []string
	inserted []domain.AdminPermission
	removed  []string
}

func newFakePermissionRepo() *fakePermissionRepo {
	return &fakePermissionRepo{
		permissions: map[string]*domain.AdminPermission{},
		deleted:     map[string]*domain.AdminPermission{},
	}
}

func (f *fakePermissionRepo) List(context.Context) ([]domain.AdminPermission, error) {
	ids := make([]string, 0, len(f.permissions))
	for id := range f.permissions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]domain.AdminPermission, 0, len(ids))
	for _, id := range ids {
		out = append(out, *f.permissions[id])
	}
	return out, nil
}

func (f *fakePermissionRepo) FindByID(_ context.Context, id string) (*domain.AdminPermission, error) {
	permission, ok := f.permissions[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return permission, nil
}

func (f *fakePermissionRepo) FindDeletedByCode(_ context.Context, code string) (*domain.AdminPermission, error) {
	for _, permission := range f.deleted {
		if permission.Code == code {
			return permission, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (f *fakePermissionRepo) ExistsByCode(_ context.Context, code, excludeID string) (bool, error) {
	for id, permission := range f.permissions {
		if id == excludeID {
			continue
		}
		if permission.Code == code {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakePermissionRepo) FindByIDs(_ context.Context, ids []string) ([]domain.AdminPermission, error) {
	out := make([]domain.AdminPermission, 0, len(ids))
	for _, id := range ids {
		if permission, ok := f.permissions[id]; ok {
			out = append(out, *permission)
		}
	}
	return out, nil
}

func (f *fakePermissionRepo) Insert(_ context.Context, permission *domain.AdminPermission) error {
	copied := *permission
	f.permissions[permission.ID] = &copied
	f.inserted = append(f.inserted, copied)
	return nil
}

func (f *fakePermissionRepo) Update(_ context.Context, permission *domain.AdminPermission) (int64, error) {
	copied := *permission
	f.permissions[permission.ID] = &copied
	return 1, nil
}

func (f *fakePermissionRepo) SoftDelete(_ context.Context, id string) (int64, error) {
	f.removed = append(f.removed, id)
	if permission, ok := f.permissions[id]; ok {
		f.deleted[id] = permission
		delete(f.permissions, id)
	}
	return 1, nil
}

func (f *fakePermissionRepo) Restore(_ context.Context, id, name string, pageID *string, updatedAt time.Time) error {
	permission, ok := f.deleted[id]
	if !ok {
		return repo.ErrNotFound
	}
	permission.Name = name
	permission.PageID = pageID
	permission.UpdatedAt = updatedAt
	f.permissions[id] = permission
	delete(f.deleted, id)
	f.restored = append(f.restored, id)
	return nil
}

// ---- 公共工具 ----

// adminFixture 组装带管理数据访问的 Service 与内存 fake。
type adminFixture struct {
	svc         *Service
	readers     *fakeReaderRepo
	workers     *fakeAdminWorkerRepo
	roles       *fakeRoleRepo
	permissions *fakePermissionRepo
	tokenName   string
}

func newAdminFixture(t *testing.T, idSequence ...string) *adminFixture {
	t.Helper()
	workers := newFakeWorkerRepo()
	pages := &fakePageRepo{}
	modules := &fakeModuleRepo{}
	satokenLogic := satoken.NewLogic(satoken.Config{
		TokenName: DefaultTokenNameForTest,
		Timeout:   3600,
		Now:       func() time.Time { return time.Unix(1730000000, 0) },
	}, satoken.NewMemoryStore(), nil)

	readers := newFakeReaderRepo()
	adminWorkers := newFakeAdminWorkerRepo()
	roles := newFakeRoleRepo()
	permissions := newFakePermissionRepo()

	resolveRoleCode := func(id string) string {
		if role, ok := roles.roles[id]; ok {
			return role.Code
		}
		return id
	}
	readers.resolveCode = resolveRoleCode
	adminWorkers.resolveCode = resolveRoleCode
	// root 登录 id 的测试工作人员默认绑 root 角色（生产路径由种子数据 t_acat_user_role 提供）。
	adminWorkers.roles[domain.RootLoginID] = []string{domain.RootRoleID}

	next := 0
	svc, err := New(Options{
		Workers: workers, Pages: pages, Modules: modules, Satoken: satokenLogic,
		Readers: readers, AdminWorkers: adminWorkers, Roles: roles, Permissions: permissions,
		Tx: passthroughTx{},
		NewID: func() string {
			if next < len(idSequence) {
				id := idSequence[next]
				next++
				return id
			}
			next++
			return fmt.Sprintf("generated-%d", next)
		},
	})
	if err != nil {
		t.Fatalf("构造 Service 失败: %v", err)
	}
	return &adminFixture{svc: svc, readers: readers, workers: adminWorkers, roles: roles, permissions: permissions}
}

// actorLogic 构造测试用 Sa-Token 兼容 Logic。
func actorLogic() *satoken.Logic {
	return satoken.NewLogic(satoken.Config{
		TokenName: DefaultTokenNameForTest,
		Timeout:   3600,
		Now:       func() time.Time { return time.Unix(1730000000, 0) },
	}, satoken.NewMemoryStore(), nil)
}

// actor 构造与服务运行期一致的 Actor（超管由会话 roles 含 root 判定）。
func actorWith(loginID string, permissions ...string) *logic.Actor {
	satokenLogic := actorLogic()
	session := satoken.NewSession(satoken.NewSessionID())
	session.LoginID = loginID
	session.Set(satoken.DataKeyPermissions, permissions)
	if loginID == domain.RootLoginID {
		// 迁移期测试便利：root 登录 id 的会话补 root 角色（生产路径由 buildBootstrap 写入）。
		session.Set(satoken.DataKeyRoles, []string{domain.RootRoleID})
	}
	ctx := middleware.WithSession(context.Background(), session, "token-value")
	return logic.ActorFrom(ctx, logic.NewChecker(satokenLogic))
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
