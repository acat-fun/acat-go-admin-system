package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/acat-fun/acat-go-admin-system/domain"
	"github.com/acat-fun/acat-go-admin-system/storage"
	"github.com/acat-fun/acat-go-common/result"
)

func addPage(env *testEnv, record domain.PageRecord) {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = fixedNow
		record.UpdatedAt = fixedNow
	}
	env.pages.addPage(record)
}

// TestListPagesIncludesAuditTimestamps 锁定 PageVO 的 createdAt/updatedAt 契约：
// 页面节点必须带 ISO "yyyy-MM-ddTHH:mm:ss" 时间。
func TestListPagesIncludesAuditTimestamps(t *testing.T) {
	env := newTestEnv(t)
	addPage(env, domain.PageRecord{
		ID: "p1", Code: "acat:admin:system", Name: "系统管理", Type: domain.PageTypeNav,
		Scope: 0, IsEnabled: 1, FrontendModuleCode: strPtr("shell"), SortOrder: 1,
	})

	tree, err := env.svc.ListPages(env.ctx, rootRC())
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("页面数 = %d", len(tree))
	}
	node := tree[0]
	if node.CreatedAt == nil || *node.CreatedAt != "2026-09-14T01:02:03" {
		t.Errorf("createdAt = %v, 期望 2026-09-14T01:02:03", node.CreatedAt)
	}
	if node.UpdatedAt == nil || *node.UpdatedAt != "2026-09-14T01:02:03" {
		t.Errorf("updatedAt = %v, 期望 2026-09-14T01:02:03", node.UpdatedAt)
	}

	raw, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	body := string(raw)
	for _, want := range []string{`"createdAt":"2026-09-14T01:02:03"`, `"updatedAt":"2026-09-14T01:02:03"`} {
		if !strings.Contains(body, want) {
			t.Errorf("页面 JSON 缺少 %s: %s", want, body)
		}
	}
}

func TestListPagesBuildsTreeAndDropsDisabledModules(t *testing.T) {
	env := newTestEnv(t)
	addModule(env, "m1", "system", "系统管理", "1.0.0", domain.FrontendModuleStatusEnabled, 1, 0)
	addPage(env, domain.PageRecord{
		ID: "p1", Code: "acat:admin:system", Name: "系统管理", Type: domain.PageTypeNav,
		Scope: 0, IsEnabled: 1, FrontendModuleCode: strPtr("system"), SortOrder: 1,
	})
	addPage(env, domain.PageRecord{
		ID: "p2", Code: "acat:admin:system:dicts", Name: "字典管理", Type: domain.PageTypePage,
		ParentID: strPtr("p1"), Scope: 0, IsEnabled: 1, FrontendModuleCode: strPtr("system"), SortOrder: 2,
	})
	// 引用未启用模块的页面应被过滤掉。
	addPage(env, domain.PageRecord{
		ID: "p3", Code: "acat:read:admin:cat-read:books", Name: "书籍管理", Type: domain.PageTypePage,
		Scope: 0, IsEnabled: 1, FrontendModuleCode: strPtr("content"), SortOrder: 3,
	})
	// shell 页面永远保留。
	addPage(env, domain.PageRecord{
		ID: "p4", Code: "acat:read:admin:dashboard", Name: "仪表盘", Type: domain.PageTypeNav,
		Scope: 0, IsEnabled: 1, FrontendModuleCode: strPtr("shell"), SortOrder: 0,
	})

	tree, err := env.svc.ListPages(env.ctx, rootRC())
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	ids := map[string]bool{}
	for _, node := range tree {
		ids[node.ID] = true
	}
	if ids["p3"] {
		t.Fatalf("未启用模块的页面应被过滤: %+v", tree)
	}
	if !ids["p1"] || !ids["p4"] {
		t.Fatalf("根节点缺失: %+v", ids)
	}
	for _, node := range tree {
		if node.ID == "p1" {
			// type=2 才不挂子节点；nav 应挂载子节点。
			if len(node.Children) != 1 || node.Children[0].ID != "p2" {
				t.Fatalf("子节点异常: %+v", node.Children)
			}
		}
		if node.ID == "p4" && node.Children == nil {
			t.Fatal("无子节点的 nav 应输出空数组而不是 null")
		}
	}
}

func TestPageTreePageTypeDropsChildren(t *testing.T) {
	vos := []domain.PageVO{
		{ID: "p1", Type: intPtr(domain.PageTypePage)},
		{ID: "p2", Type: intPtr(domain.PageTypePage), ParentID: strPtr("p1")},
	}
	tree := domain.BuildPageTree(vos)
	if len(tree) != 1 || tree[0].Children != nil {
		t.Fatalf("type=2 节点不应挂载子节点: %+v", tree)
	}
}

func TestListAccessiblePagesForRootEqualsListPages(t *testing.T) {
	env := newTestEnv(t)
	addPage(env, domain.PageRecord{
		ID: "p1", Code: "acat:admin:system", Name: "系统管理", Type: domain.PageTypeNav,
		Scope: 0, IsEnabled: 1, SortOrder: 1,
	})
	rootTree, err := env.svc.ListPages(env.ctx, rootRC())
	if err != nil {
		t.Fatalf("root 查询失败: %v", err)
	}
	root := rootRC()
	root.Actor = rootRC().Actor
	accessible, err := env.svc.ListAccessiblePages(env.ctx, root)
	if err != nil {
		t.Fatalf("mine=true 查询失败: %v", err)
	}
	if len(accessible) != len(rootTree) || accessible[0].ID != rootTree[0].ID {
		t.Fatalf("root 的 mine=true 应与 listAll 等价: %+v vs %+v", accessible, rootTree)
	}
}

func TestListAccessiblePagesFiltersByPermissionsAndKeepsAncestors(t *testing.T) {
	env := newTestEnv(t)
	addPage(env, domain.PageRecord{
		ID: "nav", Code: "acat:admin:system", Name: "系统管理", Type: domain.PageTypeNav,
		Scope: 0, IsEnabled: 1, SortOrder: 1,
	})
	addPage(env, domain.PageRecord{
		ID: "dict", Code: "acat:admin:system:dicts", Name: "字典管理", Type: domain.PageTypePage,
		ParentID: strPtr("nav"), Scope: 0, IsEnabled: 1, SortOrder: 2,
	})
	addPage(env, domain.PageRecord{
		ID: "pages", Code: "acat:admin:system:pages", Name: "页面管理", Type: domain.PageTypePage,
		ParentID: strPtr("nav"), Scope: 0, IsEnabled: 1, SortOrder: 3,
	})

	// 只有 dicts 权限：应保留 dicts 并自动补上父级 nav，pages 被过滤。
	tree, err := env.svc.ListAccessiblePages(env.ctx, workerRC("u1", "acat:admin:system:dicts"))
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(tree) != 1 || tree[0].ID != "nav" || len(tree[0].Children) != 1 || tree[0].Children[0].ID != "dict" {
		t.Fatalf("权限过滤/祖先补齐异常: %+v", tree)
	}

	// 无任何权限：返回空数组。
	empty, err := env.svc.ListAccessiblePages(env.ctx, workerRC("u2"))
	if err != nil || len(empty) != 0 {
		t.Fatalf("无权限应返回空数组: %v %v", empty, err)
	}
}

func TestListEnabledFrontendModuleCodes(t *testing.T) {
	env := newTestEnv(t)
	addModule(env, "m1", "system", "系统管理", "1.0.0", domain.FrontendModuleStatusEnabled, 1, 0)
	addModule(env, "m2", "content", "内容", "1.0.0", domain.FrontendModuleStatusDisabled, 2, 0)

	codes, err := env.svc.ListEnabledFrontendModuleCodes(env.ctx)
	if err != nil || len(codes) != 1 || codes[0] != "system" {
		t.Fatalf("启用模块码异常: %v %v", codes, err)
	}
}

func TestCreatePageGeneratesCodeFromPath(t *testing.T) {
	env := newTestEnv(t)
	entity, err := env.svc.CreatePage(env.ctx, rootRC(), domain.PageSavePayload{
		Name: strPtr("字典管理"), Path: strPtr("/admin/system/dicts"),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if entity.Code != "acat:admin:system:dicts" {
		t.Fatalf("权限码应由 path 派生，实际 %q", entity.Code)
	}
	if entity.Scope == nil || *entity.Scope != 0 || entity.Type == nil || *entity.Type != domain.PageTypePage {
		t.Fatalf("默认值异常: %+v", entity)
	}
	if len(env.pages.grants) != 1 || env.pages.grants[0] != entity.ID {
		t.Fatalf("应授予 root/admin 页面权限: %v", env.pages.grants)
	}
	if len(env.pages.history) != 1 {
		t.Fatalf("应写入一条页面历史: %v", env.pages.history)
	}
}

func TestCreatePageValidations(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.svc.CreatePage(env.ctx, rootRC(), domain.PageSavePayload{Name: strPtr("空路径")})
	assertBusiness(t, err, 1, MessagePagePathRequired)

	_, err = env.svc.CreatePage(env.ctx, rootRC(), domain.PageSavePayload{
		Name: strPtr("子页面"), Path: strPtr("/admin/system/dicts"), ParentID: strPtr("missing"),
	})
	assertBusiness(t, err, 1, MessagePageParentNotFound)

	addPage(env, domain.PageRecord{ID: "p1", Code: "acat:admin:system:dicts", Name: "字典", Type: domain.PageTypePage, Scope: 0, IsEnabled: 1})
	_, err = env.svc.CreatePage(env.ctx, rootRC(), domain.PageSavePayload{
		Name: strPtr("重复"), Path: strPtr("/admin/system/dicts"),
	})
	assertBusiness(t, err, 1, "页面标识 'acat:admin:system:dicts' 已存在")
}

func TestCreatePageValidatesRemoteRouteKeyAndModule(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.svc.CreatePage(env.ctx, rootRC(), domain.PageSavePayload{
		Name: strPtr("远程页"), Path: strPtr("/admin/system/remote"),
		FrontendModuleCode: strPtr("system"), RouteKey: strPtr("Bad.Key"),
	})
	assertBusiness(t, err, 1, MessagePageRouteKeyInvalid)

	_, err = env.svc.CreatePage(env.ctx, rootRC(), domain.PageSavePayload{
		Name: strPtr("远程页"), Path: strPtr("/admin/system/remote"),
		FrontendModuleCode: strPtr("system"), RouteKey: strPtr("system.remote.page"),
	})
	assertBusiness(t, err, 1, MessagePageModuleNotEnabled)

	addModule(env, "m1", "system", "系统管理", "1.0.0", domain.FrontendModuleStatusEnabled, 1, 0)
	if _, err := env.svc.CreatePage(env.ctx, rootRC(), domain.PageSavePayload{
		Name: strPtr("远程页"), Path: strPtr("/admin/system/remote"),
		FrontendModuleCode: strPtr("system"), RouteKey: strPtr("system.remote.page"),
	}); err != nil {
		t.Fatalf("合法远程页应创建成功: %v", err)
	}
}

func TestCreatePageRestoresSoftDeletedRow(t *testing.T) {
	env := newTestEnv(t)
	env.pages.deleted["old-id"] = domain.PageRecord{
		ID: "old-id", Code: "acat:admin:system:dicts", Name: "旧字典", Type: domain.PageTypePage,
		Scope: 0, IsEnabled: 0, CreatedAt: fixedNow, UpdatedAt: fixedNow, Version: 2,
	}

	entity, err := env.svc.CreatePage(env.ctx, rootRC(), domain.PageSavePayload{
		Name: strPtr("字典管理"), Path: strPtr("/admin/system/dicts"),
	})
	if err != nil {
		t.Fatalf("恢复分支失败: %v", err)
	}
	if entity.ID != "old-id" {
		t.Fatalf("恢复分支应复用旧 id，实际 %q", entity.ID)
	}
	order := env.pages.callOrder
	if len(order) != 2 || order[0] != "restore" || order[1] != "grant" {
		t.Fatalf("授权调用顺序异常: %v", order)
	}
}

func TestUpdatePageKeepsCodeAndAllowsClearingModule(t *testing.T) {
	env := newTestEnv(t)
	addPage(env, domain.PageRecord{
		ID: "p1", Code: "acat:admin:system:dicts", Name: "字典管理", Type: domain.PageTypePage,
		Path: strPtr("/admin/system/dicts"), Scope: 0, IsEnabled: 1, Version: 0,
	})

	entity, err := env.svc.UpdatePage(env.ctx, rootRC(), "p1", domain.PageSavePayload{
		Name: strPtr("字典配置"), Path: strPtr("/admin/system/dicts-2"),
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if entity.Code != "acat:admin:system:dicts" {
		t.Fatalf("code 不应变化: %q", entity.Code)
	}
	if entity.Path == nil || *entity.Path != "/admin/system/dicts-2" {
		t.Fatalf("path 应更新: %+v", entity.Path)
	}
	if entity.Version == nil || *entity.Version != 1 {
		t.Fatalf("版本应递增: %v", entity.Version)
	}
}

func TestUpdatePageNotFound(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.svc.UpdatePage(env.ctx, rootRC(), "missing", domain.PageSavePayload{})
	assertBusiness(t, err, 1, MessagePageNotFound)
}

func TestDeletePageCascadesChildren(t *testing.T) {
	env := newTestEnv(t)
	addPage(env, domain.PageRecord{ID: "nav", Code: "nav.code", Name: "导航", Type: domain.PageTypeNav, Scope: 0, IsEnabled: 1})
	addPage(env, domain.PageRecord{
		ID: "child", Code: "child.code", Name: "子页", Type: domain.PageTypePage,
		ParentID: strPtr("nav"), Scope: 0, IsEnabled: 1,
	})

	actor := workerRC("u1", "acat:admin:system:pages:delete")
	if err := env.svc.DeletePage(env.ctx, actor, "nav"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	for _, id := range []string{"nav", "child"} {
		if _, err := env.pages.FindPageByID(env.ctx, id); err == nil {
			t.Fatalf("页面 %s 应已软删", id)
		}
	}
	if len(env.pages.history) != 2 {
		t.Fatalf("每个被删节点都应写历史，实际 %d", len(env.pages.history))
	}
}

func TestDeletePageNotFound(t *testing.T) {
	env := newTestEnv(t)
	err := env.svc.DeletePage(env.ctx, rootRC(), "missing")
	assertBusiness(t, err, 1, MessagePageNotFound)
}

func TestDeletePageDepthGuard(t *testing.T) {
	env := newTestEnv(t)
	// 构造一个自引用环。
	addPage(env, domain.PageRecord{ID: "loop", Code: "loop", Name: "环", Type: domain.PageTypeNav, Scope: 0, IsEnabled: 1})
	record, _ := env.pages.FindPageByID(env.ctx, "loop")
	record.ParentID = strPtr("loop")
	env.pages.pages["loop"] = *record

	if err := env.svc.DeletePage(env.ctx, rootRC(), "loop"); err == nil {
		t.Fatal("环状父级应有防御性上限并返回业务失败")
	}
}

func TestNormalizePermissionPath(t *testing.T) {
	cases := map[string]string{
		"/admin/system/users": "system:users",
		"/system/users":       "system:users",
		"":                    "home",
		"/":                   "home",
		"/app/home/index":     "home:index",
	}
	for input, want := range cases {
		if got := domain.NormalizePermissionPath(input); got != want {
			t.Errorf("NormalizePermissionPath(%q) = %q，期望 %q", input, got, want)
		}
	}
}

func TestUploadAndReadFile(t *testing.T) {
	env := newTestEnv(t)
	vo, err := env.svc.UploadFile(env.ctx, rootRC(), "book_cover", "封面.PNG", "image/png", []byte("binary"))
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if vo.FileType != domain.FileTypeBookCover || vo.Size != 6 || vo.Type != "image/png" {
		t.Fatalf("上传结果异常: %+v", vo)
	}
	// 对象键必须是固定前缀 + UUID v4 + 小写扩展名。
	if len(vo.Path) == 0 || vo.Path[:len("acat-local/acat-fun/read/book/cover/")] != "acat-local/acat-fun/read/book/cover/" {
		t.Fatalf("对象路径前缀异常: %q", vo.Path)
	}
	if vo.Path[len(vo.Path)-4:] != ".png" {
		t.Fatalf("扩展名应为小写 png: %q", vo.Path)
	}
	if keys := env.objects.Keys(); len(keys) != 1 {
		t.Fatalf("对象存储应有一条记录: %v", keys)
	}

	file, err := env.svc.GetFile(env.ctx, vo.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	body, err := env.svc.OpenObject(env.ctx, *file)
	if err != nil || string(body) != "binary" {
		t.Fatalf("读取对象失败: %q %v", body, err)
	}
}

func TestUploadFileUsesConfiguredKeyPrefix(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		want   string
	}{
		{name: "自定义前缀", prefix: "platform", want: "acat-local/platform/book/cover/"},
		{name: "前缀首尾斜杠与空白归一化", prefix: " /platform/ ", want: "acat-local/platform/book/cover/"},
		{name: "留空取默认前缀", prefix: "", want: "acat-local/acat-fun/read/book/cover/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnvWith(t, func(options *Options) { options.FileKeyPrefix = tc.prefix })
			vo, err := env.svc.UploadFile(env.ctx, rootRC(), "book_cover", "封面.PNG", "image/png", []byte("binary"))
			if err != nil {
				t.Fatalf("上传失败: %v", err)
			}
			if len(vo.Path) < len(tc.want) || vo.Path[:len(tc.want)] != tc.want {
				t.Fatalf("对象路径前缀异常: %q，期望前缀 %q", vo.Path, tc.want)
			}
			// 记录路径必须与对象存储里的键一致，否则下载阶段解析不到对象。
			keys := env.objects.Keys()
			if len(keys) != 1 || keys[0] != vo.Path {
				t.Fatalf("对象键与记录路径不一致: %v / %q", keys, vo.Path)
			}
			file, err := env.svc.GetFile(env.ctx, vo.ID)
			if err != nil {
				t.Fatalf("查询失败: %v", err)
			}
			body, err := env.svc.OpenObject(env.ctx, *file)
			if err != nil || string(body) != "binary" {
				t.Fatalf("读取对象失败: %q %v", body, err)
			}
		})
	}
}

func TestUploadFileRejectsIllegalFileType(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.svc.UploadFile(env.ctx, rootRC(), "illegal", "a.txt", "text/plain", []byte("x"))
	assertBusiness(t, err, 1, MessageFileTypeUnsupported+"illegal")
	if keys := env.objects.Keys(); len(keys) != 0 {
		t.Fatalf("非法类型不应写入对象存储: %v", keys)
	}
}

func TestGetFileNotFound(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.svc.GetFile(env.ctx, "missing")
	assertBusiness(t, err, 1, MessageFileNotFound)
}

func TestDeleteFileRemovesObjectWhenRequested(t *testing.T) {
	env := newTestEnv(t)
	vo, err := env.svc.UploadFile(env.ctx, rootRC(), "avatar", "a.png", "image/png", []byte("x"))
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if err := env.svc.DeleteFile(env.ctx, vo.ID, false); err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	if keys := env.objects.Keys(); len(keys) != 1 {
		t.Fatalf("removeFromStorage=false 不应删对象: %v", keys)
	}

	second, err := env.svc.UploadFile(env.ctx, rootRC(), "avatar", "b.png", "image/png", []byte("y"))
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if err := env.svc.DeleteFile(env.ctx, second.ID, true); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if keys := env.objects.Keys(); len(keys) != 1 {
		t.Fatalf("removeFromStorage=true 应删除对象: %v", keys)
	}
}

func TestListFilesPagedAndFiltered(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.svc.UploadFile(env.ctx, rootRC(), "avatar", "a.png", "image/png", []byte("x")); err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if _, err := env.svc.UploadFile(env.ctx, rootRC(), "book_cover", "b.png", "image/png", []byte("y")); err != nil {
		t.Fatalf("上传失败: %v", err)
	}

	page, err := env.svc.ListFiles(env.ctx, 1, 10, "avatar")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	data, ok := page.(result.PageData[domain.AdminFileVO])
	if !ok {
		t.Fatalf("期望 PageData，实际 %T", page)
	}
	if data.Total != 1 || len(data.List) != 1 || data.List[0].FileType != domain.FileTypeAvatar {
		t.Fatalf("过滤结果异常: %+v", data)
	}
}

func TestResolveStorageLocation(t *testing.T) {
	location, err := domain.ResolveStorageLocation("acat-local/acat-fun/read/file/a.txt", "fallback")
	if err != nil || location.Bucket != "acat-local" || location.ObjectKey != "acat-fun/read/file/a.txt" {
		t.Fatalf("解析异常: %+v %v", location, err)
	}
	// 无分隔符 / 首尾斜杠 → 使用默认桶，整串作为 key。
	location, err = domain.ResolveStorageLocation("plain-key", "fallback")
	if err != nil || location.Bucket != "fallback" || location.ObjectKey != "plain-key" {
		t.Fatalf("无分隔符解析异常: %+v %v", location, err)
	}
	if _, err := domain.ResolveStorageLocation("", "fallback"); err == nil {
		t.Fatal("空路径应报错")
	}
}

// 确认 storage.Memory 满足注入接口（编译期）。
var _ = storage.ErrObjectNotFound
