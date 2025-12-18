// Package main provides tests for the PyPI plugin.
package main

import (
	"context"
	"os"
	"testing"

	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

func TestGetInfo(t *testing.T) {
	p := &PyPIPlugin{}
	info := p.GetInfo()

	if info.Name != "pypi" {
		t.Errorf("expected name 'pypi', got '%s'", info.Name)
	}

	if info.Version != "2.0.0" {
		t.Errorf("expected version '2.0.0', got '%s'", info.Version)
	}

	if info.Description != "Publish packages to PyPI (Python Package Index)" {
		t.Errorf("expected description 'Publish packages to PyPI (Python Package Index)', got '%s'", info.Description)
	}

	if info.Author != "Relicta Team" {
		t.Errorf("expected author 'Relicta Team', got '%s'", info.Author)
	}

	// Check hooks
	if len(info.Hooks) == 0 {
		t.Error("expected at least one hook")
	}

	hasPostPublish := false
	for _, hook := range info.Hooks {
		if hook == plugin.HookPostPublish {
			hasPostPublish = true
			break
		}
	}
	if !hasPostPublish {
		t.Error("expected PostPublish hook")
	}

	// Check config schema is valid JSON
	if info.ConfigSchema == "" {
		t.Error("expected non-empty config schema")
	}
}

func TestValidate(t *testing.T) {
	p := &PyPIPlugin{}
	ctx := context.Background()

	tests := []struct {
		name      string
		config    map[string]any
		envVars   map[string]string
		wantValid bool
	}{
		{
			name:      "missing credentials",
			config:    map[string]any{},
			wantValid: false,
		},
		{
			name: "missing password",
			config: map[string]any{
				"username": "testuser",
			},
			wantValid: false,
		},
		{
			name: "missing username",
			config: map[string]any{
				"password": "testpass",
			},
			wantValid: false,
		},
		{
			name: "valid config with credentials",
			config: map[string]any{
				"username": "testuser",
				"password": "testpass",
			},
			wantValid: true,
		},
		{
			name:   "valid config with env vars",
			config: map[string]any{},
			envVars: map[string]string{
				"PYPI_USERNAME": "envuser",
				"PYPI_PASSWORD": "envpass",
			},
			wantValid: true,
		},
		{
			name: "valid config with all options",
			config: map[string]any{
				"username":      "testuser",
				"password":      "testpass",
				"repository":    "https://test.pypi.org/legacy/",
				"dist_path":     "build/dist/*",
				"skip_existing": true,
			},
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env vars first
			os.Unsetenv("PYPI_USERNAME")
			os.Unsetenv("PYPI_PASSWORD")

			// Set env vars
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			resp, err := p.Validate(ctx, tt.config)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Valid != tt.wantValid {
				t.Errorf("expected valid=%v, got valid=%v, errors=%v", tt.wantValid, resp.Valid, resp.Errors)
			}
		})
	}
}

func TestParseConfig(t *testing.T) {
	p := &PyPIPlugin{}

	tests := []struct {
		name     string
		config   map[string]any
		envVars  map[string]string
		expected Config
	}{
		{
			name:   "defaults",
			config: map[string]any{},
			expected: Config{
				Repository: "https://upload.pypi.org/legacy/",
				DistPath:   "dist/*",
			},
		},
		{
			name: "custom values",
			config: map[string]any{
				"username":      "customuser",
				"password":      "custompass",
				"repository":    "https://test.pypi.org/legacy/",
				"dist_path":     "build/dist/*",
				"skip_existing": true,
			},
			expected: Config{
				Username:     "customuser",
				Password:     "custompass",
				Repository:   "https://test.pypi.org/legacy/",
				DistPath:     "build/dist/*",
				SkipExisting: true,
			},
		},
		{
			name:   "env var fallback",
			config: map[string]any{},
			envVars: map[string]string{
				"PYPI_USERNAME": "envuser",
				"PYPI_PASSWORD": "envpass",
			},
			expected: Config{
				Username:   "envuser",
				Password:   "envpass",
				Repository: "https://upload.pypi.org/legacy/",
				DistPath:   "dist/*",
			},
		},
		{
			name: "config overrides env vars",
			config: map[string]any{
				"username": "configuser",
				"password": "configpass",
			},
			envVars: map[string]string{
				"PYPI_USERNAME": "envuser",
				"PYPI_PASSWORD": "envpass",
			},
			expected: Config{
				Username:   "configuser",
				Password:   "configpass",
				Repository: "https://upload.pypi.org/legacy/",
				DistPath:   "dist/*",
			},
		},
		{
			name: "partial config with env var fallback",
			config: map[string]any{
				"username": "configuser",
			},
			envVars: map[string]string{
				"PYPI_PASSWORD": "envpass",
			},
			expected: Config{
				Username:   "configuser",
				Password:   "envpass",
				Repository: "https://upload.pypi.org/legacy/",
				DistPath:   "dist/*",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env vars first
			os.Unsetenv("PYPI_USERNAME")
			os.Unsetenv("PYPI_PASSWORD")

			// Set env vars
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			cfg := p.parseConfig(tt.config)

			if cfg.Username != tt.expected.Username {
				t.Errorf("username: expected '%s', got '%s'", tt.expected.Username, cfg.Username)
			}
			if cfg.Password != tt.expected.Password {
				t.Errorf("password: expected '%s', got '%s'", tt.expected.Password, cfg.Password)
			}
			if cfg.Repository != tt.expected.Repository {
				t.Errorf("repository: expected '%s', got '%s'", tt.expected.Repository, cfg.Repository)
			}
			if cfg.DistPath != tt.expected.DistPath {
				t.Errorf("dist_path: expected '%s', got '%s'", tt.expected.DistPath, cfg.DistPath)
			}
			if cfg.SkipExisting != tt.expected.SkipExisting {
				t.Errorf("skip_existing: expected %v, got %v", tt.expected.SkipExisting, cfg.SkipExisting)
			}
		})
	}
}

func TestExecuteDryRun(t *testing.T) {
	p := &PyPIPlugin{}
	ctx := context.Background()

	tests := []struct {
		name            string
		config          map[string]any
		releaseCtx      plugin.ReleaseContext
		expectedMessage string
	}{
		{
			name: "basic dry run",
			config: map[string]any{
				"username": "testuser",
				"password": "testpass",
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.2.3",
			},
			expectedMessage: "Would execute pypi plugin",
		},
		{
			name: "dry run with custom repository",
			config: map[string]any{
				"username":   "testuser",
				"password":   "testpass",
				"repository": "https://test.pypi.org/legacy/",
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v2.0.0",
			},
			expectedMessage: "Would execute pypi plugin",
		},
		{
			name: "dry run with all options",
			config: map[string]any{
				"username":      "testuser",
				"password":      "testpass",
				"repository":    "https://test.pypi.org/legacy/",
				"dist_path":     "build/dist/*",
				"skip_existing": true,
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v3.1.0",
			},
			expectedMessage: "Would execute pypi plugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := plugin.ExecuteRequest{
				Hook:    plugin.HookPostPublish,
				Config:  tt.config,
				Context: tt.releaseCtx,
				DryRun:  true,
			}

			resp, err := p.Execute(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !resp.Success {
				t.Errorf("expected success, got error: %s", resp.Error)
			}

			if resp.Message != tt.expectedMessage {
				t.Errorf("expected message '%s', got '%s'", tt.expectedMessage, resp.Message)
			}
		})
	}
}

func TestExecuteUnhandledHook(t *testing.T) {
	p := &PyPIPlugin{}
	ctx := context.Background()

	tests := []struct {
		name            string
		hook            plugin.Hook
		expectedSuccess bool
	}{
		{
			name:            "PreInit hook",
			hook:            plugin.HookPreInit,
			expectedSuccess: true,
		},
		{
			name:            "PreVersion hook",
			hook:            plugin.HookPreVersion,
			expectedSuccess: true,
		},
		{
			name:            "PostVersion hook",
			hook:            plugin.HookPostVersion,
			expectedSuccess: true,
		},
		{
			name:            "PreNotes hook",
			hook:            plugin.HookPreNotes,
			expectedSuccess: true,
		},
		{
			name:            "PostNotes hook",
			hook:            plugin.HookPostNotes,
			expectedSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := plugin.ExecuteRequest{
				Hook: tt.hook,
				Config: map[string]any{
					"username": "testuser",
					"password": "testpass",
				},
				DryRun: true,
			}

			resp, err := p.Execute(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Success != tt.expectedSuccess {
				t.Errorf("expected success=%v, got success=%v", tt.expectedSuccess, resp.Success)
			}

			expectedMessagePrefix := "Hook"
			if len(resp.Message) < len(expectedMessagePrefix) || resp.Message[:len(expectedMessagePrefix)] != expectedMessagePrefix {
				t.Errorf("expected message to start with '%s', got '%s'", expectedMessagePrefix, resp.Message)
			}
		})
	}
}

func TestExecuteActualRun(t *testing.T) {
	p := &PyPIPlugin{}
	ctx := context.Background()

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"username": "testuser",
			"password": "testpass",
		},
		Context: plugin.ReleaseContext{
			Version: "v1.0.0",
		},
		DryRun: false,
	}

	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	expectedMessage := "PyPI plugin executed successfully"
	if resp.Message != expectedMessage {
		t.Errorf("expected message '%s', got '%s'", expectedMessage, resp.Message)
	}
}
