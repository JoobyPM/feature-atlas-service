package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_Defaults(t *testing.T) {
	cfg := New()

	assert.Equal(t, Version, cfg.Version)
	assert.Equal(t, ModeAtlas, cfg.Mode)

	// Atlas defaults
	assert.Equal(t, DefaultAtlasServer, cfg.Atlas.ServerURL)
	assert.Equal(t, DefaultAtlasCert, cfg.Atlas.Cert)
	assert.Equal(t, DefaultAtlasKey, cfg.Atlas.Key)
	assert.Equal(t, DefaultAtlasCA, cfg.Atlas.CACert)

	// GitLab defaults
	assert.Equal(t, DefaultGitLabInstance, cfg.GitLab.Instance)
	assert.Equal(t, DefaultGitLabBranch, cfg.GitLab.MainBranch)
	assert.Equal(t, []string{"feature"}, cfg.GitLab.MRLabels)
	assert.True(t, cfg.GitLab.MRRemoveSourceBranch)

	// Cache defaults
	assert.Equal(t, DefaultCacheTTL, cfg.Cache.TTL)
}

func TestLoad_FromFile(t *testing.T) {
	// Create temp config file
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	configContent := `
version: "1"
mode: gitlab
gitlab:
  instance: https://gitlab.example.com
  project: mygroup/myproject
  main_branch: develop
cache:
  ttl: 30m
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	cfg, err := Load(LoadOptions{
		ExplicitPath: configPath,
		SkipEnv:      true,
	})
	require.NoError(t, err)

	assert.Equal(t, ModeGitLab, cfg.Mode)
	assert.Equal(t, "https://gitlab.example.com", cfg.GitLab.Instance)
	assert.Equal(t, "mygroup/myproject", cfg.GitLab.Project)
	assert.Equal(t, "develop", cfg.GitLab.MainBranch)
	assert.Equal(t, "30m", cfg.Cache.TTL)

	// Defaults should still be present for unspecified fields
	assert.Equal(t, DefaultAtlasServer, cfg.Atlas.ServerURL)
}

func TestLoad_EnvOverrides(t *testing.T) {
	// Create temp config file with base values
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	configContent := `
mode: atlas
gitlab:
  instance: https://gitlab.example.com
  project: base/project
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	// Set env vars
	t.Setenv(EnvMode, "GITLAB")
	t.Setenv(EnvGitLabInstance, "https://gitlab.override.com")
	t.Setenv(EnvGitLabProject, "override/project")
	t.Setenv(EnvGitLabToken, "test-token")
	t.Setenv(EnvCacheTTL, "2h")

	cfg, err := Load(LoadOptions{
		ExplicitPath: configPath,
	})
	require.NoError(t, err)

	// Env vars should override file values
	assert.Equal(t, ModeGitLab, cfg.Mode)
	assert.Equal(t, "https://gitlab.override.com", cfg.GitLab.Instance)
	assert.Equal(t, "override/project", cfg.GitLab.Project)
	assert.Equal(t, "test-token", cfg.GitLab.Token)
	assert.Equal(t, "2h", cfg.Cache.TTL)
}

func TestLoad_CIJobToken(t *testing.T) {
	t.Run("CI_JOB_TOKEN used when no explicit token", func(t *testing.T) {
		t.Setenv(EnvCIJobToken, "ci-job-token-value")

		cfg, err := Load(LoadOptions{SkipGlobal: true, SkipProject: true})
		require.NoError(t, err)

		assert.Equal(t, "ci-job-token-value", cfg.GitLab.Token)
	})

	t.Run("explicit token takes priority over CI_JOB_TOKEN", func(t *testing.T) {
		t.Setenv(EnvCIJobToken, "ci-job-token-value")
		t.Setenv(EnvGitLabToken, "explicit-token")

		cfg, err := Load(LoadOptions{SkipGlobal: true, SkipProject: true})
		require.NoError(t, err)

		assert.Equal(t, "explicit-token", cfg.GitLab.Token)
	})
}

func TestApplyCLIOverrides(t *testing.T) {
	cfg := New()

	cfg.ApplyCLIOverrides(CLIOverrides{
		Mode:           "gitlab",
		AtlasServer:    "https://custom.server:9999",
		GitLabInstance: "https://custom.gitlab.com",
		GitLabProject:  "cli/project",
	})

	assert.Equal(t, ModeGitLab, cfg.Mode)
	assert.Equal(t, "https://custom.server:9999", cfg.Atlas.ServerURL)
	assert.Equal(t, "https://custom.gitlab.com", cfg.GitLab.Instance)
	assert.Equal(t, "cli/project", cfg.GitLab.Project)

	// Empty values should not override
	cfg.ApplyCLIOverrides(CLIOverrides{})
	assert.Equal(t, ModeGitLab, cfg.Mode) // Should remain unchanged
}

func TestValidate(t *testing.T) {
	t.Run("valid atlas mode", func(t *testing.T) {
		cfg := New()
		cfg.Mode = ModeAtlas
		assert.NoError(t, cfg.Validate())
	})

	t.Run("valid gitlab mode", func(t *testing.T) {
		cfg := New()
		cfg.Mode = ModeGitLab
		cfg.GitLab.Project = "group/project"
		assert.NoError(t, cfg.Validate())
	})

	t.Run("invalid mode", func(t *testing.T) {
		cfg := New()
		cfg.Mode = "invalid"
		err := cfg.Validate()
		assert.ErrorIs(t, err, ErrInvalidMode)
	})

	t.Run("gitlab mode requires project", func(t *testing.T) {
		cfg := New()
		cfg.Mode = ModeGitLab
		cfg.GitLab.Project = ""
		err := cfg.Validate()
		assert.ErrorIs(t, err, ErrNoProject)
	})

	t.Run("invalid cache TTL", func(t *testing.T) {
		cfg := New()
		cfg.Cache.TTL = "invalid-duration"
		err := cfg.Validate()
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})
}

func TestCacheTTLDuration(t *testing.T) {
	t.Run("valid duration", func(t *testing.T) {
		cfg := New()
		cfg.Cache.TTL = "30m"
		assert.Equal(t, 30*time.Minute, cfg.CacheTTLDuration())
	})

	t.Run("empty uses default", func(t *testing.T) {
		cfg := New()
		cfg.Cache.TTL = ""
		expected, _ := time.ParseDuration(DefaultCacheTTL)
		assert.Equal(t, expected, cfg.CacheTTLDuration())
	})

	t.Run("invalid uses default", func(t *testing.T) {
		cfg := New()
		cfg.Cache.TTL = "invalid"
		expected, _ := time.ParseDuration(DefaultCacheTTL)
		assert.Equal(t, expected, cfg.CacheTTLDuration())
	})
}

func TestIsMode(t *testing.T) {
	cfg := New()

	cfg.Mode = ModeAtlas
	assert.True(t, cfg.IsAtlasMode())
	assert.False(t, cfg.IsGitLabMode())

	cfg.Mode = ModeGitLab
	assert.False(t, cfg.IsAtlasMode())
	assert.True(t, cfg.IsGitLabMode())
}

func TestString_RedactsToken(t *testing.T) {
	cfg := New()
	cfg.GitLab.Token = "super-secret-token"

	output := cfg.String()
	assert.NotContains(t, output, "super-secret-token")
	assert.Contains(t, output, "[REDACTED]")
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test-config.yaml")

	// Create config
	cfg := New()
	cfg.Mode = ModeGitLab
	cfg.GitLab.Instance = "https://gitlab.example.com"
	cfg.GitLab.Project = "test/project"
	cfg.GitLab.Token = "should-not-be-saved"
	cfg.Cache.TTL = "45m"

	// Save
	err := cfg.SaveTo(configPath)
	require.NoError(t, err)

	// Verify file permissions
	info, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// Load back
	loaded, err := Load(LoadOptions{
		ExplicitPath: configPath,
		SkipEnv:      true,
	})
	require.NoError(t, err)

	assert.Equal(t, ModeGitLab, loaded.Mode)
	assert.Equal(t, "https://gitlab.example.com", loaded.GitLab.Instance)
	assert.Equal(t, "test/project", loaded.GitLab.Project)
	assert.Empty(t, loaded.GitLab.Token, "token should not be saved")
	assert.Equal(t, "45m", loaded.Cache.TTL)
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "invalid.yaml")

	err := os.WriteFile(configPath, []byte("invalid: yaml: content: ["), 0o600)
	require.NoError(t, err)

	_, err = Load(LoadOptions{
		ExplicitPath: configPath,
		SkipEnv:      true,
	})
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load(LoadOptions{
		ExplicitPath: "/nonexistent/path/config.yaml",
		SkipEnv:      true,
	})
	assert.Error(t, err)
}

func TestDiscoverProjectConfig(t *testing.T) {
	// Create temp directory structure with .featctl.yaml
	dir := t.TempDir()
	configPath := filepath.Join(dir, ProjectConfigFile)
	err := os.WriteFile(configPath, []byte("mode: gitlab"), 0o600)
	require.NoError(t, err)

	// Create subdirectory
	subdir := filepath.Join(dir, "subdir", "nested")
	err = os.MkdirAll(subdir, 0o750)
	require.NoError(t, err)

	// Change to subdirectory
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()

	err = os.Chdir(subdir)
	require.NoError(t, err)

	// Should find config in parent
	found, err := discoverProjectConfig()
	require.NoError(t, err)

	// Resolve symlinks for comparison (macOS /var -> /private/var)
	expectedResolved, _ := filepath.EvalSymlinks(configPath)
	foundResolved, _ := filepath.EvalSymlinks(found)
	assert.Equal(t, expectedResolved, foundResolved)
}

func TestConfigPrecedence_Full(t *testing.T) {
	// This test verifies the full precedence chain:
	// CLI flags > env vars > project config > global config > defaults

	dir := t.TempDir()

	// Create "global" config
	globalDir := filepath.Join(dir, "global")
	err := os.MkdirAll(globalDir, 0o750)
	require.NoError(t, err)
	globalPath := filepath.Join(globalDir, "config.yaml")
	err = os.WriteFile(globalPath, []byte(`
mode: atlas
gitlab:
  instance: https://global.gitlab.com
  project: global/project
cache:
  ttl: 10m
`), 0o600)
	require.NoError(t, err)

	// Create "project" config
	projectPath := filepath.Join(dir, ProjectConfigFile)
	err = os.WriteFile(projectPath, []byte(`
gitlab:
  instance: https://project.gitlab.com
cache:
  ttl: 20m
`), 0o600)
	require.NoError(t, err)

	// Set env var (will override project)
	t.Setenv(EnvCacheTTL, "30m")

	// Start with defaults
	cfg := New()

	// Load global (simulated - normally done by Load)
	err = loadFile(cfg, globalPath)
	require.NoError(t, err)
	assert.Equal(t, "https://global.gitlab.com", cfg.GitLab.Instance)
	assert.Equal(t, "10m", cfg.Cache.TTL)

	// Load project (should override global)
	err = loadFile(cfg, projectPath)
	require.NoError(t, err)
	assert.Equal(t, "https://project.gitlab.com", cfg.GitLab.Instance)
	assert.Equal(t, "20m", cfg.Cache.TTL)
	assert.Equal(t, "global/project", cfg.GitLab.Project) // Preserved from global

	// Apply env (should override project)
	applyEnvOverrides(cfg)
	assert.Equal(t, "30m", cfg.Cache.TTL)
	assert.Equal(t, "https://project.gitlab.com", cfg.GitLab.Instance) // Unchanged

	// Apply CLI (should override everything)
	cfg.ApplyCLIOverrides(CLIOverrides{
		CacheTTL: "40m",
	})
	assert.Equal(t, "40m", cfg.Cache.TTL)
}
