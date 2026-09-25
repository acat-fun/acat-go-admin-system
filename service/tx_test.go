package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/repo"
	"47.108.230.93/acat-fun/acat-go-admin-system/storage"
	"github.com/acat-fun/acat-go-common/db"
)

// 本文件用真实的 db.Handle（sqlmock 支撑）验证 service 层的事务边界：
// 提交、回滚。repo 仍是内存假实现，断言的是边界而非 SQL。

type txEnv struct {
	svc     *Service
	mock    sqlmock.Sqlmock
	dicts   *fakeDictRepo
	pages   *fakePageRepo
	modules *fakeModuleRepo
	types   *fakeI18nTypeRepo
	files   *fakeFileRepo
	labels  *failingWriterLabelRepo
}

// failingWriterLabelRepo 包装假标签仓储，可按需让写失败。
type failingWriterLabelRepo struct {
	repo.LabelRepo
	fail bool
}

func (f *failingWriterLabelRepo) SaveNameLabels(ctx context.Context, sourceTable, tableDataID string,
	values []domain.I18nValue) error {
	if f.fail {
		return errors.New("标签写入失败")
	}
	return f.LabelRepo.SaveNameLabels(ctx, sourceTable, tableDataID, values)
}

func newTxEnv(t *testing.T) *txEnv {
	t.Helper()
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })

	dicts := newFakeDictRepo()
	labels := &failingWriterLabelRepo{LabelRepo: dicts}
	env := &txEnv{
		mock:    mock,
		dicts:   dicts,
		pages:   newFakePageRepo(),
		modules: newFakeModuleRepo(),
		types:   newFakeI18nTypeRepo(),
		files:   newFakeFileRepo(),
		labels:  labels,
	}
	svc, err := New(Options{
		Dicts:     dicts,
		Labels:    labels,
		Pages:     env.pages,
		Modules:   env.modules,
		I18nTypes: env.types,
		Files:     env.files,
		Audits:    repo.NewMemoryAuditLogStore(),
		Objects:   storage.NewMemory("acat-local"),
		Tx:        db.NewHandle(raw),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		NewID:     func() string { return "generated-id" },
		Now:       func() time.Time { return time.Date(2026, 9, 15, 10, 0, 0, 0, time.Local) },
	})
	if err != nil {
		t.Fatalf("构造 Service 失败: %v", err)
	}
	env.svc = svc
	return env
}

func txRequestContext() RequestContext { return RequestContext{} }

// TestDeleteDictCommitsSingleTransaction 级联删除：多次写一次事务提交。
func TestDeleteDictCommitsSingleTransaction(t *testing.T) {
	env := newTxEnv(t)
	env.dicts.dicts["d1"] = domain.DictRecord{ID: "d1", Code: "book_tag", Name: "书籍标签"}
	env.dicts.data["i1"] = domain.DictDataRecord{ID: "i1", DictID: "d1"}
	env.mock.ExpectBegin()
	env.mock.ExpectCommit()

	if err := env.svc.DeleteDict(context.Background(), "d1"); err != nil {
		t.Fatalf("删除字典应成功: %v", err)
	}
	if err := env.mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("应提交事务: %v", err)
	}
}

// TestDeletePageCommitsSingleTransaction 页面递归删除：整树 + 标签 + 角色权限一次事务提交。
func TestDeletePageCommitsSingleTransaction(t *testing.T) {
	env := newTxEnv(t)
	env.pages.pages["p1"] = domain.PageRecord{ID: "p1", Name: "首页"}
	env.mock.ExpectBegin()
	env.mock.ExpectCommit()

	if err := env.svc.DeletePage(context.Background(), txRequestContext(), "p1"); err != nil {
		t.Fatalf("删除页面应成功: %v", err)
	}
	if err := env.mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("应提交事务: %v", err)
	}
}

// TestCreateDictRollsBackWhenLabelsFail 标签写入失败 → 字典主记录一并回滚。
func TestCreateDictRollsBackWhenLabelsFail(t *testing.T) {
	env := newTxEnv(t)
	env.labels.fail = true
	env.mock.ExpectBegin()
	env.mock.ExpectRollback()

	_, err := env.svc.CreateDict(context.Background(), txRequestContext(), domain.DictSavePayload{
		Code: strPtr("new_code"), Name: strPtr("新字典"),
	})
	if err == nil {
		t.Fatal("标签写入失败时应返回错误")
	}
	if err := env.mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("应回滚事务: %v", err)
	}
}

// TestCreateDictCommitsSingleTransaction 成功路径：一次事务提交。
func TestCreateDictCommitsSingleTransaction(t *testing.T) {
	env := newTxEnv(t)
	env.mock.ExpectBegin()
	env.mock.ExpectCommit()

	if _, err := env.svc.CreateDict(context.Background(), txRequestContext(), domain.DictSavePayload{
		Code: strPtr("new_code"), Name: strPtr("新字典"),
	}); err != nil {
		t.Fatalf("创建字典应成功: %v", err)
	}
	if err := env.mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("应提交事务: %v", err)
	}
}

// TestBatchSaveDataItemsRollsBackOnLabelFailure 批量保存：任一步失败整批回滚。
func TestBatchSaveDataItemsRollsBackOnLabelFailure(t *testing.T) {
	env := newTxEnv(t)
	env.dicts.dicts["d1"] = domain.DictRecord{ID: "d1", Code: "book_tag", Name: "书籍标签"}
	env.labels.fail = true
	env.mock.ExpectBegin()
	env.mock.ExpectRollback()

	payloads := []domain.DictDataSavePayload{{Code: strPtr("c1"), Name: strPtr("项一"), Value: strPtr("v1")}}
	if err := env.svc.BatchSaveDataItems(context.Background(), txRequestContext(), "d1", payloads); err == nil {
		t.Fatal("标签写入失败时应返回错误")
	}
	if err := env.mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("应回滚事务: %v", err)
	}
}

// TestBatchSaveDataItemsCommitsSingleTransaction 批量保存成功路径：一次事务提交。
func TestBatchSaveDataItemsCommitsSingleTransaction(t *testing.T) {
	env := newTxEnv(t)
	env.dicts.dicts["d1"] = domain.DictRecord{ID: "d1", Code: "book_tag", Name: "书籍标签"}
	env.mock.ExpectBegin()
	env.mock.ExpectCommit()

	payloads := []domain.DictDataSavePayload{{Code: strPtr("c1"), Name: strPtr("项一"), Value: strPtr("v1")}}
	if err := env.svc.BatchSaveDataItems(context.Background(), txRequestContext(), "d1", payloads); err != nil {
		t.Fatalf("批量保存应成功: %v", err)
	}
	if err := env.mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("应提交事务: %v", err)
	}
}
