// Package service 实现 admin-system 的业务逻辑。
//
// 七域各自独立成文件：
//
//	dict_service.go            字典与字典数据项
//	i18n_service.go            语言类型与前端运行时字典
//	page_service.go            页面与页面树
//	frontend_module_service.go 前端模块
//	auditlog_service.go        审计日志
//	file_service.go            文件
//
// 统一口径：
//   - 业务失败返回 apperr.NewBusiness(消息)（HTTP 200 + body.code=1）；
//   - 需要非 1 业务码时用 apperr.NewBusinessCode（前端模块乐观锁 40901）；
//   - 基础设施异常原样向上抛（上层转 503/500，保留 cause，不吞异常）。
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/logic"
	"47.108.230.93/acat-fun/acat-go-admin-system/repo"
	"47.108.230.93/acat-fun/acat-go-admin-system/storage"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/db"
	"github.com/acat-fun/acat-go-common/result"
)

const (
	// DefaultAuditPageSize 审计日志默认每页条数。
	DefaultAuditPageSize = 20
	// DefaultI18nCode 是缺省语言码（zh-CN）。
	DefaultI18nCode = domain.DefaultI18nCode
	// DefaultFrontendDictCode 是前端运行时字典的默认 dictCode。
	DefaultFrontendDictCode = domain.FrontendAdminDictCode
)

// 业务码：本服务唯一的非 1 业务码。
const (
	// BusinessCodeFrontendModuleConflict 前端模块乐观锁冲突。
	BusinessCodeFrontendModuleConflict = 40901
)

// 业务失败提示文案（前端直接展示，不能改写）。
const (
	MessageDictCodeExists             = "字典编码已存在: "
	MessageDictNotFound               = "字典不存在"
	MessageDictBuiltinUndeletable     = "内置字典不可删除"
	MessageDictDataParentNotFd        = "父级数据项不存在"
	MessageDictDataNotFound           = "数据项不存在"
	MessageDictDataBuiltinUndeletable = "内置字典项不可删除"

	MessageI18nCodeExists             = "语言编码已存在: "
	MessageI18nTypeGone               = "语言类型不存在"
	MessageI18nTypeBuiltinUndeletable = "内置语言类型不可删除"

	MessagePagePathRequired       = "页面路径不能为空"
	MessagePageParentNotFound     = "父级 页面不存在"
	MessagePageParentNotAdmin     = "父级页面不属于后台"
	MessagePageNotFound           = "页面不存在"
	MessagePageBuiltinUndeletable = "内置页面不可删除"
	MessagePageRouteKeyInvalid    = "远程页面 routeKey 格式不合法"
	MessagePageModuleNotEnabled   = "引用的前端模块不存在或未启用"

	MessageModuleNotFound        = "前端模块不存在"
	MessageModuleCodeImmutable   = "模块代码创建后不可修改"
	MessageModuleVersionExists   = "同模块版本已存在"
	MessageModuleSystemUndelete  = "系统管理模块不可停用"
	MessageModuleStatusInvalid   = "发布状态仅支持启用或停用"
	MessageModuleContractInvalid = "当前仅支持远程契约版本 1"
	MessageModuleManifestInvalid = "manifest 路径必须是当前模块和版本对应的同源不可变路径"
	MessageModuleFallbackSame    = "回退版本不能指向当前版本"
	MessageModuleFallbackPath    = "回退 manifest 路径不合法"
	MessageModuleFallbackMissing = "回退版本不存在"
	MessageModuleConflict        = "前端模块已被其他操作更新，请刷新后重试"

	MessageFileNotFound          = "文件不存在"
	MessageFileTypeUnsupported   = "不支持的文件业务类型: "
	MessageFileUploadFailed      = "文件上传失败"
	MessageFileMetaPersistFailed = "文件元数据持久化失败"
	MessageFileStreamFailed      = "获取文件流失败"
	MessageFileRemoveFailed      = "对象存储文件删除失败"
	MessageFileOverwriteFallback = "文件元数据持久化失败: 影响行数为 0"
)

// RequestContext 承载请求上下文信息：
// Sa-Token 登录态（会话角色与权限快照）与请求语言。
type RequestContext struct {
	// I18nCode 是 Accept-Language 原样值；空则回退 zh-CN。
	I18nCode string
	// Actor 是当前操作者（登录 id + 会话权限）；可为 nil（未登录，仅公共接口）。
	Actor *logic.Actor
}

// Language 返回生效的语言编码。
func (rc RequestContext) Language() string {
	if strings.TrimSpace(rc.I18nCode) == "" {
		return DefaultI18nCode
	}
	return rc.I18nCode
}

// LoginID 返回当前登录 id；无登录态时回退空串（写路径 createBy/updateBy 记空）。
func (rc RequestContext) LoginID() string {
	if rc.Actor != nil && rc.Actor.LoginID != "" {
		return rc.Actor.LoginID
	}
	return ""
}

// IsRoot 判断当前操作者是否为超级管理员（会话角色含 root）。
func (rc RequestContext) IsRoot() bool {
	return rc.Actor != nil && rc.Actor.IsRoot()
}

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

// 写日志用的表名（仅用于结构化日志，不参与 SQL）。
const (
	// TableDict 字典表。
	TableDict = "t_acat_dict"
	// TableDictData 字典数据项表。
	TableDictData = "t_acat_dict_data"
	// TableI18nLabel i18n 标签表。
	TableI18nLabel = "t_acat_i18n_label"
	// TablePage 页面表。
	TablePage = "t_acat_page"
	// TablePageHistory 页面历史表。
	TablePageHistory = "t_acat_page_history"
	// TableFile 文件表。
	TableFile = "t_acat_file"
	// TableI18nType 语言类型表。
	TableI18nType = "t_acat_i18n_type"
	// TableFrontendModule 前端模块表。
	TableFrontendModule = "t_acat_frontend_module"
)

// ServiceName 用于结构化写日志的 service 字段。
const ServiceName = "acat-admin-system"

// writeFact 把一次写操作的结果事实交给公共库工具判定（Repository 只报事实，Service 定语义）。
func writeFact(operation, entity, id string, expectedVersion *int, affected int64) db.WriteFact {
	return db.WriteFact{
		Service:         ServiceName,
		Operation:       operation,
		Entity:          entity,
		ID:              id,
		ExpectedVersion: expectedVersion,
		Affected:        affected,
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

// Service 聚合 admin-system 的全部业务能力。
type Service struct {
	dicts     repo.DictRepo
	labels    repo.LabelRepo
	pages     repo.PageRepo
	modules   repo.FrontendModuleRepo
	i18nTypes repo.I18nTypeRepo
	files     repo.FileRepo
	tx        TxRunner
	logger    *slog.Logger
	audits    repo.AuditLogStore
	objects   storage.ObjectStorage
	i18nCode  string
	// newID 生成主键（UUID v7），默认 domain.NewID；测试可注入固定序列。
	newID func() string
	// now 返回当前时间（秒截断），默认 domain.Now；测试可注入固定时间。
	now func() time.Time
}

// Options 配置 Service。
type Options struct {
	Dicts     repo.DictRepo
	Labels    repo.LabelRepo
	Pages     repo.PageRepo
	Modules   repo.FrontendModuleRepo
	I18nTypes repo.I18nTypeRepo
	Files     repo.FileRepo
	// Tx 事务执行器（acat-go-common/db.Handle）；事务边界只在 service 层开启。
	Tx TxRunner
	// Logger 可选，默认 slog.Default()（写路径的 0 行影响告警使用）。
	Logger *slog.Logger
	// Audits 是审计日志存储；nil 时使用内存实现（本机无 Mongo）。
	Audits repo.AuditLogStore
	// Objects 是对象存储；nil 时使用内存实现（本机无 MinIO）。
	Objects storage.ObjectStorage
	// I18nCode 覆盖默认语言。
	I18nCode string
	// NewID 主键生成器，默认 domain.NewID。
	NewID func() string
	// Now 时间源，默认 domain.Now。
	Now func() time.Time
}

// New 构造 Service。
//
// Pages/Modules/Dicts/Labels 为必需依赖；审计与对象存储缺省时使用内存实现，
// 保证无 Mongo/MinIO 的本地环境也能启动（内存实现不可用于生产，见 README）。
func New(opts Options) (*Service, error) {
	if opts.Dicts == nil || opts.Labels == nil || opts.Pages == nil || opts.Modules == nil || opts.I18nTypes == nil {
		return nil, fmt.Errorf("service: 字典/标签/页面/前端模块/语言类型依赖必须注入")
	}
	if opts.Tx == nil {
		return nil, fmt.Errorf("service: Tx 必须注入（事务边界由 service 开启，见规范 §8.7）")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	audits := opts.Audits
	if audits == nil {
		audits = repo.NewMemoryAuditLogStore()
	}
	objects := opts.Objects
	if objects == nil {
		objects = storage.NewMemory("acat")
	}
	i18nCode := opts.I18nCode
	if i18nCode == "" {
		i18nCode = DefaultI18nCode
	}
	newID := opts.NewID
	if newID == nil {
		newID = domain.NewID
	}
	now := opts.Now
	if now == nil {
		now = domain.Now
	}
	return &Service{
		dicts:     opts.Dicts,
		labels:    opts.Labels,
		pages:     opts.Pages,
		modules:   opts.Modules,
		i18nTypes: opts.I18nTypes,
		files:     opts.Files,
		tx:        opts.Tx,
		logger:    logger,
		audits:    audits,
		objects:   objects,
		i18nCode:  i18nCode,
		newID:     newID,
		now:       now,
	}, nil
}

// nextID 生成主键（UUID v7）。
func (s *Service) nextID() string {
	if s.newID == nil {
		return domain.NewID()
	}
	return s.newID()
}

// nextCompactID 生成 32 位无连字符 UUID。
func (s *Service) nextCompactID() string {
	return strings.ReplaceAll(s.nextID(), "-", "")
}

// AuditStore 暴露审计日志存储，供 HTTP 审计中间件写入。
//
// httpapi 中间件在装配时取用同一存储实例（httpapi.New 的默认装配）。
func (s *Service) AuditStore() repo.AuditLogStore { return s.audits }

// business 构造业务失败（HTTP 200 + code=1）。
//
// 消息一律由调用方拼好，因此这里不做格式化，避免
// "non-constant format string" 之类的误用。
func business(message string) error {
	return apperr.NewBusiness(message)
}

// businessCode 构造指定业务码的业务失败。
func businessCode(code int, message string) error {
	return apperr.NewBusinessCode(code, message)
}

// toI18nValues 把 repo 标签行转换为 I18nValue 列表（空切片。
func toI18nValues(records []domain.LabelRecord) []domain.I18nValue {
	out := make([]domain.I18nValue, 0, len(records))
	for _, record := range records {
		out = append(out, domain.I18nValue{I18n: record.I18nCode, Value: record.LabelValue})
	}
	return out
}

// resolveDefaultNameOrNil。
// 优先取 i18nValue 中 zh-CN 的非空白值，否则回退 name（可能为 nil）。
func resolveDefaultNameOrNil(fallback *string, values []domain.I18nValue) *string {
	if len(values) == 0 {
		return fallback
	}
	for _, value := range values {
		if value.I18n != domain.DefaultI18nCode {
			continue
		}
		if strings.TrimSpace(value.Value) == "" {
			continue
		}
		resolved := value.Value
		return &resolved
	}
	return fallback
}

// resolveName 在 fallback 基础上做语言解析（列表场景：当前语言标签优先）。
func (s *Service) resolveName(rc RequestContext, sourceTable, tableDataID, fallback string) string {
	label, err := s.labels.ResolveCurrentName(context.Background(), sourceTable, tableDataID, rc.Language())
	if err != nil || strings.TrimSpace(label) == "" {
		return fallback
	}
	return label
}

// newPageData 构造分页结果。
//
// 普通分页 headNodeTotal=null，
// 树分页用 withHeadNodeTotal。
func newPageData[T any](list []T, total int64, pageIndex, pageSize int) result.PageData[T] {
	return result.NewPageData(list, total, pageIndex, pageSize)
}

// withHeadNodeTotal 设置树分页的根节点总数。
func withHeadNodeTotal[T any](page result.PageData[T], headNodeTotal int64) result.PageData[T] {
	page.HeadNodeTotal = &headNodeTotal
	return page
}
