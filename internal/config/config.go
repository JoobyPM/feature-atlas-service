// Package config provides configuration management for featctl.
// Configuration is loaded from YAML files with environment variable overrides.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Version is the current config schema version.
const Version = "1"

// Mode constants for backend selection.
const (
	ModeAtlas  = "atlas"
	ModeGitLab = "gitlab"
)

// Default file paths.
const (
	GlobalConfigDir   = ".config/featctl"
	GlobalConfigFile  = "config.yaml"
	ProjectConfigFile = ".featctl.yaml"
)

// Default values.
const (
	DefaultMode           = ModeAtlas
	DefaultAtlasServer    = "https://localhost:8443"
	DefaultAtlasCert      = "certs/alice.crt"
	DefaultAtlasKey       = "certs/alice.key"
	DefaultAtlasCA        = "certs/ca.crt"
	DefaultGitLabInstance = "https://gitlab.com"
	DefaultGitLabBranch   = "main"
	DefaultCacheTTL       = "1h"
)

// Environment variable names.
const (
	EnvMode           = "FEATCTL_MODE"
	EnvAtlasServer    = "FEATCTL_ATLAS_SERVER"
	EnvGitLabInstance = "FEATCTL_GITLAB_INSTANCE"
	EnvGitLabProject  = "FEATCTL_GITLAB_PROJECT"
	EnvGitLabToken    = "FEATCTL_GITLAB_TOKEN" //nolint:gosec // Env var name, not a credential
	EnvGitLabClientID = "FEATCTL_GITLAB_CLIENT_ID"
	EnvCacheTTL       = "FEATCTL_CACHE_TTL"
	EnvCIJobToken     = "CI_JOB_TOKEN" //nolint:gosec // Env var name, not a credential
)

// Config represents the complete featctl configuration.
type Config struct {
	Version string       `yaml:"version"`
	Mode    string       `yaml:"mode"`
	Atlas   AtlasConfig  `yaml:"atlas"`
	GitLab  GitLabConfig `yaml:"gitlab"`
	Cache   CacheConfig  `yaml:"cache"`
}

// AtlasConfig holds Atlas backend settings.
type AtlasConfig struct {
	ServerURL string `yaml:"server_url" json:"server_url"`
	Cert      string `yaml:"cert" json:"cert"`
	Key       string `yaml:"key" json:"key"`
	CACert    string `yaml:"ca_cert" json:"ca_cert"`
}

// GitLabConfig holds GitLab backend settings.
type GitLabConfig struct {
	Instance             string   `yaml:"instance"`
	Project              string   `yaml:"project"`
	MainBranch           string   `yaml:"main_branch"`
	OAuthClientID        string   `yaml:"oauth_client_id"`
	MRLabels             []string `yaml:"mr_labels"`
	MRRemoveSourceBranch bool     `yaml:"mr_remove_source_branch"`
	DefaultAssignee      string   `yaml:"default_assignee"`
	// Token is not stored in config files - loaded from env or keyring only
	Token string `yaml:"-"`
}

// CacheConfig holds cache settings.
type CacheConfig struct {
	TTL string `yaml:"ttl" json:"ttl"`
	Dir string `yaml:"dir" json:"dir"`
}

// Errors.
var (
	ErrInvalidMode   = errors.New("invalid mode: must be 'atlas' or 'gitlab'")
	ErrInvalidConfig = errors.New("invalid configuration")
	ErrNoProject     = errors.New("gitlab.project is required in gitlab mode")
)

// New creates a Config with default values.
func New() *Config {
	return &Config{
		Version: Version,
		Mode:    DefaultMode,
		Atlas: AtlasConfig{
			ServerURL: DefaultAtlasServer,
			Cert:      DefaultAtlasCert,
			Key:       DefaultAtlasKey,
			CACert:    DefaultAtlasCA,
		},
		GitLab: GitLabConfig{
			Instance:             DefaultGitLabInstance,
			MainBranch:           DefaultGitLabBranch,
			MRLabels:             []string{"feature"},
			MRRemoveSourceBranch: true,
		},
		Cache: CacheConfig{
			TTL: DefaultCacheTTL,
		},
	}
}

// LoadOptions configures config loading behavior.
type LoadOptions struct {
	// ExplicitPath overrides config discovery (--config flag).
	ExplicitPath string
	// SkipGlobal skips loading global config (~/.config/featctl/config.yaml).
	SkipGlobal bool
	// SkipProject skips loading project config (.featctl.yaml).
	SkipProject bool
	// SkipEnv skips environment variable overrides.
	SkipEnv bool
}

// Load loads configuration with the following precedence (highest to lowest):
// 1. Environment variables
// 2. Project config (.featctl.yaml in repo root)
// 3. Global config (~/.config/featctl/config.yaml)
// 4. Built-in defaults
//
// If ExplicitPath is set, it replaces both global and project configs.
func Load(opts LoadOptions) (*Config, error) {
	cfg := New()

	// Load global config (lowest priority file)
	if !opts.SkipGlobal && opts.ExplicitPath == "" {
		globalPath, err := globalConfigPath()
		if err == nil {
			if loadErr := loadFile(cfg, globalPath); loadErr != nil && !os.IsNotExist(loadErr) {
				return nil, fmt.Errorf("load global config: %w", loadErr)
			}
		}
	}

	// Load project config (higher priority than global)
	if !opts.SkipProject && opts.ExplicitPath == "" {
		projectPath, err := discoverProjectConfig()
		if err == nil {
			if loadErr := loadFile(cfg, projectPath); loadErr != nil && !os.IsNotExist(loadErr) {
				return nil, fmt.Errorf("load project config: %w", loadErr)
			}
		}
	}

	// Load explicit config (replaces global and project)
	if opts.ExplicitPath != "" {
		if err := loadFile(cfg, opts.ExplicitPath); err != nil {
			return nil, fmt.Errorf("load config %s: %w", opts.ExplicitPath, err)
		}
	}

	// Apply environment variable overrides (highest file priority)
	if !opts.SkipEnv {
		applyEnvOverrides(cfg)
	}

	return cfg, nil
}

// loadFile reads and unmarshals a YAML config file into cfg.
// Fields not present in the file retain their current values (merge behavior).
func loadFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // Config path from trusted source
	if err != nil {
		return err
	}

	// Unmarshal into existing config (partial merge)
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	return nil
}

// globalConfigPath returns the path to the global config file.
func globalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, GlobalConfigDir, GlobalConfigFile), nil
}

// discoverProjectConfig walks up from CWD looking for .featctl.yaml.
// Stops at git root or filesystem root.
func discoverProjectConfig() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := cwd
	for {
		path := filepath.Join(dir, ProjectConfigFile)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}

		// Stop at git root
		gitPath := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			break
		}

		// Move to parent
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", os.ErrNotExist
}

// applyEnvOverrides applies environment variable overrides to config.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv(EnvMode); v != "" {
		cfg.Mode = strings.ToLower(v)
	}

	// Atlas
	if v := os.Getenv(EnvAtlasServer); v != "" {
		cfg.Atlas.ServerURL = v
	}

	// GitLab
	if v := os.Getenv(EnvGitLabInstance); v != "" {
		cfg.GitLab.Instance = v
	}
	if v := os.Getenv(EnvGitLabProject); v != "" {
		cfg.GitLab.Project = v
	}
	if v := os.Getenv(EnvGitLabToken); v != "" {
		cfg.GitLab.Token = v
	}
	if v := os.Getenv(EnvGitLabClientID); v != "" {
		cfg.GitLab.OAuthClientID = v
	}

	// CI_JOB_TOKEN is auto-detected (lower priority than explicit token)
	if cfg.GitLab.Token == "" {
		if v := os.Getenv(EnvCIJobToken); v != "" {
			cfg.GitLab.Token = v
		}
	}

	// Cache
	if v := os.Getenv(EnvCacheTTL); v != "" {
		cfg.Cache.TTL = v
	}
}

// CLIOverrides contains values from CLI flags that override config.
type CLIOverrides struct {
	Mode           string
	AtlasServer    string
	AtlasCert      string
	AtlasKey       string
	AtlasCA        string
	GitLabInstance string
	GitLabProject  string
	CacheTTL       string
}

// ApplyCLIOverrides applies CLI flag values to config.
// Only non-empty values are applied (highest priority).
func (cfg *Config) ApplyCLIOverrides(o CLIOverrides) {
	if o.Mode != "" {
		cfg.Mode = strings.ToLower(o.Mode)
	}
	if o.AtlasServer != "" {
		cfg.Atlas.ServerURL = o.AtlasServer
	}
	if o.AtlasCert != "" {
		cfg.Atlas.Cert = o.AtlasCert
	}
	if o.AtlasKey != "" {
		cfg.Atlas.Key = o.AtlasKey
	}
	if o.AtlasCA != "" {
		cfg.Atlas.CACert = o.AtlasCA
	}
	if o.GitLabInstance != "" {
		cfg.GitLab.Instance = o.GitLabInstance
	}
	if o.GitLabProject != "" {
		cfg.GitLab.Project = o.GitLabProject
	}
	if o.CacheTTL != "" {
		cfg.Cache.TTL = o.CacheTTL
	}
}

// Validate checks the configuration for errors.
func (cfg *Config) Validate() error {
	// Validate mode
	switch cfg.Mode {
	case ModeAtlas, ModeGitLab:
		// Valid
	default:
		return fmt.Errorf("%w: got %q", ErrInvalidMode, cfg.Mode)
	}

	// Mode-specific validation
	if cfg.Mode == ModeGitLab && cfg.GitLab.Project == "" {
		return ErrNoProject
	}

	// Validate cache TTL format
	if cfg.Cache.TTL != "" {
		if _, err := time.ParseDuration(cfg.Cache.TTL); err != nil {
			return fmt.Errorf("%w: invalid cache.ttl %q: %w", ErrInvalidConfig, cfg.Cache.TTL, err)
		}
	}

	return nil
}

// defaultCacheTTLDuration is the parsed default cache TTL.
// We parse it once at package init time since the default is a constant.
var defaultCacheTTLDuration = mustParseDuration(DefaultCacheTTL)

func mustParseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		panic("invalid default duration: " + s)
	}
	return d
}

// CacheTTLDuration returns the cache TTL as a time.Duration.
// Returns DefaultCacheTTL parsed if TTL is empty or invalid.
func (cfg *Config) CacheTTLDuration() time.Duration {
	if cfg.Cache.TTL == "" {
		return defaultCacheTTLDuration
	}
	d, err := time.ParseDuration(cfg.Cache.TTL)
	if err != nil {
		return defaultCacheTTLDuration
	}
	return d
}

// IsGitLabMode returns true if the config is set to GitLab mode.
func (cfg *Config) IsGitLabMode() bool {
	return cfg.Mode == ModeGitLab
}

// IsAtlasMode returns true if the config is set to Atlas mode.
func (cfg *Config) IsAtlasMode() bool {
	return cfg.Mode == ModeAtlas
}

// configDisplay is used for String() output with token field included.
type configDisplay struct {
	Version string              `yaml:"version"`
	Mode    string              `yaml:"mode"`
	Atlas   AtlasConfig         `yaml:"atlas"`
	GitLab  gitLabConfigDisplay `yaml:"gitlab"`
	Cache   CacheConfig         `yaml:"cache"`
}

type gitLabConfigDisplay struct {
	Instance             string   `yaml:"instance"`
	Project              string   `yaml:"project"`
	MainBranch           string   `yaml:"main_branch"`
	OAuthClientID        string   `yaml:"oauth_client_id"`
	MRLabels             []string `yaml:"mr_labels"`
	MRRemoveSourceBranch bool     `yaml:"mr_remove_source_branch"`
	DefaultAssignee      string   `yaml:"default_assignee"`
	Token                string   `yaml:"token,omitempty"`
}

// String returns a human-readable representation of the config.
// Sensitive fields (tokens) are redacted.
func (cfg *Config) String() string {
	// Create display struct with token field visible but redacted
	display := configDisplay{
		Version: cfg.Version,
		Mode:    cfg.Mode,
		Atlas:   cfg.Atlas,
		GitLab: gitLabConfigDisplay{
			Instance:             cfg.GitLab.Instance,
			Project:              cfg.GitLab.Project,
			MainBranch:           cfg.GitLab.MainBranch,
			OAuthClientID:        cfg.GitLab.OAuthClientID,
			MRLabels:             cfg.GitLab.MRLabels,
			MRRemoveSourceBranch: cfg.GitLab.MRRemoveSourceBranch,
			DefaultAssignee:      cfg.GitLab.DefaultAssignee,
		},
		Cache: cfg.Cache,
	}

	if cfg.GitLab.Token != "" {
		display.GitLab.Token = "[REDACTED]"
	}

	data, err := yaml.Marshal(&display)
	if err != nil {
		return fmt.Sprintf("config error: %v", err)
	}
	return string(data)
}

// SaveGlobal writes the config to the global config file.
// Creates the directory if it doesn't exist.
func (cfg *Config) SaveGlobal() error {
	path, err := globalConfigPath()
	if err != nil {
		return fmt.Errorf("get global config path: %w", err)
	}

	return cfg.SaveTo(path)
}

// SaveTo writes the config to the specified path.
// Creates parent directories if needed. Tokens are NOT saved.
func (cfg *Config) SaveTo(path string) error {
	// Create a copy without sensitive data
	saveCfg := *cfg
	saveCfg.GitLab.Token = "" // Never save token to file

	data, err := yaml.Marshal(&saveCfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	// Write with restrictive permissions (config may contain OAuth client ID)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

// DiscoveredPaths returns which config files were found.
// Useful for debugging configuration issues.
// Returns empty strings for paths that don't exist or can't be determined.
func DiscoveredPaths() (global, project string) {
	globalPath, err := globalConfigPath()
	if err == nil {
		if _, statErr := os.Stat(globalPath); statErr == nil {
			global = globalPath
		}
	}
	projectPath, err := discoverProjectConfig()
	if err == nil {
		project = projectPath
	}
	return global, project
}
