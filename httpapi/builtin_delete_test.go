package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/acat-fun/acat-go-admin-system/domain"
	"github.com/acat-fun/acat-go-admin-system/repo"
	"github.com/acat-fun/acat-go-admin-system/service"
	"github.com/acat-fun/acat-go-admin-system/storage"
	"github.com/acat-fun/acat-go-common/config"
	"github.com/acat-fun/acat-go-common/satoken"
)

// 本文件验证删除内置行经共享 apperr 业务失败路径返回：HTTP 200 + code≠0 + 中文文案。

// builtinPageRepo 只覆写内置页面判定所需的方法。
type builtinPageRepo struct {
	repo.PageRepo
	record domain.PageRecord
}

func (r *builtinPageRepo) FindPageByID(context.Context, string) (*domain.PageRecord, error) {
	record := r.record
	return &record, nil
}

// builtinDictRepo 只覆写内置字典与内置数据项判定所需的方法。
type builtinDictRepo struct {
	repo.DictRepo
	repo.LabelRepo
	dict domain.DictRecord
	data domain.DictDataRecord
}

func (r *builtinDictRepo) FindDictByID(context.Context, string) (*domain.DictRecord, error) {
	record := r.dict
	return &record, nil
}

func (r *builtinDictRepo) FindDataItemByID(context.Context, string) (*domain.DictDataRecord, error) {
	record := r.data
	return &record, nil
}

// builtinTypeRepo 只覆写内置语言类型判定所需的方法。
type builtinTypeRepo struct {
	repo.I18nTypeRepo
	record domain.I18nTypeRecord
}

func (r *builtinTypeRepo) FindTypeByID(context.Context, string) (*domain.I18nTypeRecord, error) {
	record := r.record
	return &record, nil
}

// newBuiltinDeleteServer 构造各目录的删除目标均为内置行的测试服务。
func newBuiltinDeleteServer(t *testing.T) *testServer {
	t.Helper()
	logic := satoken.NewLogic(satoken.Config{
		TokenName:  "acat-admin-token",
		LoginType:  "login",
		Timeout:    satoken.DefaultTimeoutSeconds,
		IsShare:    true,
		TokenStyle: "uuid",
	}, satoken.NewMemoryStore(), nil)

	dicts := &builtinDictRepo{
		dict: domain.DictRecord{ID: "d1", Code: "book_tag", Name: "书籍标签", IsEnabled: 1, IsBuiltin: 1},
		data: domain.DictDataRecord{
			ID: "i1", DictID: "d1", Code: "enabled", Name: "启用", Value: "enabled",
			IsEnabled: 1, IsBuiltin: 1,
		},
	}
	svc, err := service.New(service.Options{
		Tx:        passthroughTx{},
		Dicts:     dicts,
		Labels:    dicts,
		Pages:     &builtinPageRepo{record: domain.PageRecord{ID: "p1", Code: "page-servers", Name: "服务器", IsEnabled: 1, IsBuiltin: 1}},
		Modules:   &stubModuleRepo{},
		I18nTypes: &builtinTypeRepo{record: domain.I18nTypeRecord{ID: "t1", Code: "zh-CN", Name: "中文", IsEnabled: 1, IsBuiltin: 1}},
		Files:     &stubFileRepo{},
		Audits:    repo.NewMemoryAuditLogStore(),
		Objects:   storage.NewMemory("acat-local"),
	})
	if err != nil {
		t.Fatalf("构造 Service 失败: %v", err)
	}
	api, err := New(Options{
		Service: svc,
		Satoken: logic,
		Config:  config.SaTokenConfig{CookieName: "acat-admin-token", CookiePath: "/"},
	})
	if err != nil {
		t.Fatalf("构造 API 失败: %v", err)
	}
	mux := http.NewServeMux()
	api.Register(mux)
	return &testServer{handler: mux, logic: logic, api: api, svc: svc}
}

func TestDeleteBuiltinRowsReturnBusinessFailure(t *testing.T) {
	server := newBuiltinDeleteServer(t)
	token := server.login(t, domain.RootLoginID, nil)

	cases := []struct {
		name    string
		method  string
		path    string
		message string
	}{
		{"页面", http.MethodDelete, PathPages + "/p1", service.MessagePageBuiltinUndeletable},
		{"字典", http.MethodDelete, PathDicts + "/d1", service.MessageDictBuiltinUndeletable},
		{"字典项", http.MethodDelete, "/api/admin/system/dicts/d1/data/i1", service.MessageDictDataBuiltinUndeletable},
		{"语言类型", http.MethodDelete, PathI18nTypes + "/t1", service.MessageI18nTypeBuiltinUndeletable},
	}
	for _, item := range cases {
		recorder := server.do(t, item.method, item.path, token, nil)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s：业务失败应返回 200，实际 %d (%s)", item.name, recorder.Code, recorder.Body.String())
		}
		code, message, _ := decodeResult(t, recorder)
		if code == 0 {
			t.Fatalf("%s：业务失败 code 不应为 0（%s）", item.name, recorder.Body.String())
		}
		if message != item.message {
			t.Fatalf("%s：文案 = %q，期望 %q", item.name, message, item.message)
		}
	}
}
