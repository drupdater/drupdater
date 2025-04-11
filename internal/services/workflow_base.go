package services

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"

	"ebersolve.com/updater/internal"
	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"go.uber.org/zap"
)

//go:embed templates
var templates embed.FS

type TemplateData struct {
	ComposerDiff           string
	DependencyUpdateReport DependencyUpdateReport
	SecurityReport         SecurityReport
	UpdateHooks            UpdateHooksPerSite
}

type SecurityReport struct {
	FixedAdvisories       []Advisory
	AfterUpdateAdvisories []Advisory
	NumUnresolvedIssues   int
}

type WorkflowService interface {
	StartUpdate() error
}

type WorkflowBaseService struct {
	logger *zap.Logger
	config internal.Config
}

func (ws *WorkflowBaseService) PushChanges(repository internal.Repository, updateBranchName string) error {
	err := repository.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs: []gitConfig.RefSpec{
			gitConfig.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", updateBranchName, updateBranchName)),
		},
		Auth: &http.BasicAuth{
			Username: "du", // yes, this can be anything except an empty string
			Password: ws.config.Token,
		},
	})

	if err != nil {
		ws.logger.Error("failed to push changes", zap.Error(err))
		return err
	}

	return nil
}

func (ws *WorkflowBaseService) GenerateDescription(data interface{}, filename string) (string, error) {

	tmpl, err := template.ParseFS(templates, "templates/*.go.tmpl")
	if err != nil {
		panic(err)
	}

	var output bytes.Buffer

	err = tmpl.ExecuteTemplate(&output, filename, data)
	if err != nil {
		return "", err
	}

	return output.String(), nil
}
