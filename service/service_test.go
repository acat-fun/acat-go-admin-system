package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/logic"
	"47.108.230.93/acat-fun/acat-go-admin-system/repo"
	"47.108.230.93/acat-fun/acat-go-admin-system/storage"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/result"
	"github.com/acat-fun/acat-go-common/satoken"
)

// passthroughTx 让不关心事务的测试直接执行用例函数；真实提交/回滚由 tx_test.go 覆盖。
type passthroughTx struct{}

func (passthroughTx) Within(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// testEnv 汇总一次单测所需的 Service 与各假实现。
type testEnv struct {
	svc     *Service
	dicts   *fakeDictRepo
	pages   *fakePageRepo
	modules *fakeModuleRepo
	types   *fakeI18nTypeRepo
	files   *fakeFileRepo
	audits  *repo.MemoryAuditLogStore
	objects *storage.Memory
	ctx     context.Context
	rc      RequestContext
	// ids 依次返回，保证断言可预测。
	nextIDs []string
}

var fixedNow = time.Date(2026, 9, 14, 1, 2, 3, 0, time.Local)

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWith(t, nil)
}

// newTestEnvWith 在默认测试装配上追加选项覆盖（用于验证可配置项）。
func newTestEnvWith(t *testing.T, mutate func(*Options)) *testEnv {
	t.Helper()
	dicts := newFakeDictRepo()
	pages := newFakePageRepo()
	modules := newFakeModuleRepo()
	types := newFakeI18nTypeRepo()
	files := newFakeFileRepo()
	audits := repo.NewMemoryAuditLogStore()
	objects := storage.NewMemory("acat-local")

	env := &testEnv{
		dicts: dicts, pages: pages, modules: modules, types: types, files: files,
		audits: audits, objects: objects, ctx: context.Background(),
	}
	env.rc = RequestContext{I18nCode: "zh-CN"}
	env.nextIDs = []string{
		"019f0000-0000-7000-8000-000000000001",
		"019f0000-0000-7000-8000-000000000002",
		"019f0000-0000-7000-8000-000000000003",
		"019f0000-0000-7000-8000-000000000004",
	}
	index := 0
	options := Options{
		Dicts:     dicts,
		Labels:    dicts,
		Pages:     pages,
		Modules:   modules,
		I18nTypes: types,
		Files:     files,
		Audits:    audits,
		Objects:   objects,
		I18nCode:  "zh-CN",
		Tx:        passthroughTx{},
		NewID: func() string {
			if index < len(env.nextIDs) {
				id := env.nextIDs[index]
				index++
				return id
			}
			index++
			return domain.NewID()
		},
		Now: fixedClock(fixedNow),
	}
	if mutate != nil {
		mutate(&options)
	}
	svc, err := New(options)
	if err != nil {
		t.Fatalf("构造 Service 失败: %v", err)
	}
	env.svc = svc
	return env
}

// rootRC 构造超管操作者（会话角色含 root，与生产链路 buildBootstrap 写入的快照一致）。
func rootRC() RequestContext {
	session := satoken.NewSession("session-root")
	session.LoginID = domain.RootLoginID
	session.Set(satoken.DataKeyRoles, []string{domain.RootRoleID})
	ctx := middleware.WithSession(context.Background(), session, "token-root")
	return RequestContext{I18nCode: "zh-CN", Actor: logic.ActorFrom(ctx, nil)}
}

// workerRC 构造普通操作者（模拟 worker 会话 + 会话权限码快照）。
//
// 通过 middleware.WithSession + logic.ActorFrom 构造，保证与生产链路一致
// （logic.Actor 的 checker 字段不可导出，单测只依赖登录 id 与 Session）。
func workerRC(loginID string, permissions ...string) RequestContext {
	session := satoken.NewSession("session-" + loginID)
	session.LoginID = loginID
	session.Set(satoken.DataKeyPermissions, permissions)
	ctx := middleware.WithSession(context.Background(), session, "token-"+loginID)
	return RequestContext{I18nCode: "zh-CN", Actor: logic.ActorFrom(ctx, nil)}
}

// assertBusiness 断言错误是业务失败，并返回其业务码。
func assertBusiness(t *testing.T, err error, wantCode int, wantMessagePart string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望业务失败，实际 nil")
	}
	business, ok := apperr.IsBusiness(err)
	if !ok {
		t.Fatalf("期望业务失败，实际 %T: %v", err, err)
	}
	if business.Code != wantCode {
		t.Fatalf("业务码 = %d，期望 %d（消息 %q）", business.Code, wantCode, business.Message)
	}
	if wantMessagePart != "" && !strings.Contains(business.Message, wantMessagePart) {
		t.Fatalf("业务消息 = %q，期望包含 %q", business.Message, wantMessagePart)
	}
}

func mustPageData(t *testing.T, value any) result.PageData[any] {
	t.Helper()
	page, ok := value.(result.PageData[any])
	if !ok {
		t.Fatalf("期望 PageData，实际 %T", value)
	}
	return page
}

func TestServiceRequiresCoreDependencies(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("缺少必备依赖时应报错")
	}
	if _, err := New(Options{
		Dicts: newFakeDictRepo(), Labels: newFakeDictRepo(),
		Pages: newFakePageRepo(), Modules: newFakeModuleRepo(),
	}); err == nil {
		t.Fatal("缺少 I18nTypes 时应报错")
	}
}

func TestRequestContextDefaults(t *testing.T) {
	if got := (RequestContext{}).Language(); got != domain.DefaultI18nCode {
		t.Fatalf("默认语言 = %q", got)
	}
	if got := (RequestContext{I18nCode: "en"}).Language(); got != "en" {
		t.Fatalf("语言 = %q", got)
	}
	if got := (RequestContext{}).LoginID(); got != "" {
		t.Fatalf("无登录态应为空串，实际 %q", got)
	}
	rc := workerRC("u1", "acat:admin:system:dicts")
	if rc.IsRoot() {
		t.Fatal("非 0 登录 id 不应判为 root")
	}
	if rc.LoginID() != "u1" {
		t.Fatalf("登录 id = %q", rc.LoginID())
	}
}

func TestResolveDefaultNamePrefersZhCN(t *testing.T) {
	fallback := "原名"
	values := []domain.I18nValue{{I18n: "en", Value: "Name"}, {I18n: "zh-CN", Value: "中文名"}}
	if got := resolveDefaultNameOrNil(&fallback, values); got == nil || *got != "中文名" {
		t.Fatalf("应优先 zh-CN，实际 %v", got)
	}
	blank := []domain.I18nValue{{I18n: "zh-CN", Value: "   "}}
	if got := resolveDefaultNameOrNil(&fallback, blank); got == nil || *got != "原名" {
		t.Fatalf("zh-CN 为空白时应回退，实际 %v", got)
	}
	if got := resolveDefaultNameOrNil(nil, nil); got != nil {
		t.Fatalf("均缺失应返回 nil，实际 %v", got)
	}
}

func TestPaginateHelper(t *testing.T) {
	list := []int{1, 2, 3, 4, 5}
	if got := paginate(list, 2, 2); len(got) != 2 || got[0] != 3 {
		t.Fatalf("分页异常: %v", got)
	}
	if got := paginate(list, 0, 2); len(got) != 2 || got[0] != 1 {
		t.Fatalf("页码 <1 应回退首页: %v", got)
	}
	if got := paginate(list, 9, 2); len(got) != 0 {
		t.Fatalf("越界页应为空: %v", got)
	}
}

func TestUnavailableDependencyIsNotBusinessError(t *testing.T) {
	err := errors.New("db down")
	if _, ok := apperr.IsBusiness(err); ok {
		t.Fatal("普通错误不应被当作业务失败")
	}
}
