package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
	"github.com/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-admin-system/auth/repo"
	"github.com/acat-fun/acat-go-admin-system/auth/service"
	"github.com/acat-fun/acat-go-common/config"
	"github.com/acat-fun/acat-go-common/satoken"
)

// passthroughTx 让不关心事务的测试直接执行用例函数；真实提交/回滚由 tx_test.go 覆盖。
type passthroughTx struct{}

func (passthroughTx) Within(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// ---- WorkerRepo stub（多账号，支持 root 与受限账号）----

type adminStubWorkers struct {
	byUsername map[string]*domain.Worker
	byID       map[string]*domain.Worker
	roles      map[string][]string
	perms      map[string][]string
	urls       map[string][]string
}

func newAdminStubWorkers() *adminStubWorkers {
	return &adminStubWorkers{
		byUsername: map[string]*domain.Worker{},
		byID:       map[string]*domain.Worker{},
		roles:      map[string][]string{},
		perms:      map[string][]string{},
		urls:       map[string][]string{},
	}
}

func (s *adminStubWorkers) add(id, username, password string, permissions ...string) {
	hashed, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	worker := &domain.Worker{ID: id, Username: username, Password: string(hashed), Status: 1}
	s.byUsername[username] = worker
	s.byID[id] = worker
	if permissions != nil {
		s.perms[id] = permissions
	}
}

// addRoot 注册绑定 root 角色的超管账号（登录后 buildBootstrap 会话 roles 含 root）。
func (s *adminStubWorkers) addRoot(id, username, password string) {
	s.add(id, username, password)
	s.roles[id] = []string{domain.RootRoleID}
}

func (s *adminStubWorkers) FindByUsername(_ context.Context, username string) (*domain.Worker, error) {
	worker, ok := s.byUsername[username]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return worker, nil
}

func (s *adminStubWorkers) FindByID(_ context.Context, id string) (*domain.Worker, error) {
	worker, ok := s.byID[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return worker, nil
}

func (s *adminStubWorkers) RoleCodes(_ context.Context, userID string) ([]string, error) {
	return s.roles[userID], nil
}

func (s *adminStubWorkers) URLAndButtonCodes(_ context.Context, userID string) ([]string, error) {
	return s.perms[userID], nil
}

func (s *adminStubWorkers) URLCodes(_ context.Context, userID string) ([]string, error) {
	return s.urls[userID], nil
}

func (s *adminStubWorkers) AllPermissionCodes(context.Context) ([]string, error) {
	out := []string{}
	for _, codes := range s.perms {
		out = append(out, codes...)
	}
	return out, nil
}

func (s *adminStubWorkers) AllURLCodes(context.Context) ([]string, error) {
	out := []string{}
	for _, codes := range s.urls {
		out = append(out, codes...)
	}
	return out, nil
}

// ---- 管理数据访问 stub（内存实现，覆盖 HTTP 契约验证所需路径）----

type adminStubReaders struct {
	users map[string]*domain.ReaderUser
	roles map[string][]string
	// resolveCode 把角色 id 解析为编码（t_acat_user_role 存 id，selectRolesByUserId 返回 code）。
	resolveCode func(id string) string
}

func newAdminStubReaders() *adminStubReaders {
	return &adminStubReaders{users: map[string]*domain.ReaderUser{}, roles: map[string][]string{}}
}

func (s *adminStubReaders) List(_ context.Context, keyword string, offset, limit int) ([]domain.ReaderUser, int64, error) {
	ids := sortedKeys(s.users)
	out := make([]domain.ReaderUser, 0, len(ids))
	for _, id := range ids {
		user := s.users[id]
		if keyword != "" && !strings.Contains(user.Username, keyword) && !strings.Contains(derefString(user.Email), keyword) {
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

func (s *adminStubReaders) FindByUsername(_ context.Context, username string) (*domain.ReaderUser, error) {
	for _, user := range s.users {
		if user.Username == username {
			return user, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (s *adminStubReaders) FindByID(_ context.Context, id string) (*domain.ReaderUser, error) {
	user, ok := s.users[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return user, nil
}

func (s *adminStubReaders) Insert(_ context.Context, user *domain.ReaderUser) error {
	copied := *user
	s.users[user.ID] = &copied
	return nil
}

func (s *adminStubReaders) Update(_ context.Context, user *domain.ReaderUser) (int64, error) {
	copied := *user
	s.users[user.ID] = &copied
	return 1, nil
}

func (s *adminStubReaders) SoftDelete(_ context.Context, id string) (int64, error) {
	delete(s.users, id)
	return 1, nil
}

func (s *adminStubReaders) RoleCodes(_ context.Context, userID string) ([]string, error) {
	return s.roles[userID], nil
}

func (s *adminStubReaders) SoftDeleteRoles(_ context.Context, userID string) (int64, error) {
	delete(s.roles, userID)
	return 1, nil
}

func (s *adminStubReaders) InsertRoles(_ context.Context, userID string, roleIDs []string) (int64, error) {
	codes := make([]string, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		codes = append(codes, s.resolve(roleID))
	}
	s.roles[userID] = codes
	return 1, nil
}

func (s *adminStubReaders) resolve(id string) string {
	if s.resolveCode == nil {
		return id
	}
	return s.resolveCode(id)
}

type adminStubWorkersRepo struct {
	workers map[string]*domain.AdminWorker
	roles   map[string][]string
	// resolveCode 把角色 id 解析为编码（t_acat_user_role 存 id，selectRolesByUserId 返回 code）。
	resolveCode func(id string) string
}

func newAdminStubWorkersRepo() *adminStubWorkersRepo {
	return &adminStubWorkersRepo{workers: map[string]*domain.AdminWorker{}, roles: map[string][]string{}}
}

func (s *adminStubWorkersRepo) List(_ context.Context, keyword string, offset, limit int) ([]domain.AdminWorker, int64, error) {
	ids := sortedKeys(s.workers)
	out := make([]domain.AdminWorker, 0, len(ids))
	for _, id := range ids {
		worker := s.workers[id]
		if keyword != "" && !strings.Contains(worker.Username, keyword) && !strings.Contains(derefString(worker.Email), keyword) {
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

func (s *adminStubWorkersRepo) FindByUsername(_ context.Context, username string) (*domain.AdminWorker, error) {
	for _, worker := range s.workers {
		if worker.Username == username {
			return worker, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (s *adminStubWorkersRepo) FindByID(_ context.Context, id string) (*domain.AdminWorker, error) {
	worker, ok := s.workers[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return worker, nil
}

func (s *adminStubWorkersRepo) Insert(_ context.Context, worker *domain.AdminWorker) error {
	copied := *worker
	s.workers[worker.ID] = &copied
	return nil
}

func (s *adminStubWorkersRepo) Update(_ context.Context, worker *domain.AdminWorker) (int64, error) {
	copied := *worker
	s.workers[worker.ID] = &copied
	return 1, nil
}

func (s *adminStubWorkersRepo) SoftDelete(_ context.Context, id string) (int64, error) {
	delete(s.workers, id)
	return 1, nil
}

func (s *adminStubWorkersRepo) RoleCodes(_ context.Context, userID string) ([]string, error) {
	return s.roles[userID], nil
}

func (s *adminStubWorkersRepo) SoftDeleteRoles(_ context.Context, userID string) (int64, error) {
	delete(s.roles, userID)
	return 1, nil
}

func (s *adminStubWorkersRepo) InsertRoles(_ context.Context, userID string, roleIDs []string) (int64, error) {
	codes := make([]string, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		codes = append(codes, s.resolve(roleID))
	}
	s.roles[userID] = codes
	return 1, nil
}

func (s *adminStubWorkersRepo) resolve(id string) string {
	if s.resolveCode == nil {
		return id
	}
	return s.resolveCode(id)
}

type adminStubRoles struct {
	roles map[string]*domain.Role
	pages map[string]bool
	perms map[string][]string
}

func newAdminStubRoles() *adminStubRoles {
	return &adminStubRoles{roles: map[string]*domain.Role{}, pages: map[string]bool{}, perms: map[string][]string{}}
}

func (s *adminStubRoles) List(context.Context) ([]domain.Role, error) {
	ids := sortedKeys(s.roles)
	out := make([]domain.Role, 0, len(ids))
	for _, id := range ids {
		out = append(out, *s.roles[id])
	}
	return out, nil
}

func (s *adminStubRoles) ListPage(_ context.Context, offset, limit int) ([]domain.Role, int64, error) {
	all, _ := s.List(context.Background())
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

func (s *adminStubRoles) FindByID(_ context.Context, id string) (*domain.Role, error) {
	role, ok := s.roles[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return role, nil
}

func (s *adminStubRoles) FindByIDs(_ context.Context, ids []string) ([]domain.Role, error) {
	out := make([]domain.Role, 0, len(ids))
	for _, id := range ids {
		if role, ok := s.roles[id]; ok {
			out = append(out, *role)
		}
	}
	return out, nil
}

func (s *adminStubRoles) Insert(_ context.Context, role *domain.Role) error {
	copied := *role
	s.roles[role.ID] = &copied
	return nil
}

func (s *adminStubRoles) Update(_ context.Context, role *domain.Role) (int64, error) {
	copied := *role
	s.roles[role.ID] = &copied
	return 1, nil
}

func (s *adminStubRoles) SoftDelete(_ context.Context, id string) (int64, error) {
	delete(s.roles, id)
	return 1, nil
}

func (s *adminStubRoles) SoftDeleteUserRelations(context.Context, string) (int64, error) {
	return 1, nil
}

func (s *adminStubRoles) SoftDeletePermissions(_ context.Context, roleID string) (int64, error) {
	delete(s.perms, roleID)
	return 1, nil
}

func (s *adminStubRoles) PermissionIDs(_ context.Context, roleID string) ([]string, error) {
	if ids, ok := s.perms[roleID]; ok {
		return ids, nil
	}
	return []string{}, nil
}

func (s *adminStubRoles) ExistingPageIDs(_ context.Context, ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if s.pages[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *adminStubRoles) InsertPermissions(_ context.Context, roleID string, ids []string, _ int) (int64, error) {
	s.perms[roleID] = append(s.perms[roleID], ids...)
	return 1, nil
}

type adminStubPermissions struct {
	permissions map[string]*domain.AdminPermission
	deleted     map[string]*domain.AdminPermission
}

func newAdminStubPermissions() *adminStubPermissions {
	return &adminStubPermissions{permissions: map[string]*domain.AdminPermission{}, deleted: map[string]*domain.AdminPermission{}}
}

func (s *adminStubPermissions) List(context.Context) ([]domain.AdminPermission, error) {
	ids := sortedKeys(s.permissions)
	out := make([]domain.AdminPermission, 0, len(ids))
	for _, id := range ids {
		out = append(out, *s.permissions[id])
	}
	return out, nil
}

func (s *adminStubPermissions) FindByID(_ context.Context, id string) (*domain.AdminPermission, error) {
	permission, ok := s.permissions[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return permission, nil
}

func (s *adminStubPermissions) FindDeletedByCode(_ context.Context, code string) (*domain.AdminPermission, error) {
	for _, permission := range s.deleted {
		if permission.Code == code {
			return permission, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (s *adminStubPermissions) ExistsByCode(_ context.Context, code, excludeID string) (bool, error) {
	for id, permission := range s.permissions {
		if id != excludeID && permission.Code == code {
			return true, nil
		}
	}
	return false, nil
}

func (s *adminStubPermissions) FindByIDs(_ context.Context, ids []string) ([]domain.AdminPermission, error) {
	out := make([]domain.AdminPermission, 0, len(ids))
	for _, id := range ids {
		if permission, ok := s.permissions[id]; ok {
			out = append(out, *permission)
		}
	}
	return out, nil
}

func (s *adminStubPermissions) Insert(_ context.Context, permission *domain.AdminPermission) error {
	copied := *permission
	s.permissions[permission.ID] = &copied
	return nil
}

func (s *adminStubPermissions) Update(_ context.Context, permission *domain.AdminPermission) (int64, error) {
	copied := *permission
	s.permissions[permission.ID] = &copied
	return 1, nil
}

func (s *adminStubPermissions) SoftDelete(_ context.Context, id string) (int64, error) {
	if permission, ok := s.permissions[id]; ok {
		s.deleted[id] = permission
		delete(s.permissions, id)
	}
	return 1, nil
}

func (s *adminStubPermissions) Restore(_ context.Context, id, name string, pageID *string, updatedAt time.Time) error {
	permission, ok := s.deleted[id]
	if !ok {
		return repo.ErrNotFound
	}
	permission.Name = name
	permission.PageID = pageID
	permission.UpdatedAt = updatedAt
	s.permissions[id] = permission
	delete(s.deleted, id)
	return nil
}

// ---- fixture ----

type adminFixture struct {
	handler     http.Handler
	workers     *adminStubWorkers
	readers     *adminStubReaders
	adminWorker *adminStubWorkersRepo
	roles       *adminStubRoles
	permissions *adminStubPermissions
}

func newAdminAPIFixture(t *testing.T) *adminFixture {
	t.Helper()
	workers := newAdminStubWorkers()
	// root：绑定 root 角色的超管账号。
	workers.addRoot(domain.RootLoginID, "root", "root-pw")
	// 受限账号：无任何权限码。
	workers.add("u-editor", "editor", "editor-pw", logic.SystemUsersReaders)

	readers := newAdminStubReaders()
	adminWorker := newAdminStubWorkersRepo()
	roles := newAdminStubRoles()
	permissions := newAdminStubPermissions()

	resolveRoleCode := func(id string) string {
		if role, ok := roles.roles[id]; ok {
			return role.Code
		}
		return id
	}
	readers.resolveCode = resolveRoleCode
	adminWorker.resolveCode = resolveRoleCode

	satokenLogic := satoken.NewLogic(satoken.Config{
		TokenName: testTokenName,
		Timeout:   3600,
		Now:       func() time.Time { return time.Unix(1730000000, 0) },
	}, satoken.NewMemoryStore(), nil)

	svc, err := service.New(service.Options{
		Tx:      passthroughTx{},
		Workers: workers, Pages: stubPages{}, Modules: stubModules{}, Satoken: satokenLogic,
		Readers: readers, AdminWorkers: adminWorker, Roles: roles, Permissions: permissions,
		NewID: func() string { return "generated-id" },
	})
	if err != nil {
		t.Fatalf("构造 Service 失败: %v", err)
	}
	api, err := New(Options{
		Service: svc,
		Satoken: satokenLogic,
		Config:  config.SaTokenConfig{TokenName: testTokenName, CookieName: testTokenName, CookiePath: "/", CookieSameSite: "Lax"},
	})
	if err != nil {
		t.Fatalf("构造 API 失败: %v", err)
	}
	mux := http.NewServeMux()
	api.Register(mux)
	return &adminFixture{handler: mux, workers: workers, readers: readers, adminWorker: adminWorker, roles: roles, permissions: permissions}
}

func (f *adminFixture) loginAs(t *testing.T, username, password string) *http.Cookie {
	t.Helper()
	body := strings.NewReader(fmt.Sprintf(`{"username":%q,"password":%q}`, username, password))
	req := httptest.NewRequest(http.MethodPost, PathAuthLogin, body)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("登录状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == testTokenName {
			return cookie
		}
	}
	t.Fatalf("登录未下发 Cookie: %v", rec.Result().Cookies())
	return nil
}

func (f *adminFixture) do(t *testing.T, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func decodeResult[T any](t *testing.T, rec *httptest.ResponseRecorder) (int, string, T) {
	t.Helper()
	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    T      `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应失败: %v (%s)", err, rec.Body.String())
	}
	return payload.Code, payload.Message, payload.Data
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// ---- 未登录 / 无权限 ----

func TestAdminRoutesRequireLogin(t *testing.T) {
	f := newAdminAPIFixture(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, PathReaders},
		{http.MethodPost, PathReaders},
		{http.MethodPut, PathReaders + "/u1"},
		{http.MethodPatch, PathReaders + "/u1"},
		{http.MethodDelete, PathReaders + "/u1"},
		{http.MethodPut, PathReaders + "/u1/roles"},
		{http.MethodGet, PathWorkers},
		{http.MethodGet, PathRoles},
		{http.MethodGet, PathPermissions},
	} {
		rec := f.do(t, tc.method, tc.path, "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s 未登录状态码 = %d, 期望 401", tc.method, tc.path, rec.Code)
		}
	}
}

func TestAdminRoutesForbiddenWithoutPermission(t *testing.T) {
	f := newAdminAPIFixture(t)
	cookie := f.loginAs(t, "root", "root-pw")
	_ = cookie
	limited := f.loginAs(t, "editor", "editor-pw")

	// 受限账号只有 readers 只读权限，其余管理接口必须 403。
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, PathWorkers, ""},
		{http.MethodGet, PathRoles, ""},
		{http.MethodGet, PathPermissions, ""},
		{http.MethodPost, PathReaders, `{"username":"x","password":"y"}`},
		{http.MethodPut, PathRoles + "/r1", `{"name":"x"}`},
	} {
		rec := f.do(t, tc.method, tc.path, tc.body, limited)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s 状态码 = %d, 期望 403, body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
			continue
		}
		code, message, _ := decodeResult[any](t, rec)
		if code != 403 || message != logic.MessageForbidden {
			t.Errorf("%s %s 响应 = code:%d message:%q", tc.method, tc.path, code, message)
		}
	}

	// 有权限的接口放行。
	rec := f.do(t, http.MethodGet, PathReaders, "", limited)
	if rec.Code != http.StatusOK {
		t.Fatalf("readers 列表状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestReadersListPageDataShape(t *testing.T) {
	f := newAdminAPIFixture(t)
	f.readers.users["u1"] = &domain.ReaderUser{ID: "u1", Username: "reader", Status: 1,
		CreatedAt: time.Date(2026, 9, 14, 1, 2, 3, 0, time.Local), UpdatedAt: time.Now()}
	f.readers.roles["u1"] = []string{"editor"}

	rec := f.do(t, http.MethodGet, PathReaders+"?pageIndex=1&pageSize=10&keyword=read", "", f.loginAs(t, "editor", "editor-pw"))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	code, message, data := decodeResult[map[string]any](t, rec)
	if code != 0 || message != "OK" {
		t.Fatalf("响应包装 = %d/%q", code, message)
	}
	for _, field := range []string{"total", "headNodeTotal", "pageIndex", "pageSize", "list"} {
		if _, ok := data[field]; !ok {
			t.Errorf("PageData 缺少字段 %s: %v", field, data)
		}
	}
	list, _ := data["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("list = %v", data["list"])
	}
	item, _ := list[0].(map[string]any)
	for _, field := range []string{"id", "username", "email", "avatar", "status", "muted", "ageLevel", "roles", "createdAt"} {
		if _, ok := item[field]; !ok {
			t.Errorf("UserVO 缺少字段 %s: %v", field, item)
		}
	}
}

func TestCreateReaderDuplicateIsBusinessFailure(t *testing.T) {
	f := newAdminAPIFixture(t)
	root := f.loginAs(t, "root", "root-pw")

	rec := f.do(t, http.MethodPost, PathReaders, `{"username":"reader","password":"pw","email":"r@acat.com","ageLevel":18}`, root)
	if rec.Code != http.StatusOK {
		t.Fatalf("创建状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	code, message, data := decodeResult[map[string]any](t, rec)
	if code != 0 || message != "OK" || data["id"] != "generated-id" || data["status"] != float64(1) {
		t.Fatalf("创建响应 = %d/%q/%v", code, message, data)
	}

	rec = f.do(t, http.MethodPost, PathReaders, `{"username":"reader","password":"pw"}`, root)
	if rec.Code != http.StatusOK {
		t.Fatalf("业务失败必须 HTTP 200，实际 %d", rec.Code)
	}
	code, message, data = decodeResult[map[string]any](t, rec)
	if code != 1 || message != "用户名已存在" || data != nil {
		t.Errorf("业务失败响应 = %d/%q/%v", code, message, data)
	}
}

func TestUpdateReaderNotFoundIsBusinessFailure(t *testing.T) {
	f := newAdminAPIFixture(t)
	rec := f.do(t, http.MethodPut, PathReaders+"/ghost", `{"username":"x"}`, f.loginAs(t, "root", "root-pw"))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	code, message, _ := decodeResult[any](t, rec)
	if code != 1 || message != "用户不存在" {
		t.Errorf("响应 = %d/%q", code, message)
	}
}

func TestReaderLifecycleEndpoints(t *testing.T) {
	f := newAdminAPIFixture(t)
	root := f.loginAs(t, "root", "root-pw")
	f.readers.users["u1"] = &domain.ReaderUser{ID: "u1", Username: "reader", Status: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	f.roles.roles["r1"] = &domain.Role{ID: "r1", Code: "editor", Name: "编辑", Status: 1}

	for _, tc := range []struct {
		name, method, path, body string
	}{
		{"改状态", http.MethodPatch, PathReaders + "/u1", `{"status":0}`},
		{"改禁言", http.MethodPatch, PathReaders + "/u1/mute", `{"muted":1}`},
		{"分配角色", http.MethodPut, PathReaders + "/u1/roles", `{"roleIds":["r1","ghost"]}`},
	} {
		rec := f.do(t, tc.method, tc.path, tc.body, root)
		code, message, data := decodeResult[any](t, rec)
		if rec.Code != http.StatusOK || code != 0 || message != "OK" || data != nil {
			t.Errorf("%s 响应 = http:%d code:%d message:%q data:%v", tc.name, rec.Code, code, message, data)
		}
	}
	if f.readers.users["u1"].Status != 0 || f.readers.users["u1"].Muted != 1 {
		t.Errorf("状态未更新: %+v", f.readers.users["u1"])
	}
	if got := f.readers.roles["u1"]; len(got) != 1 || got[0] != "editor" {
		t.Errorf("角色 = %v（不存在的 id 必须丢弃）", got)
	}

	rec := f.do(t, http.MethodDelete, PathReaders+"/u1", "", root)
	if code, _, _ := decodeResult[any](t, rec); rec.Code != http.StatusOK || code != 0 {
		t.Errorf("删除响应 = http:%d code:%d", rec.Code, code)
	}
	if _, ok := f.readers.users["u1"]; ok {
		t.Errorf("删除后用户仍存在")
	}
}

func TestWorkerEndpointsAndRootProtection(t *testing.T) {
	f := newAdminAPIFixture(t)
	root := f.loginAs(t, "root", "root-pw")
	f.roles.roles["r1"] = &domain.Role{ID: "r1", Code: "editor", Name: "编辑", Status: 1}

	rec := f.do(t, http.MethodPost, PathWorkers, `{"username":"worker","password":"pw","roleIds":["r1"]}`, root)
	if rec.Code != http.StatusOK {
		t.Fatalf("创建状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	code, message, data := decodeResult[map[string]any](t, rec)
	if code != 0 || message != "OK" || data["id"] != "generated-id" {
		t.Fatalf("创建响应 = %d/%q/%v", code, message, data)
	}
	roles, _ := data["roles"].([]any)
	if len(roles) != 1 {
		t.Errorf("roles = %v", data["roles"])
	}

	// root 不可被禁用/删除（当前登录为 root 时命中 root 保护；workerIsRoot 按角色判定）。
	f.adminWorker.workers[domain.RootLoginID] = &domain.AdminWorker{ID: domain.RootLoginID, Username: "root", Status: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	f.adminWorker.roles[domain.RootLoginID] = []string{domain.RootRoleID}
	rec = f.do(t, http.MethodPatch, PathWorkers+"/"+domain.RootLoginID, `{"status":0}`, root)
	if code, message, _ := decodeResult[any](t, rec); code != 1 || message != "不可禁用超级管理员" {
		t.Errorf("禁用 root 响应 = %d/%q", code, message)
	}
	rec = f.do(t, http.MethodDelete, PathWorkers+"/"+domain.RootLoginID, "", root)
	if code, message, _ := decodeResult[any](t, rec); code != 1 || message != "不可删除超级管理员" {
		t.Errorf("删除 root 响应 = %d/%q", code, message)
	}

	// 不存在的工作人员。
	rec = f.do(t, http.MethodPatch, PathWorkers+"/ghost", `{"status":0}`, root)
	if code, message, _ := decodeResult[any](t, rec); code != 1 || message != "工作人员不存在" {
		t.Errorf("不存在响应 = %d/%q", code, message)
	}
}

func TestWorkerListOrderedAndMapped(t *testing.T) {
	f := newAdminAPIFixture(t)
	root := f.loginAs(t, "root", "root-pw")
	now := time.Now()
	f.adminWorker.workers["0"] = &domain.AdminWorker{ID: "0", Username: "root", Status: 1, CreatedAt: now, UpdatedAt: now}
	f.adminWorker.workers["w1"] = &domain.AdminWorker{ID: "w1", Username: "worker", Status: 1, CreatedAt: now, UpdatedAt: now}
	f.adminWorker.roles["w1"] = []string{"editor"}
	f.roles.roles["r1"] = &domain.Role{ID: "r1", Code: "editor", Name: "编辑", Status: 1}

	rec := f.do(t, http.MethodGet, PathWorkers+"?pageIndex=1&pageSize=10", "", root)
	code, _, data := decodeResult[map[string]any](t, rec)
	if rec.Code != http.StatusOK || code != 0 {
		t.Fatalf("状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	list, _ := data["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("list = %v", data["list"])
	}
	first, _ := list[0].(map[string]any)
	if first["id"] != "0" {
		t.Errorf("必须按 id ASC 排序: %v", first["id"])
	}
	second, _ := list[1].(map[string]any)
	roles, _ := second["roles"].([]any)
	if len(roles) != 1 {
		t.Fatalf("worker roles = %v", second["roles"])
	}
	role, _ := roles[0].(map[string]any)
	if role["code"] != "editor" || role["permissionCount"] != nil {
		t.Errorf("WorkerRoleVO = %v", role)
	}
}

func TestRoleEndpointsVariants(t *testing.T) {
	f := newAdminAPIFixture(t)
	root := f.loginAs(t, "root", "root-pw")
	f.roles.roles["r1"] = &domain.Role{ID: "r1", Code: "editor", Name: "编辑", Status: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	f.roles.pages["page-1"] = true
	f.permissions.permissions["btn-1"] = &domain.AdminPermission{ID: "btn-1", Code: "c1", Name: "按钮", CreatedAt: time.Now()}

	// format=select
	rec := f.do(t, http.MethodGet, PathRoles+"?format=select", "", root)
	code, _, data := decodeResult[[]map[string]any](t, rec)
	if rec.Code != http.StatusOK || code != 0 || len(data) != 1 || data[0]["label"] != "编辑 (editor)" || data[0]["value"] != "r1" {
		t.Errorf("select 响应 = %d/%d/%v", rec.Code, code, data)
	}

	// all=true → 全量实体
	rec = f.do(t, http.MethodGet, PathRoles+"?all=true", "", root)
	code, _, raw := decodeResult[[]map[string]any](t, rec)
	if rec.Code != http.StatusOK || code != 0 || len(raw) != 1 || raw[0]["code"] != "editor" {
		t.Errorf("all 响应 = %d/%d/%v", rec.Code, code, raw)
	}
	if _, ok := raw[0]["createdAt"]; !ok {
		t.Errorf("AdminRoleEntity 缺少 createdAt: %v", raw[0])
	}

	// 默认分页
	rec = f.do(t, http.MethodGet, PathRoles+"?pageIndex=1&pageSize=5", "", root)
	code, _, page := decodeResult[map[string]any](t, rec)
	if rec.Code != http.StatusOK || code != 0 {
		t.Fatalf("分页响应 = %d/%d", rec.Code, code)
	}
	for _, field := range []string{"total", "headNodeTotal", "pageIndex", "pageSize", "list"} {
		if _, ok := page[field]; !ok {
			t.Errorf("PageData 缺少字段 %s: %v", field, page)
		}
	}

	// 权限 id 列表
	rec = f.do(t, http.MethodGet, PathRoles+"/r1/permissions", "", root)
	code, _, ids := decodeResult[[]string](t, rec)
	if rec.Code != http.StatusOK || code != 0 || len(ids) != 0 {
		t.Errorf("权限 id 响应 = %d/%d/%v（空列表必须为 []）", rec.Code, code, ids)
	}

	// 分配权限：page-1 走 resource_type=0，btn-1 走 1，ghost 丢弃
	rec = f.do(t, http.MethodPut, PathRoles+"/r1/permissions", `{"permissionIds":["page-1","btn-1","ghost"]}`, root)
	if code, message, _ := decodeResult[any](t, rec); rec.Code != http.StatusOK || code != 0 || message != "OK" {
		t.Errorf("分配权限响应 = %d/%d/%q", rec.Code, code, message)
	}

	// 角色不存在
	rec = f.do(t, http.MethodGet, PathRoles+"/ghost/permissions", "", root)
	if code, message, _ := decodeResult[any](t, rec); code != 1 || message != "角色不存在" {
		t.Errorf("不存在角色响应 = %d/%q", code, message)
	}

	// 创建 + 修改 + 状态 + 删除
	rec = f.do(t, http.MethodPost, PathRoles, `{"code":"viewer","name":"访客","i18nValue":[{"i18n":"zh-CN","value":"访客2"}]}`, root)
	if code, _, role := decodeResult[map[string]any](t, rec); code != 0 || role["name"] != "访客2" {
		t.Errorf("创建角色响应 = %d/%v", code, role)
	}
	rec = f.do(t, http.MethodPatch, PathRoles+"/r1", `{"status":2}`, root)
	if code, message, _ := decodeResult[any](t, rec); code != 1 || message != "状态值无效" {
		t.Errorf("非法状态响应 = %d/%q", code, message)
	}
	rec = f.do(t, http.MethodDelete, PathRoles+"/r1", "", root)
	if code, _, _ := decodeResult[any](t, rec); code != 0 {
		t.Errorf("删除角色响应 = %d", code)
	}
}

func TestPermissionEndpoints(t *testing.T) {
	f := newAdminAPIFixture(t)
	root := f.loginAs(t, "root", "root-pw")
	now := time.Now()
	f.permissions.permissions["p1"] = &domain.AdminPermission{ID: "p1", Code: "c1", Name: "字典", CreatedAt: now, UpdatedAt: now}
	f.permissions.deleted["p9"] = &domain.AdminPermission{ID: "p9", Code: "c2", Name: "旧", CreatedAt: now}

	// 无 format：实体列表
	rec := f.do(t, http.MethodGet, PathPermissions, "", root)
	code, _, list := decodeResult[[]map[string]any](t, rec)
	if rec.Code != http.StatusOK || code != 0 || len(list) != 1 {
		t.Fatalf("列表响应 = %d/%d/%v", rec.Code, code, list)
	}
	for _, field := range []string{"id", "code", "name", "pageId", "isDeleted", "createdAt", "updatedAt"} {
		if _, ok := list[0][field]; !ok {
			t.Errorf("AdminPermissionEntity 缺少字段 %s: %v", field, list[0])
		}
	}

	// format=tree：PermissionVO
	rec = f.do(t, http.MethodGet, PathPermissions+"?format=tree", "", root)
	code, _, tree := decodeResult[[]map[string]any](t, rec)
	if rec.Code != http.StatusOK || code != 0 || len(tree) != 1 || tree[0]["code"] != "c1" {
		t.Errorf("tree 响应 = %d/%d/%v", rec.Code, code, tree)
	}

	// 创建（重复 code → 业务失败）
	rec = f.do(t, http.MethodPost, PathPermissions, `{"code":"c1","name":"重复"}`, root)
	if code, message, _ := decodeResult[any](t, rec); code != 1 || message != "权限标识已存在" {
		t.Errorf("重复 code 响应 = %d/%q", code, message)
	}
	// 恢复已软删记录
	rec = f.do(t, http.MethodPost, PathPermissions, `{"code":"c2","name":"恢复","pageId":"page-9"}`, root)
	if code, _, restored := decodeResult[map[string]any](t, rec); code != 0 || restored["id"] != "p9" || restored["name"] != "恢复" {
		t.Errorf("恢复响应 = %d/%v", code, restored)
	}
	// 修改
	rec = f.do(t, http.MethodPut, PathPermissions+"/p1", `{"code":"c1","name":"字典2"}`, root)
	if code, _, updated := decodeResult[map[string]any](t, rec); code != 0 || updated["name"] != "字典2" {
		t.Errorf("修改响应 = %d/%v", code, updated)
	}
	// 删除
	rec = f.do(t, http.MethodDelete, PathPermissions+"/p1", "", root)
	if code, _, _ := decodeResult[any](t, rec); code != 0 {
		t.Errorf("删除响应 = %d", code)
	}
	rec = f.do(t, http.MethodDelete, PathPermissions+"/ghost", "", root)
	if code, message, _ := decodeResult[any](t, rec); code != 1 || message != "权限不存在" {
		t.Errorf("删除不存在响应 = %d/%q", code, message)
	}
}

func TestMalformedBodiesAreBadRequest(t *testing.T) {
	f := newAdminAPIFixture(t)
	root := f.loginAs(t, "root", "root-pw")
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, PathReaders, `{"username":`},
		{http.MethodPatch, PathReaders + "/u1", `{"status":`},
		{http.MethodPatch, PathReaders + "/u1/mute", `{"muted":`},
	} {
		rec := f.do(t, tc.method, tc.path, tc.body, root)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s 状态码 = %d, 期望 400", tc.method, tc.path, rec.Code)
		}
	}

	// 缺少 status 字段：侧为 NPE（500），Go 侧按请求格式错误返回 400。
	rec := f.do(t, http.MethodPatch, PathReaders+"/u1", `{}`, root)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("缺少 status 状态码 = %d, 期望 400", rec.Code)
	}
}
