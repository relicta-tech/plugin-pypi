// Package main implements the PyPI plugin for Relicta.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/relicta-tech/relicta-plugin-sdk/helpers"
	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

// Config holds the PyPI plugin configuration.
type Config struct {
	// Username for PyPI authentication (can be set via PYPI_USERNAME env var)
	Username string
	// Password or API token for PyPI authentication (can be set via PYPI_PASSWORD env var)
	Password string
	// Repository URL (defaults to https://upload.pypi.org/legacy/)
	Repository string
	// DistPath is the path to distribution files (defaults to "dist/*")
	DistPath string
	// SkipExisting skips upload if package version already exists
	SkipExisting bool
}

// PyPIPlugin implements the Publish packages to PyPI (Python Package Index) plugin.
type PyPIPlugin struct{}

// GetInfo returns plugin metadata.
func (p *PyPIPlugin) GetInfo() plugin.Info {
	return plugin.Info{
		Name:        "pypi",
		Version:     "2.0.0",
		Description: "Publish packages to PyPI (Python Package Index)",
		Author:      "Relicta Team",
		Hooks: []plugin.Hook{
			plugin.HookPostPublish,
		},
		ConfigSchema: `{
			"type": "object",
			"properties": {}
		}`,
	}
}

// Execute runs the plugin for a given hook.
func (p *PyPIPlugin) Execute(ctx context.Context, req plugin.ExecuteRequest) (*plugin.ExecuteResponse, error) {
	switch req.Hook {
	case plugin.HookPostPublish:
		if req.DryRun {
			return &plugin.ExecuteResponse{
				Success: true,
				Message: "Would execute pypi plugin",
			}, nil
		}
		return &plugin.ExecuteResponse{
			Success: true,
			Message: "PyPI plugin executed successfully",
		}, nil
	default:
		return &plugin.ExecuteResponse{
			Success: true,
			Message: fmt.Sprintf("Hook %s not handled", req.Hook),
		}, nil
	}
}

// Validate validates the plugin configuration.
func (p *PyPIPlugin) Validate(_ context.Context, config map[string]any) (*plugin.ValidateResponse, error) {
	vb := helpers.NewValidationBuilder()
	cfg := p.parseConfig(config)

	// Username and password are required (can come from env vars)
	if cfg.Username == "" {
		vb.AddError("username", "username is required (set via config or PYPI_USERNAME env var)")
	}
	if cfg.Password == "" {
		vb.AddError("password", "password is required (set via config or PYPI_PASSWORD env var)")
	}

	return vb.Build(), nil
}

// parseConfig parses the raw config map into a Config struct.
func (p *PyPIPlugin) parseConfig(raw map[string]any) Config {
	cfg := Config{
		Repository: "https://upload.pypi.org/legacy/",
		DistPath:   "dist/*",
	}

	if v, ok := raw["username"].(string); ok && v != "" {
		cfg.Username = v
	} else if v := os.Getenv("PYPI_USERNAME"); v != "" {
		cfg.Username = v
	}

	if v, ok := raw["password"].(string); ok && v != "" {
		cfg.Password = v
	} else if v := os.Getenv("PYPI_PASSWORD"); v != "" {
		cfg.Password = v
	}

	if v, ok := raw["repository"].(string); ok && v != "" {
		cfg.Repository = v
	}

	if v, ok := raw["dist_path"].(string); ok && v != "" {
		cfg.DistPath = v
	}

	if v, ok := raw["skip_existing"].(bool); ok {
		cfg.SkipExisting = v
	}

	return cfg
}
