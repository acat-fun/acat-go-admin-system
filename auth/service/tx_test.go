package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
	"github.com/acat-fun/acat-go-admin-system/logic"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/db"
	"github.com/acat-fun/acat-go-common/satoken"
)

// 本文件用真实的 db.Handle（sqlmock 支撑）验证 service 层的事务边界与写入语义分级：
// 提交、回滚、并发冲突（要求影响行数）。repo 仍是内存假实现，断言的是边界而非 SQL。

// zeroUpdateRoleRepo 让角色状态更新影响 0 行（模拟并发下角色已被删除或改动）。
type zeroUpdateRoleRepo struct{ *fakeRoleRepo }

func (z zeroUpdateRoleRepo) Update(context.Context, *domain.Role) (int64, error) { return 0, nil }

// failingResetReaderRepo 让「先软删再插入」的第一步失败，用于验证整体回滚。
type failingResetReaderRepo struct{ *fakeReaderRepo }

func (f failingResetReaderRepo) SoftDeleteRoles(context.Context, string) (int64, error) {
	return 0, errors.New("数据库不可用")
}

type txServices struct {
	svc         *Service
	mock        sqlmock.Sqlmock
	roles       *fakeRoleRepo
	readers     *fakeReaderRepo
	workers     *fakeAdminWorkerRepo
	permissions *fakePermissionRepo
}

func newAdminTxService(t *testing.T, opts func(*txServices)) *txServices {
	t.Helper()
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })

	roles := newFakeRoleRepo()
	readers := newFakeReaderRepo()
	workers := newFakeAdminWorkerRepo()
	permissions := newFakePermissionRepo()
	resolve := func(id string) string {
		if role, ok := roles.roles[id]; ok {
			return role.Code
		}
		return id
	}
	readers.resolveCode = resolve
	workers.resolveCode = resolve

	svc := &Service{
		workers: newFakeWorkerRepo(),
		pages:   &fakePageRepo{},
		modules: &fakeModuleRepo{},
		satoken: satoken.NewLogic(satoken.Config{TokenName: DefaultTokenNameForTest, Timeout: 3600,
			Now: func() time.Time { return time.Unix(1730000000, 0) }}, satoken.NewMemoryStore(), nil),
		readerRepo:      readers,
		adminWorkerRepo: workers,
		roleRepo:        roles,
		permissionRepo:  permissions,
		tx:              db.NewHandle(raw),
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		newID:           func() string { return "generated-id" },
	}
	fixture := &txServices{svc: svc, mock: mock, roles: roles, readers: readers, workers: workers, permissions: permissions}
	if opts != nil {
		opts(fixture)
	}
	return fixture
}

// TestDeleteRoleCommitsSingleTransaction 删除角色：三条写一次事务提交。
func TestDeleteRoleCommitsSingleTransaction(t *testing.T) {
	fixture := newAdminTxService(t, nil)
	fixture.roles.roles["r1"] = &domain.Role{ID: "r1", Code: "editor", Name: "编辑"}
	fixture.mock.ExpectBegin()
	fixture.mock.ExpectCommit()

	if err := fixture.svc.DeleteRole(context.Background(), rootActor(), "r1"); err != nil {
		t.Fatalf("删除角色应成功: %v", err)
	}
	if len(fixture.roles.softDeleted) != 1 || len(fixture.roles.userRelDeleted) != 1 ||
		len(fixture.roles.permReset) != 1 {
		t.Fatalf("应写三处: role=%v userRel=%v perm=%v",
			fixture.roles.softDeleted, fixture.roles.userRelDeleted, fixture.roles.permReset)
	}
	if err := fixture.mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("应提交事务: %v", err)
	}
}

// TestUpdateRoleStatusReturnsTargetMissingOnZeroRows 并发改动角色：
// 状态更新影响 0 行 → 「角色不存在」+ 40401。
func TestUpdateRoleStatusReturnsTargetMissingOnZeroRows(t *testing.T) {
	fixture := newAdminTxService(t, nil)
	roles := newFakeRoleRepo()
	roles.roles["r1"] = &domain.Role{ID: "r1", Code: "editor"}
	fixture.svc.roleRepo = zeroUpdateRoleRepo{roles}

	status := 0
	err := fixture.svc.UpdateRoleStatus(context.Background(), "r1", &status)
	business, ok := apperr.IsBusiness(err)
	if !ok {
		t.Fatalf("并发冲突应为业务失败: %v", err)
	}
	if business.Code != apperr.CodeTargetMissing || business.Message != MessageRoleNotExist {
		t.Fatalf("应为 40401/「%s」，实际 %d/「%s」", MessageRoleNotExist, business.Code, business.Message)
	}
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("冲突哨兵应保留在错误链中: %v", err)
	}
}

// TestAssignReaderRolesRollsBackOnFailure 「先软删再插入」失败时整体回滚。
func TestAssignReaderRolesRollsBackOnFailure(t *testing.T) {
	fixture := newAdminTxService(t, nil)
	fixture.roles.roles["r1"] = &domain.Role{ID: "r1", Code: "editor"}
	fixture.svc.readerRepo = failingResetReaderRepo{fixture.readers}
	fixture.mock.ExpectBegin()
	fixture.mock.ExpectRollback()

	if err := fixture.svc.AssignReaderRoles(context.Background(), "u1", []string{"r1"}); err == nil {
		t.Fatal("依赖失败时应返回错误")
	}
	if err := fixture.mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("应回滚事务: %v", err)
	}
}

// TestAssignReaderRolesCommitsSingleTransaction 读者角色分配：先删后插一次事务提交。
func TestAssignReaderRolesCommitsSingleTransaction(t *testing.T) {
	fixture := newAdminTxService(t, nil)
	fixture.roles.roles["r1"] = &domain.Role{ID: "r1", Code: "reader"}
	fixture.mock.ExpectBegin()
	fixture.mock.ExpectCommit()

	if err := fixture.svc.AssignReaderRoles(context.Background(), "u1", []string{"r1"}); err != nil {
		t.Fatalf("分配角色应成功: %v", err)
	}
	if len(fixture.readers.roleInserted["u1"]) != 1 {
		t.Fatalf("应写入一次角色关联，实际 %v", fixture.readers.roleInserted)
	}
	if err := fixture.mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("应提交事务: %v", err)
	}
}

// TestDeleteWorkerCommitsSingleTransaction 删除工作人员：软删 + 角色关联一次事务提交。
func TestDeleteWorkerCommitsSingleTransaction(t *testing.T) {
	fixture := newAdminTxService(t, nil)
	fixture.workers.workers["w1"] = &domain.AdminWorker{ID: "w1", Username: "worker"}
	fixture.mock.ExpectBegin()
	fixture.mock.ExpectCommit()

	if err := fixture.svc.DeleteWorker(context.Background(), rootActor(), "w1"); err != nil {
		t.Fatalf("删除工作人员应成功: %v", err)
	}
	if err := fixture.mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("应提交事务: %v", err)
	}
}

// rootActor 构造超级管理员操作者（会话角色含 root，权限判定直接放行）。
func rootActor() *logic.Actor {
	session := &satoken.Session{LoginID: domain.RootLoginID}
	session.Set(satoken.DataKeyRoles, []string{domain.RootRoleID})
	return &logic.Actor{LoginID: domain.RootLoginID, Session: session}
}
