// Package main implements the PyPI plugin for Relicta.
package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/relicta-tech/relicta-plugin-sdk/helpers"
	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

// Security validation patterns.
var (
	// distPathPattern validates dist path patterns - allows alphanumerics, dots, dashes, underscores, forward slashes, and glob patterns.
	distPathPattern = regexp.MustCompile(`^[a-zA-Z0-9._/*-]+$`)
)

// CommandExecutor abstracts command execution for testability.
type CommandExecutor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// RealCommandExecutor executes real shell commands.
type RealCommandExecutor struct{}

// Run executes a command and returns combined output.
func (e *RealCommandExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

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
type PyPIPlugin struct {
	// cmdExecutor is used for executing shell commands. If nil, uses RealCommandExecutor.
	cmdExecutor CommandExecutor
}

// getExecutor returns the command executor, defaulting to RealCommandExecutor.
func (p *PyPIPlugin) getExecutor() CommandExecutor {
	if p.cmdExecutor != nil {
		return p.cmdExecutor
	}
	return &RealCommandExecutor{}
}

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
			"properties": {
				"username": {"type": "string", "description": "PyPI username (or use PYPI_USERNAME env)"},
				"password": {"type": "string", "description": "PyPI password or API token (or use PYPI_PASSWORD env)"},
				"repository": {"type": "string", "description": "Repository URL", "default": "https://upload.pypi.org/legacy/"},
				"dist_path": {"type": "string", "description": "Path to distribution files", "default": "dist/*"},
				"skip_existing": {"type": "boolean", "description": "Skip upload if version exists", "default": false}
			},
			"required": []
		}`,
	}
}

// Execute runs the plugin for a given hook.
func (p *PyPIPlugin) Execute(ctx context.Context, req plugin.ExecuteRequest) (*plugin.ExecuteResponse, error) {
	cfg := p.parseConfig(req.Config)

	switch req.Hook {
	case plugin.HookPostPublish:
		return p.uploadPackage(ctx, cfg, req.Context, req.DryRun)
	default:
		return &plugin.ExecuteResponse{
			Success: true,
			Message: fmt.Sprintf("Hook %s not handled", req.Hook),
		}, nil
	}
}

// uploadPackage executes twine upload with the configured options.
func (p *PyPIPlugin) uploadPackage(ctx context.Context, cfg Config, releaseCtx plugin.ReleaseContext, dryRun bool) (*plugin.ExecuteResponse, error) {
	// Validate configuration
	if err := p.validateConfig(cfg); err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("configuration validation failed: %v", err),
		}, nil
	}

	version := strings.TrimPrefix(releaseCtx.Version, "v")

	if dryRun {
		return &plugin.ExecuteResponse{
			Success: true,
			Message: fmt.Sprintf("Would upload package to %s", cfg.Repository),
			Outputs: map[string]any{
				"repository":    cfg.Repository,
				"dist_path":     cfg.DistPath,
				"skip_existing": cfg.SkipExisting,
				"version":       version,
			},
		}, nil
	}

	// Build twine command arguments
	args := p.buildTwineArgs(cfg)

	// Execute twine upload
	executor := p.getExecutor()
	output, err := executor.Run(ctx, "twine", args...)
	if err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("twine upload failed: %v\nOutput: %s", err, string(output)),
		}, nil
	}

	return &plugin.ExecuteResponse{
		Success: true,
		Message: fmt.Sprintf("Successfully uploaded package to %s", cfg.Repository),
		Outputs: map[string]any{
			"repository": cfg.Repository,
			"dist_path":  cfg.DistPath,
			"version":    version,
			"output":     string(output),
		},
	}, nil
}

// buildTwineArgs constructs the command line arguments for twine upload.
func (p *PyPIPlugin) buildTwineArgs(cfg Config) []string {
	args := []string{"upload"}

	// Repository URL
	args = append(args, "--repository-url", cfg.Repository)

	// Username and password
	args = append(args, "-u", cfg.Username)
	args = append(args, "-p", cfg.Password)

	// Skip existing if enabled
	if cfg.SkipExisting {
		args = append(args, "--skip-existing")
	}

	// Distribution path
	args = append(args, cfg.DistPath)

	return args
}

// validateConfig performs security validation on the configuration.
func (p *PyPIPlugin) validateConfig(cfg Config) error {
	// Validate repository URL
	if err := validateRepositoryURL(cfg.Repository); err != nil {
		return fmt.Errorf("invalid repository URL: %w", err)
	}

	// Validate dist path
	if err := validateDistPath(cfg.DistPath); err != nil {
		return fmt.Errorf("invalid dist path: %w", err)
	}

	// Validate credentials are present
	if cfg.Username == "" {
		return fmt.Errorf("username is required")
	}
	if cfg.Password == "" {
		return fmt.Errorf("password is required")
	}

	return nil
}

// validateRepositoryURL validates that a repository URL is safe (SSRF protection).
func validateRepositoryURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("repository URL cannot be empty")
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	host := parsedURL.Hostname()

	// Allow localhost for testing purposes (HTTP is allowed only for localhost/127.0.0.1)
	isLocalhost := host == "localhost" || host == "127.0.0.1" || host == "::1"

	// Require HTTPS for non-localhost URLs
	if parsedURL.Scheme != "https" && !isLocalhost {
		return fmt.Errorf("only HTTPS URLs are allowed (got %s)", parsedURL.Scheme)
	}

	// Allow HTTP only for localhost
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("only HTTP(S) URLs are allowed (got %s)", parsedURL.Scheme)
	}

	// For localhost, skip the private IP check (it's intentionally local)
	if isLocalhost {
		return nil
	}

	// Resolve hostname to check for private IPs
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("failed to resolve hostname: %w", err)
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("URLs pointing to private networks are not allowed")
		}
	}

	return nil
}

// isPrivateIP checks if an IP address is in a private/reserved range.
func isPrivateIP(ip net.IP) bool {
	// Private IPv4 ranges
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16", // Link-local
		"0.0.0.0/8",
	}

	// Cloud metadata endpoints
	cloudMetadata := []string{
		"169.254.169.254/32", // AWS/GCP/Azure metadata
		"fd00:ec2::254/128",  // AWS IMDSv2 IPv6
	}

	allRanges := append(privateRanges, cloudMetadata...)

	for _, cidr := range allRanges {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if block.Contains(ip) {
			return true
		}
	}

	// Check for IPv6 private ranges
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return true
	}

	return false
}

// validateDistPath validates that a distribution path is safe.
func validateDistPath(path string) error {
	if path == "" {
		return fmt.Errorf("dist path cannot be empty")
	}

	if len(path) > 256 {
		return fmt.Errorf("dist path too long (max 256 characters)")
	}

	// Check for valid characters
	if !distPathPattern.MatchString(path) {
		return fmt.Errorf("dist path contains invalid characters")
	}

	// Clean the path for traversal check
	cleaned := filepath.Clean(path)

	// Check for path traversal attempts (excluding glob patterns)
	pathWithoutGlob := strings.ReplaceAll(cleaned, "*", "")
	if strings.HasPrefix(pathWithoutGlob, "..") || strings.Contains(pathWithoutGlob, string(filepath.Separator)+"..") {
		return fmt.Errorf("path traversal detected: cannot use '..' to escape working directory")
	}

	// Check for absolute paths (potential escape from working directory)
	if filepath.IsAbs(path) {
		return fmt.Errorf("absolute paths are not allowed")
	}

	return nil
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

	// Validate repository URL
	if cfg.Repository != "" {
		if err := validateRepositoryURL(cfg.Repository); err != nil {
			vb.AddError("repository", err.Error())
		}
	}

	// Validate dist path
	if cfg.DistPath != "" {
		if err := validateDistPath(cfg.DistPath); err != nil {
			vb.AddError("dist_path", err.Error())
		}
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
