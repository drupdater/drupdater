package services

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/drupdater/drupdater/internal"
	"go.uber.org/zap"
)

// SecurityUpdateStrategy implements the security update workflow
type SecurityUpdateStrategy struct {
	logger          *zap.Logger
	config          internal.Config
	current         time.Time
	composerService ComposerService
	beforeAudit     ComposerAudit
	afterAudit      ComposerAudit
}

func NewSecurityUpdateStrategy(
	logger *zap.Logger,
	config internal.Config,
	composerService ComposerService,
) SecurityUpdateStrategy {
	return SecurityUpdateStrategy{
		logger:          logger,
		config:          config,
		current:         time.Now(),
		composerService: composerService,
	}
}

func (s SecurityUpdateStrategy) PreUpdate(ctx context.Context, path string) ([]string, bool, error) {
	var err error

	s.beforeAudit, err = s.composerService.RunComposerAudit(ctx, path)
	if err != nil {
		return nil, false, err
	}

	packagesToUpdate, err := s.getPackagesToUpdate(ctx, path, s.beforeAudit.Advisories)
	if err != nil {
		return nil, false, err
	}

	// For security updates, we use minimal changes approach
	return packagesToUpdate, true, nil
}

func (s SecurityUpdateStrategy) ShouldContinue(packagesToUpdate []string) bool {
	if len(packagesToUpdate) == 0 {
		s.logger.Info("no security advisories found, skipping security update")
		return false
	}
	return true
}

func (s SecurityUpdateStrategy) PostUpdate(ctx context.Context, path string, worktree internal.Worktree, result WorkflowUpdateResult) error {
	var err error

	s.afterAudit, err = s.composerService.RunComposerAudit(ctx, path)
	if err != nil {
		return err
	}

	return nil
}

func (s SecurityUpdateStrategy) GenerateBranchName(composerLockHash string) string {
	return fmt.Sprintf("security-update-%s", composerLockHash)
}

func (s SecurityUpdateStrategy) GeneratePRDetails() (string, string) {
	title := fmt.Sprintf("%s: Drupal Security Updates", s.current.Format("2006-01-02"))
	templateName := "security_update.go.tmpl"
	return title, templateName
}

func (s SecurityUpdateStrategy) GetTemplateData(result WorkflowUpdateResult, updateHooks UpdateHooksPerSite) (TemplateData, error) {

	return TemplateData{
		ComposerDiff:           result.table,
		DependencyUpdateReport: result.updateReport,
		SecurityReport: SecurityReport{
			FixedAdvisories:       s.GetFixedAdvisories(),
			AfterUpdateAdvisories: s.afterAudit.Advisories,
			NumUnresolvedIssues:   len(s.afterAudit.Advisories),
		},
		UpdateHooks: updateHooks,
	}, nil
}

func (s SecurityUpdateStrategy) GetFixedAdvisories() []Advisory {
	// Get advisories from before that are not present in after
	var fixed = make([]Advisory, 0)
	for _, beforeAdvisory := range s.beforeAudit.Advisories {
		found := false
		for _, afterAdvisory := range s.afterAudit.Advisories {
			if beforeAdvisory.CVE == afterAdvisory.CVE {
				found = true
				break
			}
		}
		if !found {
			fixed = append(fixed, beforeAdvisory)
		}
	}
	return fixed
}

func (s SecurityUpdateStrategy) getPackagesToUpdate(ctx context.Context, path string, advisories []Advisory) ([]string, error) {
	s.logger.Info("found security advisories", zap.Int("numAdvisories", len(advisories)))
	s.logger.Info("advisories", zap.Any("advisories", advisories))

	packagesToUpdate := make([]string, 0)
	for _, advisory := range advisories {
		if slices.Contains(packagesToUpdate, advisory.PackageName) {
			continue
		}
		packagesToUpdate = append(packagesToUpdate, advisory.PackageName)
	}

	if slices.Contains(packagesToUpdate, "drupal/core") {
		packagesToUpdate = append(packagesToUpdate, "drupal/core-recommended")
		packagesToUpdate = append(packagesToUpdate, "drupal/core-composer-scaffold")
	}

	return packagesToUpdate, nil
}
