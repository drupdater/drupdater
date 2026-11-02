package composer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/drupdater/drupdater/internal/agent"
)

// Sites asks the agent about one site of a project at a time: its facts, its update, its config
// export, and the throwaway SQLite database it runs on, kept under dataDir.
type Sites struct {
	cli     *CLI
	dataDir string
}

// Sites holds each site's database under dataDir, which the caller creates and removes.
func (s *CLI) Sites(dataDir string) *Sites {
	return &Sites{cli: s, dataDir: dataDir}
}

type siteRequest struct {
	Site    string `json:"site"`
	DataDir string `json:"data_dir,omitempty"`
}

type factsResult struct {
	Facts json.RawMessage `json:"facts"`
}

// Facts reads what the addons read of a site, asking drupal.org which modules are unsupported.
func (s *Sites) Facts(ctx context.Context, dir string, site string) (json.RawMessage, error) {
	result, err := agent.Call[factsResult](ctx, s.cli.command(dir), "drupdater:site-facts", struct {
		Site         string `json:"site"`
		UpdateStatus bool   `json:"update_status"`
	}{site, true})
	if err != nil {
		return nil, fmt.Errorf("failed to read the facts of site %s: %w", site, err)
	}
	return facts(result, site)
}

// Update runs the site's update hooks and resaves its configuration, returning its facts as they
// stand afterwards, without the unsupported modules.
func (s *Sites) Update(ctx context.Context, dir string, site string) (json.RawMessage, error) {
	result, err := agent.Call[factsResult](ctx, s.cli.command(dir), "drupdater:site-update", siteRequest{Site: site})
	if err != nil {
		return nil, fmt.Errorf("failed to update site %s: %w", site, err)
	}
	return facts(result, site)
}

// ExportConfiguration exports the site's configuration and returns the directory it wrote,
// relative to dir.
func (s *Sites) ExportConfiguration(ctx context.Context, dir string, site string) (string, error) {
	result, err := agent.Call[struct {
		Path string `json:"path"`
	}](ctx, s.cli.command(dir), "drupdater:config-export", siteRequest{Site: site})
	if err != nil {
		return "", fmt.Errorf("failed to export the configuration of site %s: %w", site, err)
	}
	return result.Path, nil
}

// Install installs the site from its exported configuration, leaving settings.php as it was.
func (s *Sites) Install(ctx context.Context, dir string, site string) error {
	return s.database(ctx, dir, site, "drupdater:site-install")
}

// ConfigureDatabase points the site at its database. Pair it with RestoreSettings: settings.php
// is tracked, and nothing may commit it in between.
func (s *Sites) ConfigureDatabase(ctx context.Context, dir string, site string) error {
	return s.database(ctx, dir, site, "drupdater:site-database")
}

// RestoreSettings leaves settings.php byte for byte as the project has it.
func (s *Sites) RestoreSettings(ctx context.Context, dir string, site string) error {
	return s.database(ctx, dir, site, "drupdater:site-restore")
}

func (s *Sites) database(ctx context.Context, dir string, site string, command string) error {
	_, err := agent.Call[struct{}](ctx, s.cli.command(dir), command, siteRequest{Site: site, DataDir: s.dataDir})
	return err
}

// facts refuses anything but an object: the addons decode a site's facts as one.
func facts(result factsResult, site string) (json.RawMessage, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(result.Facts), []byte("{")) {
		return nil, fmt.Errorf("failed to read the facts of site %s: %w", site, errors.New("the agent answered no facts"))
	}
	return result.Facts, nil
}
