package service

import (
	"context"
	"errors"
	"strings"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/repo"
)

// ListFrontendModules。
func (s *Service) ListFrontendModules(ctx context.Context, moduleCode string) ([]domain.FrontendModuleVO, error) {
	records, err := s.modules.ListModules(ctx, moduleCode)
	if err != nil {
		return nil, err
	}
	out := make([]domain.FrontendModuleVO, 0, len(records))
	for _, record := range records {
		out = append(out, record.ToVO())
	}
	return out, nil
}

// CreateFrontendModule。
func (s *Service) CreateFrontendModule(ctx context.Context, param domain.FrontendModuleSaveParam) (*domain.FrontendModuleVO, error) {
	if err := s.validateFrontendModule(param.ModuleCode, param.ContractVersion,
		param.ReleaseVersion, param.ManifestPath, param.FallbackVersion, param.FallbackManifestPath); err != nil {
		return nil, err
	}
	moduleCode := domain.DerefString(param.ModuleCode)
	releaseVersion := domain.DerefString(param.ReleaseVersion)
	existing, err := s.modules.FindModuleByCodeAndVersion(ctx, moduleCode, releaseVersion)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	if existing != nil {
		return nil, business(MessageModuleVersionExists)
	}
	now := s.now()
	record := domain.FrontendModuleRecord{
		ID:                   s.nextID(),
		ModuleCode:           moduleCode,
		Name:                 domain.DerefString(param.Name),
		ReleaseVersion:       releaseVersion,
		ContractVersion:      intValueOr(param.ContractVersion, 0),
		ManifestPath:         domain.DerefString(param.ManifestPath),
		FallbackVersion:      param.FallbackVersion,
		FallbackManifestPath: nil,
		Status:               domain.FrontendModuleStatusDraft,
		SortOrder:            intValueOr(param.SortOrder, 0),
		CreatedAt:            now,
		UpdatedAt:            now,
		Version:              0,
	}
	record.FallbackManifestPath, err = s.resolveFallbackManifestPath(ctx, moduleCode,
		param.FallbackVersion, param.FallbackManifestPath)
	if err != nil {
		return nil, err
	}
	if err := s.modules.InsertModule(ctx, record); err != nil {
		return nil, err
	}
	vo := record.ToVO()
	return &vo, nil
}

// UpdateFrontendModule。
func (s *Service) UpdateFrontendModule(ctx context.Context, id string, param domain.FrontendModuleSaveParam) (*domain.FrontendModuleVO, error) {
	record, err := s.modules.FindModuleByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, business(MessageModuleNotFound)
		}
		return nil, err
	}
	moduleCode := domain.DerefString(param.ModuleCode)
	if record.ModuleCode != moduleCode {
		return nil, business(MessageModuleCodeImmutable)
	}
	if err := s.validateFrontendModule(param.ModuleCode, param.ContractVersion,
		param.ReleaseVersion, param.ManifestPath, param.FallbackVersion, param.FallbackManifestPath); err != nil {
		return nil, err
	}
	releaseVersion := domain.DerefString(param.ReleaseVersion)
	sameVersion, err := s.modules.FindModuleByCodeAndVersion(ctx, moduleCode, releaseVersion)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	if sameVersion != nil && sameVersion.ID != id {
		return nil, business(MessageModuleVersionExists)
	}

	record.Name = domain.DerefString(param.Name)
	record.SortOrder = intValueOr(param.SortOrder, 0)
	record.ReleaseVersion = releaseVersion
	record.ContractVersion = intValueOr(param.ContractVersion, 0)
	record.ManifestPath = domain.DerefString(param.ManifestPath)
	record.FallbackVersion = param.FallbackVersion
	record.FallbackManifestPath, err = s.resolveFallbackManifestPath(ctx, moduleCode,
		param.FallbackVersion, param.FallbackManifestPath)
	if err != nil {
		return nil, err
	}
	record.UpdatedAt = s.now()
	if _, err := s.modules.UpdateModule(ctx, *record); err != nil {
		return nil, err
	}
	// MyBatis-Plus 乐观锁把新版本号写回内存实体。
	record.Version++
	vo := record.ToVO()
	return &vo, nil
}

// PublishFrontendModule。
// 版本预检 → 系统模块保护 → 状态白名单 → 校验 → 唯一性 → 更新（0 行 → 40901）→ 停用同模块其他启用版本。
func (s *Service) PublishFrontendModule(ctx context.Context, id string, param domain.FrontendModulePublicationParam) (*domain.FrontendModuleVO, error) {
	record, err := s.modules.FindModuleByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, business(MessageModuleNotFound)
		}
		return nil, err
	}
	expected := 0
	if param.ExpectedVersion != nil {
		expected = *param.ExpectedVersion
	} else {
		return nil, businessCode(BusinessCodeFrontendModuleConflict, MessageModuleConflict)
	}
	if record.Version != expected {
		return nil, businessCode(BusinessCodeFrontendModuleConflict, MessageModuleConflict)
	}
	status := intValueOr(param.Status, 0)
	if record.ModuleCode == "system" && status != domain.FrontendModuleStatusEnabled {
		return nil, business(MessageModuleSystemUndelete)
	}
	if status != domain.FrontendModuleStatusEnabled && status != domain.FrontendModuleStatusDisabled {
		return nil, business(MessageModuleStatusInvalid)
	}
	if err := s.validateFrontendModule(
		ptrString(record.ModuleCode), param.ContractVersion, param.ReleaseVersion,
		param.ManifestPath, param.FallbackVersion, param.FallbackManifestPath); err != nil {
		return nil, err
	}
	releaseVersion := domain.DerefString(param.ReleaseVersion)
	sameVersion, err := s.modules.FindModuleByCodeAndVersion(ctx, record.ModuleCode, releaseVersion)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	if sameVersion != nil && sameVersion.ID != id {
		return nil, business(MessageModuleVersionExists)
	}

	record.ReleaseVersion = releaseVersion
	record.ContractVersion = intValueOr(param.ContractVersion, 0)
	record.ManifestPath = domain.DerefString(param.ManifestPath)
	record.FallbackVersion = param.FallbackVersion
	record.FallbackManifestPath, err = s.resolveFallbackManifestPath(ctx, record.ModuleCode,
		param.FallbackVersion, param.FallbackManifestPath)
	if err != nil {
		return nil, err
	}
	record.Status = status
	record.UpdatedAt = s.now()
	affected, err := s.modules.UpdateModule(ctx, *record)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, businessCode(BusinessCodeFrontendModuleConflict, MessageModuleConflict)
	}
	if status == domain.FrontendModuleStatusEnabled {
		if err := s.modules.DisableOtherEnabledVersions(ctx, record.ModuleCode, record.ID); err != nil {
			return nil, err
		}
	}
	record.Version++
	vo := record.ToVO()
	return &vo, nil
}

// validateFrontendModule。
func (s *Service) validateFrontendModule(moduleCode *string, contractVersion *int, releaseVersion, manifestPath,
	fallbackVersion, fallbackManifestPath *string) error {
	if contractVersion == nil || *contractVersion != 1 {
		return business(MessageModuleContractInvalid)
	}
	if !domain.IsValidManifestPath(domain.DerefString(moduleCode), domain.DerefString(releaseVersion),
		domain.DerefString(manifestPath)) {
		return business(MessageModuleManifestInvalid)
	}
	hasFallbackVersion := !domain.IsBlank(fallbackVersion)
	hasFallbackPath := !domain.IsBlank(fallbackManifestPath)
	if hasFallbackVersion && domain.DerefString(fallbackVersion) == domain.DerefString(releaseVersion) {
		return business(MessageModuleFallbackSame)
	}
	if hasFallbackVersion && hasFallbackPath &&
		!domain.IsValidManifestPath(domain.DerefString(moduleCode), domain.DerefString(fallbackVersion),
			domain.DerefString(fallbackManifestPath)) {
		return business(MessageModuleFallbackPath)
	}
	if hasFallbackVersion && !hasFallbackPath {
		found, err := s.modules.FindModuleByCodeAndVersion(context.Background(),
			domain.DerefString(moduleCode), domain.DerefString(fallbackVersion))
		if err != nil && !errors.Is(err, repo.ErrNotFound) {
			return err
		}
		if found == nil {
			return business(MessageModuleFallbackMissing)
		}
	}
	return nil
}

// resolveFallbackManifestPath。
// fallbackVersion 为空 → null；否则优先取库中该版本行的 manifest_path，查不到才用入参。
func (s *Service) resolveFallbackManifestPath(ctx context.Context, moduleCode string,
	fallbackVersion, fallbackManifestPath *string) (*string, error) {
	if domain.IsBlank(fallbackVersion) {
		return nil, nil
	}
	fallback, err := s.modules.FindModuleByCodeAndVersion(ctx, moduleCode, domain.DerefString(fallbackVersion))
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	if fallback != nil {
		path := fallback.ManifestPath
		return &path, nil
	}
	if fallbackManifestPath == nil || strings.TrimSpace(*fallbackManifestPath) == "" {
		return nil, nil
	}
	return fallbackManifestPath, nil
}

func ptrString(value string) *string { return &value }
