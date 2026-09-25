package domain

import (
	"regexp"
	"strings"
)

// 本文件对应 侧前端模块域实体与视图：
//   - AdminFrontendModuleEntity (fun.acat.admin.entity.AdminFrontendModuleEntity, 表 t_acat_frontend_module)
//   - FrontendModuleVO          (fun.acat.admin.system.vo.FrontendModuleVO)
//   - FrontendModuleSaveParam / FrontendModulePublicationParam (请求参数)

// 前端模块状态（AdminFrontendModuleEntity.STATUS_*）。
const (
	FrontendModuleStatusDraft    = 0
	FrontendModuleStatusEnabled  = 1
	FrontendModuleStatusDisabled = 2
)

// FrontendModuleVO。
type FrontendModuleVO struct {
	ID                   string  `json:"id"`
	ModuleCode           string  `json:"moduleCode"`
	Name                 string  `json:"name"`
	ReleaseVersion       string  `json:"releaseVersion"`
	ContractVersion      int     `json:"contractVersion"`
	ManifestPath         string  `json:"manifestPath"`
	FallbackVersion      *string `json:"fallbackVersion"`
	FallbackManifestPath *string `json:"fallbackManifestPath"`
	Status               int     `json:"status"`
	SortOrder            int     `json:"sortOrder"`
	Version              int     `json:"version"`
	CreatedAt            *string `json:"createdAt"`
	UpdatedAt            *string `json:"updatedAt"`
}

// FrontendModuleRecord 是 t_acat_frontend_module 的内部读模型（定义见 record.go），
// ToVO。
func (m FrontendModuleRecord) ToVO() FrontendModuleVO {
	return FrontendModuleVO{
		ID:                   m.ID,
		ModuleCode:           m.ModuleCode,
		Name:                 m.Name,
		ReleaseVersion:       m.ReleaseVersion,
		ContractVersion:      m.ContractVersion,
		ManifestPath:         m.ManifestPath,
		FallbackVersion:      m.FallbackVersion,
		FallbackManifestPath: m.FallbackManifestPath,
		Status:               m.Status,
		SortOrder:            m.SortOrder,
		Version:              m.Version,
		CreatedAt:            StringOrNil(FormatDateTime(m.CreatedAt)),
		UpdatedAt:            StringOrNil(FormatDateTime(m.UpdatedAt)),
	}
}

// FrontendModuleSaveParam。
//
// 用指针承载可空字段，以便区分“未传”与“传了空串”。
type FrontendModuleSaveParam struct {
	ModuleCode           *string `json:"moduleCode"`
	Name                 *string `json:"name"`
	ReleaseVersion       *string `json:"releaseVersion"`
	ContractVersion      *int    `json:"contractVersion"`
	ManifestPath         *string `json:"manifestPath"`
	FallbackVersion      *string `json:"fallbackVersion"`
	FallbackManifestPath *string `json:"fallbackManifestPath"`
	SortOrder            *int    `json:"sortOrder"`
}

// FrontendModulePublicationParam。
type FrontendModulePublicationParam struct {
	ReleaseVersion       *string `json:"releaseVersion"`
	ContractVersion      *int    `json:"contractVersion"`
	ManifestPath         *string `json:"manifestPath"`
	FallbackVersion      *string `json:"fallbackVersion"`
	FallbackManifestPath *string `json:"fallbackManifestPath"`
	Status               *int    `json:"status"`
	ExpectedVersion      *int    `json:"expectedVersion"`
}

// FrontendModuleCodePattern 是模块码格式约束。
var FrontendModuleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

// frontendModuleCodePattern 与 FrontendModuleCodePattern 同义（FrontendModulePathValidator 使用）。
var frontendModuleCodePattern = FrontendModuleCodePattern

// frontendModuleVersionPattern。
var frontendModuleVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// IsValidManifestPath。
// manifest 必须等于 /admin-remotes/{moduleCode}/{version}/mf-manifest.json。
func IsValidManifestPath(moduleCode, version, manifestPath string) bool {
	if !frontendModuleCodePattern.MatchString(moduleCode) {
		return false
	}
	if !frontendModuleVersionPattern.MatchString(version) {
		return false
	}
	return manifestPath == "/admin-remotes/"+moduleCode+"/"+version+"/mf-manifest.json"
}

// IsBlank 判断字符串是否为 null/空白（含全角空格以外的 Unicode 空白，
// Go 侧用 TrimSpace 近似；对本服务的实际输入等价）。
func IsBlank(value *string) bool {
	return value == nil || strings.TrimSpace(*value) == ""
}
