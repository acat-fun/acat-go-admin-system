// Package service 实现 admin-user 的认证与权限装配业务逻辑。
//
// 职责：登录校验、启动数据（bootstrap）装配、
// 页面树构建与前端模块过滤。
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"47.108.230.93/acat-fun/acat-go-admin-system/auth/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/auth/repo"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/db"
	"github.com/acat-fun/acat-go-common/satoken"
)

// 业务失败提示文案。
const (
	// MessageInvalidCredential 登录失败统一文案（用户不存在/禁用/密码错都是它）。
	MessageInvalidCredential = "用户名或密码错误"
	// MessageUserNotFound 会话存在但用户已删除。
	MessageUserNotFound = "用户不存在"
	// DefaultI18nCode 是缺省语言码。
	DefaultI18nCode = domain.DefaultI18nCode
	// PageScopeAdmin 管理端页面范围。
	PageScopeAdmin = 0
)

// TxRunner 开启事务，并让 fn 及其下游 repo 通过 ctx 共享同一事务
// （由 acat-go-common/db.Handle 实现）。事务边界只出现在 service 层。
type TxRunner interface {
	// Within 在事务中执行 fn：返回 nil 提交，返回错误回滚；嵌套调用加入外层事务。
	Within(ctx context.Context, fn func(ctx context.Context) error) error
}

// ErrVersionConflict 表示写操作未命中任何行（并发冲突 / 记录已被改动或删除）。
//
// 内部哨兵：对外响应由调用方选择语义错误。
var ErrVersionConflict = errors.New("service: 写入未命中任何行")

// 写日志用的表名（仅用于结构化日志与错误定位，不参与 SQL）。
const (
	// TableReaderUser 读者用户表。
	TableReaderUser = "t_acat_user"
	// TableUserRole 用户-角色关联表。
	TableUserRole = "t_acat_user_role"
	// TableAdminWorker 工作人员表。
	TableAdminWorker = "t_acat_user_worker"
	// TableRole 角色表。
	TableRole = "t_acat_role"
	// TableRolePermission 角色-权限关联表。
	TableRolePermission = "t_acat_role_permission"
	// TablePermission 权限表。
	TablePermission = "t_acat_permission"
	// TablePage 页面表。
	TablePage = "t_acat_page"
)

// ServiceName 用于结构化写日志的 service 字段。
const ServiceName = "acat-admin-user"

// writeFact 把一次写操作的结果事实交给公共库工具判定（Repository 只报事实，Service 定语义）。
func writeFact(operation, entity, id string, affected int64) db.WriteFact {
	return db.WriteFact{
		Service:   ServiceName,
		Operation: operation,
		Entity:    entity,
		ID:        id,
		Affected:  affected,
	}
}

// requireUpdated 用于**后果严重**的写：0 行影响即返回冲突错误（严格模式）。
func requireUpdated(fact db.WriteFact, semantic error) error {
	return db.RequireUpdated(fact, semantic)
}

// warnIfNotUpdated 用于**普通资料更新 / 幂等删除 / 关联重建**：0 行只记结构化日志。
func (s *Service) warnIfNotUpdated(fact db.WriteFact) {
	db.WarnIfUnaffected(s.logger, fact)
}

// Service 提供认证与启动数据能力。
type Service struct {
	workers  repo.WorkerRepo
	pages    repo.PageRepo
	modules  repo.FrontendModuleRepo
	satoken  *satoken.Logic
	i18nCode string

	// 管理接口数据访问（readers/workers/roles/permissions）。
	// 允许为 nil：此时只有对应管理接口返回“未注入”错误，认证链路不受影响。
	readerRepo      repo.ReaderUserRepo
	adminWorkerRepo repo.AdminWorkerRepo
	roleRepo        repo.RoleRepo
	permissionRepo  repo.PermissionRepo
	tx              TxRunner
	logger          *slog.Logger
	// newID 生成主键（UUID v7），默认 domain.NewID；测试可注入固定序列。
	newID func() string
}

// Options 配置 Service。
type Options struct {
	Workers  repo.WorkerRepo
	Pages    repo.PageRepo
	Modules  repo.FrontendModuleRepo
	Satoken  *satoken.Logic
	I18nCode string
	// 以下为管理接口（readers/workers/roles/permissions）依赖，可选注入。
	Readers      repo.ReaderUserRepo
	AdminWorkers repo.AdminWorkerRepo
	Roles        repo.RoleRepo
	Permissions  repo.PermissionRepo
	// Tx 事务执行器（acat-go-common/db.Handle）；事务边界只在 service 层开启。
	Tx TxRunner
	// Logger 可选，默认 slog.Default()（写路径的 0 行影响告警使用）。
	Logger *slog.Logger
	// NewID 主键生成器，默认 domain.NewID。
	NewID func() string
}

// New 构造 Service。
func New(opts Options) (*Service, error) {
	if opts.Workers == nil || opts.Pages == nil || opts.Modules == nil || opts.Satoken == nil {
		return nil, fmt.Errorf("service: 依赖未完整注入")
	}
	if opts.Tx == nil {
		return nil, fmt.Errorf("service: Tx 必须注入（事务边界由 service 开启，见规范 §8.7）")
	}
	i18nCode := opts.I18nCode
	if i18nCode == "" {
		i18nCode = DefaultI18nCode
	}
	newID := opts.NewID
	if newID == nil {
		newID = domain.NewID
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		workers:         opts.Workers,
		pages:           opts.Pages,
		modules:         opts.Modules,
		satoken:         opts.Satoken,
		i18nCode:        i18nCode,
		readerRepo:      opts.Readers,
		adminWorkerRepo: opts.AdminWorkers,
		roleRepo:        opts.Roles,
		permissionRepo:  opts.Permissions,
		tx:              opts.Tx,
		logger:          logger,
		newID:           newID,
	}, nil
}

// nextID 生成主键（UUID v7）。
func (s *Service) nextID() string {
	if s.newID == nil {
		return domain.NewID()
	}
	return s.newID()
}

// LoginResult 是登录成功的返回值。
type LoginResult struct {
	// Token 是 Sa-Token token 值，服务端据此下发 HttpOnly Cookie。
	Token string
	// Bootstrap 是登录响应体。
	Bootstrap domain.Bootstrap
}

// Login 校验账号密码、写入 Sa-Token 会话并装配启动数据。
//
// 失败时返回 (*LoginResult)(nil) 与 err：调用方按 HTTP 200 + code=1 输出
// 。
func (s *Service) Login(ctx context.Context, username, password string) (*LoginResult, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, apperr.NewBusiness(MessageInvalidCredential)
	}
	worker, err := s.workers.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, apperr.NewBusiness(MessageInvalidCredential)
		}
		return nil, err
	}
	if worker.Status == 0 {
		return nil, apperr.NewBusiness(MessageInvalidCredential)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(worker.Password), []byte(password)); err != nil {
		return nil, apperr.NewBusiness(MessageInvalidCredential)
	}

	token, err := s.satoken.Login(ctx, worker.ID)
	if err != nil {
		return nil, fmt.Errorf("写入登录会话失败: %w", err)
	}
	bootstrap, err := s.buildBootstrap(ctx, worker)
	if err != nil {
		return nil, err
	}
	return &LoginResult{Token: token, Bootstrap: bootstrap}, nil
}

// Bootstrap 返回指定登录 id 的启动数据（bootstrap / user-info 共用）。
func (s *Service) Bootstrap(ctx context.Context, loginID string) (domain.Bootstrap, error) {
	worker, err := s.workers.FindByID(ctx, loginID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return domain.NewEmptyBootstrap(), apperr.NewBusiness(MessageUserNotFound)
		}
		return domain.NewEmptyBootstrap(), err
	}
	return s.buildBootstrap(ctx, worker)
}

// Permissions 返回指定登录 id 的按钮权限码（前端 hasPermission 使用）。
//
// 超管判定按角色：会话 roles 快照含 root 时返回全量权限码。
func (s *Service) Permissions(ctx context.Context, loginID string) ([]string, error) {
	session, err := s.satoken.GetSession(ctx, loginID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return []string{}, nil
	}
	if domain.IsRootRole(session.StringList(satoken.DataKeyRoles)) {
		codes, err := s.workers.AllPermissionCodes(ctx)
		if err != nil {
			return nil, err
		}
		return codes, nil
	}
	return session.StringList(satoken.DataKeyPermissions), nil
}

// Logout 注销 token。
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.satoken.Logout(ctx, token)
}

// buildBootstrap 装配登录后的启动数据（会话、页面树与前端模块清单）。
func (s *Service) buildBootstrap(ctx context.Context, worker *domain.Worker) (domain.Bootstrap, error) {
	// 超管判定统一按角色：roles 含 root 即超管（全量权限/全量页面），不再按登录 id 特判。
	roles, err := s.workers.RoleCodes(ctx, worker.ID)
	if err != nil {
		return domain.NewEmptyBootstrap(), err
	}
	root := domain.IsRootRole(roles)

	permissions, err := s.permissions(ctx, root, worker.ID)
	if err != nil {
		return domain.NewEmptyBootstrap(), err
	}
	urlCodes, err := s.urlCodes(ctx, root, worker.ID)
	if err != nil {
		return domain.NewEmptyBootstrap(), err
	}

	accessiblePages, err := s.loadAccessiblePages(ctx, root, urlCodes)
	if err != nil {
		return domain.NewEmptyBootstrap(), err
	}
	enabledModules, enabledModuleCodes, err := s.loadEnabledModules(ctx, accessiblePages)
	if err != nil {
		return domain.NewEmptyBootstrap(), err
	}

	visiblePages := make([]domain.Page, 0, len(accessiblePages))
	for _, page := range accessiblePages {
		if s.isModuleVisible(page.FrontendModuleCode, enabledModuleCodes) {
			visiblePages = append(visiblePages, page)
		}
	}

	// effectiveUrlCodes：仅保留可见页面对应的 code，去重且保持原顺序。
	visible := make(map[string]struct{}, len(visiblePages))
	for _, page := range visiblePages {
		visible[page.Code] = struct{}{}
	}
	effectiveURLs := make([]string, 0, len(urlCodes))
	seen := make(map[string]struct{}, len(urlCodes))
	for _, code := range urlCodes {
		if _, ok := visible[code]; !ok {
			continue
		}
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		effectiveURLs = append(effectiveURLs, code)
	}

	// 写入会话快照，供其它服务判权读取。
	if err := s.saveSessionSnapshot(ctx, worker, permissions, roles); err != nil {
		return domain.NewEmptyBootstrap(), err
	}

	bootstrap := domain.NewEmptyBootstrap()
	bootstrap.Session = domain.Session{
		UserID:      worker.ID,
		AccountName: worker.Username,
		Username:    worker.Username,
		Email:       nullable(worker.Email),
		Avatar:      nullable(worker.Avatar),
	}
	bootstrap.Roles = roles
	bootstrap.Permissions = permissions
	bootstrap.URLs = effectiveURLs
	bootstrap.Pages = BuildPageTree(visiblePages)
	bootstrap.FrontendModules = toModuleDescriptors(enabledModules)
	return bootstrap, nil
}

func (s *Service) permissions(ctx context.Context, root bool, userID string) ([]string, error) {
	if root {
		return s.workers.AllPermissionCodes(ctx)
	}
	codes, err := s.workers.URLAndButtonCodes(ctx, userID)
	if err != nil {
		return nil, err
	}
	return codes, nil
}

func (s *Service) urlCodes(ctx context.Context, root bool, userID string) ([]string, error) {
	if root {
		return s.workers.AllURLCodes(ctx)
	}
	return s.workers.URLCodes(ctx, userID)
}

// loadAccessiblePages 按 scope 拉取启用页面，非 root 按授权码过滤并补全祖先节点。
func (s *Service) loadAccessiblePages(ctx context.Context, root bool, urlCodes []string) ([]domain.Page, error) {
	all, err := s.pages.ListByScope(ctx, PageScopeAdmin, s.i18nCode)
	if err != nil {
		return nil, err
	}
	enabled := make([]domain.Page, 0, len(all))
	for _, page := range all {
		if page.IsEnabled == 1 {
			enabled = append(enabled, page)
		}
	}
	if root {
		return enabled, nil
	}
	if len(urlCodes) == 0 {
		return []domain.Page{}, nil
	}

	allowed := make(map[string]struct{}, len(urlCodes))
	for _, code := range urlCodes {
		allowed[code] = struct{}{}
	}
	byID := make(map[string]domain.Page, len(enabled))
	for _, page := range enabled {
		if _, exists := byID[page.ID]; !exists {
			byID[page.ID] = page
		}
	}
	allowedIDs := make(map[string]struct{})
	for _, page := range enabled {
		if _, ok := allowed[page.Code]; ok {
			allowedIDs[page.ID] = struct{}{}
		}
	}
	// 展开祖先节点，保证父目录可见。
	for id := range allowedIDs {
		current, ok := byID[id]
		for ok && current.ParentID != "" {
			allowedIDs[current.ParentID] = struct{}{}
			current, ok = byID[current.ParentID]
		}
	}
	out := make([]domain.Page, 0, len(allowedIDs))
	for _, page := range enabled {
		if _, ok := allowedIDs[page.ID]; ok {
			out = append(out, page)
		}
	}
	return out, nil
}

// loadEnabledModules 查询可见页面引用的已启用前端模块。
func (s *Service) loadEnabledModules(ctx context.Context, pages []domain.Page) ([]domain.FrontendModule, map[string]struct{}, error) {
	seen := map[string]struct{}{}
	codes := make([]string, 0, 8)
	for _, page := range pages {
		code := strings.TrimSpace(page.FrontendModuleCode)
		if code == "" || code == domain.ShellModuleCode {
			continue
		}
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		return []domain.FrontendModule{}, map[string]struct{}{}, nil
	}
	sort.Strings(codes)
	modules, err := s.modules.ListEnabledByCodes(ctx, codes)
	if err != nil {
		return nil, nil, err
	}
	enabled := make(map[string]struct{}, len(modules))
	for _, module := range modules {
		enabled[module.ModuleCode] = struct{}{}
	}
	return modules, enabled, nil
}

func (s *Service) isModuleVisible(moduleCode string, enabled map[string]struct{}) bool {
	code := strings.TrimSpace(moduleCode)
	if code == "" || code == domain.ShellModuleCode {
		return true
	}
	_, ok := enabled[code]
	return ok
}

// saveSessionSnapshot 把 permissions/roles/username 写入 Sa-Token Account-Session。
func (s *Service) saveSessionSnapshot(ctx context.Context, worker *domain.Worker, permissions, roles []string) error {
	session, err := s.satoken.GetSession(ctx, worker.ID)
	if err != nil {
		return fmt.Errorf("读取会话失败: %w", err)
	}
	if session == nil {
		session = satoken.NewSession(satoken.NewSessionID())
		session.LoginID = worker.ID
	}
	session.Set(satoken.DataKeyPermissions, permissions)
	session.Set(satoken.DataKeyRoles, roles)
	session.Set(satoken.DataKeyUsername, worker.Username)
	if err := s.satoken.SaveSession(ctx, session); err != nil {
		return fmt.Errorf("写入会话失败: %w", err)
	}
	return nil
}

func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// toModuleDescriptors。
// manifestUrl/fallbackManifestUrl 直接透传库值，NULL 保持 null（不转空串）。
func toModuleDescriptors(modules []domain.FrontendModule) []domain.FrontendModuleDescriptor {
	out := make([]domain.FrontendModuleDescriptor, 0, len(modules))
	for _, module := range modules {
		out = append(out, domain.FrontendModuleDescriptor{
			ModuleCode:          module.ModuleCode,
			Version:             module.ReleaseVersion,
			ContractVersion:     module.ContractVersion,
			ManifestURL:         module.ManifestPath,
			FallbackManifestURL: module.FallbackManifestPath,
		})
	}
	return out
}
