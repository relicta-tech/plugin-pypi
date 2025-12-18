// Package main provides tests for the PyPI plugin.
package main

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

// MockCommandExecutor is a mock implementation of CommandExecutor for testing.
type MockCommandExecutor struct {
	RunFunc     func(ctx context.Context, name string, args ...string) ([]byte, error)
	RunCalls    []MockRunCall
	ReturnError error
	ReturnOut   []byte
}

// MockRunCall records a call to Run.
type MockRunCall struct {
	Name string
	Args []string
}

// Run implements CommandExecutor.
func (m *MockCommandExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.RunCalls = append(m.RunCalls, MockRunCall{Name: name, Args: args})
	if m.RunFunc != nil {
		return m.RunFunc(ctx, name, args...)
	}
	return m.ReturnOut, m.ReturnError
}

func TestGetInfo(t *testing.T) {
	p := &PyPIPlugin{}
	info := p.GetInfo()

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"name", info.Name, "pypi"},
		{"version", info.Version, "2.0.0"},
		{"description", info.Description, "Publish packages to PyPI (Python Package Index)"},
		{"author", info.Author, "Relicta Team"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("expected %s '%s', got '%s'", tt.name, tt.expected, tt.got)
			}
		})
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
	tests := []struct {
		name      string
		config    map[string]any
		envVars   map[string]string
		wantValid bool
		wantError string
	}{
		{
			name:      "missing credentials",
			config:    map[string]any{},
			wantValid: false,
			wantError: "username",
		},
		{
			name: "missing password",
			config: map[string]any{
				"username": "testuser",
			},
			wantValid: false,
			wantError: "password",
		},
		{
			name: "missing username",
			config: map[string]any{
				"password": "testpass",
			},
			wantValid: false,
			wantError: "username",
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
		{
			name: "invalid repository - http non-localhost",
			config: map[string]any{
				"username":   "testuser",
				"password":   "testpass",
				"repository": "http://pypi.example.com/legacy/",
			},
			wantValid: false,
			wantError: "only HTTPS",
		},
		{
			name: "valid localhost http repository",
			config: map[string]any{
				"username":   "testuser",
				"password":   "testpass",
				"repository": "http://localhost:8080/legacy/",
			},
			wantValid: true,
		},
		{
			name: "invalid dist_path - path traversal",
			config: map[string]any{
				"username":  "testuser",
				"password":  "testpass",
				"dist_path": "../../../etc/passwd",
			},
			wantValid: false,
			wantError: "path traversal",
		},
		{
			name: "invalid dist_path - absolute path",
			config: map[string]any{
				"username":  "testuser",
				"password":  "testpass",
				"dist_path": "/etc/passwd",
			},
			wantValid: false,
			wantError: "absolute paths",
		},
		{
			name: "invalid dist_path - invalid characters",
			config: map[string]any{
				"username":  "testuser",
				"password":  "testpass",
				"dist_path": "dist/$(rm -rf /)",
			},
			wantValid: false,
			wantError: "invalid characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PyPIPlugin{}
			ctx := context.Background()

			// Clear env vars first
			_ = os.Unsetenv("PYPI_USERNAME")
			_ = os.Unsetenv("PYPI_PASSWORD")

			// Set env vars
			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
				defer func(key string) { _ = os.Unsetenv(key) }(k)
			}

			resp, err := p.Validate(ctx, tt.config)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Valid != tt.wantValid {
				t.Errorf("expected valid=%v, got valid=%v, errors=%v", tt.wantValid, resp.Valid, resp.Errors)
			}

			if !tt.wantValid && tt.wantError != "" {
				found := false
				for _, e := range resp.Errors {
					if strings.Contains(e.Message, tt.wantError) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing '%s', got errors=%v", tt.wantError, resp.Errors)
				}
			}
		})
	}
}

func TestParseConfig(t *testing.T) {
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
			p := &PyPIPlugin{}

			// Clear env vars first
			_ = os.Unsetenv("PYPI_USERNAME")
			_ = os.Unsetenv("PYPI_PASSWORD")

			// Set env vars
			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
				defer func(key string) { _ = os.Unsetenv(key) }(k)
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
	tests := []struct {
		name            string
		config          map[string]any
		releaseCtx      plugin.ReleaseContext
		expectedOutputs map[string]any
		expectContains  string
		expectedSuccess bool
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
			expectedOutputs: map[string]any{
				"repository":    "https://upload.pypi.org/legacy/",
				"dist_path":     "dist/*",
				"skip_existing": false,
				"version":       "1.2.3",
			},
			expectContains:  "Would upload package",
			expectedSuccess: true,
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
			expectedOutputs: map[string]any{
				"repository":    "https://test.pypi.org/legacy/",
				"dist_path":     "dist/*",
				"skip_existing": false,
				"version":       "2.0.0",
			},
			expectContains:  "Would upload package to https://test.pypi.org/legacy/",
			expectedSuccess: true,
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
			expectedOutputs: map[string]any{
				"repository":    "https://test.pypi.org/legacy/",
				"dist_path":     "build/dist/*",
				"skip_existing": true,
				"version":       "3.1.0",
			},
			expectContains:  "Would upload package",
			expectedSuccess: true,
		},
		{
			name: "dry run version without v prefix",
			config: map[string]any{
				"username": "testuser",
				"password": "testpass",
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "4.5.6",
			},
			expectedOutputs: map[string]any{
				"version": "4.5.6",
			},
			expectContains:  "Would upload package",
			expectedSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PyPIPlugin{}
			ctx := context.Background()

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

			if resp.Success != tt.expectedSuccess {
				t.Errorf("expected success=%v, got success=%v, error: %s", tt.expectedSuccess, resp.Success, resp.Error)
			}

			if !strings.Contains(resp.Message, tt.expectContains) {
				t.Errorf("expected message to contain '%s', got '%s'", tt.expectContains, resp.Message)
			}

			for key, expected := range tt.expectedOutputs {
				if got, ok := resp.Outputs[key]; !ok {
					t.Errorf("expected output key '%s' not found", key)
				} else if got != expected {
					t.Errorf("output '%s': expected '%v', got '%v'", key, expected, got)
				}
			}
		})
	}
}

func TestExecuteUnhandledHook(t *testing.T) {
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
			p := &PyPIPlugin{}
			ctx := context.Background()

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
	tests := []struct {
		name           string
		config         map[string]any
		releaseCtx     plugin.ReleaseContext
		mockOutput     []byte
		mockError      error
		expectedArgs   []string
		expectSuccess  bool
		expectContains string
	}{
		{
			name: "successful upload",
			config: map[string]any{
				"username": "testuser",
				"password": "testpass",
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			mockOutput:     []byte("Uploading distributions to https://upload.pypi.org/legacy/\nUploading mypackage-1.0.0.tar.gz\n"),
			mockError:      nil,
			expectedArgs:   []string{"upload", "--repository-url", "https://upload.pypi.org/legacy/", "-u", "testuser", "-p", "testpass", "dist/*"},
			expectSuccess:  true,
			expectContains: "Successfully uploaded",
		},
		{
			name: "upload with skip existing",
			config: map[string]any{
				"username":      "testuser",
				"password":      "testpass",
				"skip_existing": true,
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v2.0.0",
			},
			mockOutput:     []byte("Uploading distributions..."),
			mockError:      nil,
			expectedArgs:   []string{"upload", "--repository-url", "https://upload.pypi.org/legacy/", "-u", "testuser", "-p", "testpass", "--skip-existing", "dist/*"},
			expectSuccess:  true,
			expectContains: "Successfully uploaded",
		},
		{
			name: "upload with custom dist path",
			config: map[string]any{
				"username":  "testuser",
				"password":  "testpass",
				"dist_path": "build/dist/*.whl",
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v3.0.0",
			},
			mockOutput:     []byte("Uploading distributions..."),
			mockError:      nil,
			expectedArgs:   []string{"upload", "--repository-url", "https://upload.pypi.org/legacy/", "-u", "testuser", "-p", "testpass", "build/dist/*.whl"},
			expectSuccess:  true,
			expectContains: "Successfully uploaded",
		},
		{
			name: "upload with custom repository",
			config: map[string]any{
				"username":   "testuser",
				"password":   "testpass",
				"repository": "https://test.pypi.org/legacy/",
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v4.0.0",
			},
			mockOutput:     []byte("Uploading distributions..."),
			mockError:      nil,
			expectedArgs:   []string{"upload", "--repository-url", "https://test.pypi.org/legacy/", "-u", "testuser", "-p", "testpass", "dist/*"},
			expectSuccess:  true,
			expectContains: "Successfully uploaded",
		},
		{
			name: "twine upload fails",
			config: map[string]any{
				"username": "testuser",
				"password": "testpass",
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			mockOutput:     []byte("HTTPError: 400 Bad Request"),
			mockError:      errors.New("exit status 1"),
			expectedArgs:   []string{"upload", "--repository-url", "https://upload.pypi.org/legacy/", "-u", "testuser", "-p", "testpass", "dist/*"},
			expectSuccess:  false,
			expectContains: "twine upload failed",
		},
		{
			name: "upload with all options",
			config: map[string]any{
				"username":      "testuser",
				"password":      "testpass",
				"repository":    "http://localhost:9999/",
				"dist_path":     "output/*.tar.gz",
				"skip_existing": true,
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v5.0.0",
			},
			mockOutput:     []byte("Success!"),
			mockError:      nil,
			expectedArgs:   []string{"upload", "--repository-url", "http://localhost:9999/", "-u", "testuser", "-p", "testpass", "--skip-existing", "output/*.tar.gz"},
			expectSuccess:  true,
			expectContains: "Successfully uploaded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockExecutor := &MockCommandExecutor{
				ReturnOut:   tt.mockOutput,
				ReturnError: tt.mockError,
			}
			p := &PyPIPlugin{cmdExecutor: mockExecutor}
			ctx := context.Background()

			req := plugin.ExecuteRequest{
				Hook:    plugin.HookPostPublish,
				Config:  tt.config,
				Context: tt.releaseCtx,
				DryRun:  false,
			}

			resp, err := p.Execute(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Success != tt.expectSuccess {
				t.Errorf("expected success=%v, got success=%v, error: %s", tt.expectSuccess, resp.Success, resp.Error)
			}

			if tt.expectSuccess {
				if !strings.Contains(resp.Message, tt.expectContains) {
					t.Errorf("expected message to contain '%s', got '%s'", tt.expectContains, resp.Message)
				}
			} else {
				if !strings.Contains(resp.Error, tt.expectContains) {
					t.Errorf("expected error to contain '%s', got '%s'", tt.expectContains, resp.Error)
				}
			}

			// Verify twine was called with correct arguments
			if len(mockExecutor.RunCalls) != 1 {
				t.Fatalf("expected 1 Run call, got %d", len(mockExecutor.RunCalls))
			}

			call := mockExecutor.RunCalls[0]
			if call.Name != "twine" {
				t.Errorf("expected command 'twine', got '%s'", call.Name)
			}

			if len(call.Args) != len(tt.expectedArgs) {
				t.Errorf("expected %d args, got %d: %v", len(tt.expectedArgs), len(call.Args), call.Args)
			} else {
				for i, expected := range tt.expectedArgs {
					if call.Args[i] != expected {
						t.Errorf("arg[%d]: expected '%s', got '%s'", i, expected, call.Args[i])
					}
				}
			}
		})
	}
}

func TestExecuteConfigValidation(t *testing.T) {
	tests := []struct {
		name          string
		config        map[string]any
		expectSuccess bool
		expectError   string
	}{
		{
			name: "missing username",
			config: map[string]any{
				"password": "testpass",
			},
			expectSuccess: false,
			expectError:   "username is required",
		},
		{
			name: "missing password",
			config: map[string]any{
				"username": "testuser",
			},
			expectSuccess: false,
			expectError:   "password is required",
		},
		{
			name: "invalid repository URL - non-https",
			config: map[string]any{
				"username":   "testuser",
				"password":   "testpass",
				"repository": "http://evil.com/",
			},
			expectSuccess: false,
			expectError:   "only HTTPS",
		},
		{
			name: "invalid dist_path - path traversal",
			config: map[string]any{
				"username":  "testuser",
				"password":  "testpass",
				"dist_path": "../../../etc/passwd",
			},
			expectSuccess: false,
			expectError:   "path traversal",
		},
		{
			name: "invalid dist_path - absolute path",
			config: map[string]any{
				"username":  "testuser",
				"password":  "testpass",
				"dist_path": "/etc/passwd",
			},
			expectSuccess: false,
			expectError:   "absolute paths",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env vars to ensure config validation works correctly
			_ = os.Unsetenv("PYPI_USERNAME")
			_ = os.Unsetenv("PYPI_PASSWORD")

			p := &PyPIPlugin{}
			ctx := context.Background()

			req := plugin.ExecuteRequest{
				Hook:    plugin.HookPostPublish,
				Config:  tt.config,
				Context: plugin.ReleaseContext{Version: "v1.0.0"},
				DryRun:  false,
			}

			resp, err := p.Execute(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Success != tt.expectSuccess {
				t.Errorf("expected success=%v, got success=%v", tt.expectSuccess, resp.Success)
			}

			if !tt.expectSuccess {
				if !strings.Contains(resp.Error, tt.expectError) {
					t.Errorf("expected error to contain '%s', got '%s'", tt.expectError, resp.Error)
				}
			}
		})
	}
}

func TestBuildTwineArgs(t *testing.T) {
	tests := []struct {
		name         string
		config       Config
		expectedArgs []string
	}{
		{
			name: "basic args",
			config: Config{
				Repository: "https://upload.pypi.org/legacy/",
				Username:   "user",
				Password:   "pass",
				DistPath:   "dist/*",
			},
			expectedArgs: []string{"upload", "--repository-url", "https://upload.pypi.org/legacy/", "-u", "user", "-p", "pass", "dist/*"},
		},
		{
			name: "with skip existing",
			config: Config{
				Repository:   "https://upload.pypi.org/legacy/",
				Username:     "user",
				Password:     "pass",
				DistPath:     "dist/*",
				SkipExisting: true,
			},
			expectedArgs: []string{"upload", "--repository-url", "https://upload.pypi.org/legacy/", "-u", "user", "-p", "pass", "--skip-existing", "dist/*"},
		},
		{
			name: "custom repository and dist path",
			config: Config{
				Repository: "https://test.pypi.org/legacy/",
				Username:   "testuser",
				Password:   "testpass",
				DistPath:   "build/output/*.whl",
			},
			expectedArgs: []string{"upload", "--repository-url", "https://test.pypi.org/legacy/", "-u", "testuser", "-p", "testpass", "build/output/*.whl"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PyPIPlugin{}
			args := p.buildTwineArgs(tt.config)

			if len(args) != len(tt.expectedArgs) {
				t.Fatalf("expected %d args, got %d: %v", len(tt.expectedArgs), len(args), args)
			}

			for i, expected := range tt.expectedArgs {
				if args[i] != expected {
					t.Errorf("arg[%d]: expected '%s', got '%s'", i, expected, args[i])
				}
			}
		})
	}
}

func TestValidateRepositoryURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid https URL",
			url:     "https://upload.pypi.org/legacy/",
			wantErr: false,
		},
		{
			name:    "valid test pypi URL",
			url:     "https://test.pypi.org/legacy/",
			wantErr: false,
		},
		{
			name:    "valid localhost http URL",
			url:     "http://localhost:8080/simple/",
			wantErr: false,
		},
		{
			name:    "valid 127.0.0.1 http URL",
			url:     "http://127.0.0.1:9000/",
			wantErr: false,
		},
		{
			name:        "empty URL",
			url:         "",
			wantErr:     true,
			errContains: "cannot be empty",
		},
		{
			name:        "http non-localhost URL",
			url:         "http://pypi.example.com/",
			wantErr:     true,
			errContains: "only HTTPS",
		},
		{
			name:        "ftp URL",
			url:         "ftp://pypi.org/",
			wantErr:     true,
			errContains: "only HTTPS",
		},
		{
			name:        "file URL",
			url:         "file:///etc/passwd",
			wantErr:     true,
			errContains: "only HTTPS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRepositoryURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateDistPath(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid default path",
			path:    "dist/*",
			wantErr: false,
		},
		{
			name:    "valid wheel path",
			path:    "dist/*.whl",
			wantErr: false,
		},
		{
			name:    "valid nested path",
			path:    "build/dist/*.tar.gz",
			wantErr: false,
		},
		{
			name:    "valid path with underscores and dashes",
			path:    "my_package-dist/output/*.whl",
			wantErr: false,
		},
		{
			name:        "empty path",
			path:        "",
			wantErr:     true,
			errContains: "cannot be empty",
		},
		{
			name:        "path traversal with leading ..",
			path:        "../../../etc/passwd",
			wantErr:     true,
			errContains: "path traversal",
		},
		{
			name:        "path traversal in middle",
			path:        "dist/../../../etc/passwd",
			wantErr:     true,
			errContains: "path traversal",
		},
		{
			name:        "absolute path unix",
			path:        "/etc/passwd",
			wantErr:     true,
			errContains: "absolute paths",
		},
		{
			name:        "shell injection attempt",
			path:        "dist/$(rm -rf /)*",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "command substitution backticks",
			path:        "dist/`whoami`/*",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "semicolon injection",
			path:        "dist/*; rm -rf /",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "pipe injection",
			path:        "dist/* | cat /etc/passwd",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "path too long",
			path:        strings.Repeat("a", 257),
			wantErr:     true,
			errContains: "too long",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDistPath(tt.path)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name      string
		ip        string
		isPrivate bool
	}{
		{"public IP", "8.8.8.8", false},
		{"google DNS", "8.8.4.4", false},
		{"cloudflare DNS", "1.1.1.1", false},
		{"private 10.x", "10.0.0.1", true},
		{"private 172.16.x", "172.16.0.1", true},
		{"private 172.31.x", "172.31.255.255", true},
		{"private 192.168.x", "192.168.1.1", true},
		{"loopback", "127.0.0.1", true},
		{"link local", "169.254.1.1", true},
		{"aws metadata", "169.254.169.254", true},
		{"ipv6 loopback", "::1", true},
		{"ipv6 public", "2607:f8b0:4004:800::200e", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := parseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP: %s", tt.ip)
			}
			result := isPrivateIP(ip)
			if result != tt.isPrivate {
				t.Errorf("isPrivateIP(%s) = %v, expected %v", tt.ip, result, tt.isPrivate)
			}
		})
	}
}

// parseIP is a helper for testing that parses an IP string.
func parseIP(s string) []byte {
	return []byte(net.ParseIP(s))
}

func TestGetExecutor(t *testing.T) {
	t.Run("returns custom executor when set", func(t *testing.T) {
		mockExecutor := &MockCommandExecutor{}
		p := &PyPIPlugin{cmdExecutor: mockExecutor}

		exec := p.getExecutor()
		if exec != mockExecutor {
			t.Error("expected custom executor to be returned")
		}
	})

	t.Run("returns RealCommandExecutor when not set", func(t *testing.T) {
		p := &PyPIPlugin{}

		exec := p.getExecutor()
		if _, ok := exec.(*RealCommandExecutor); !ok {
			t.Error("expected RealCommandExecutor to be returned")
		}
	})
}
