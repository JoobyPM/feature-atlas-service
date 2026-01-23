// Package main provides the CLI entry point for featctl.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/JoobyPM/feature-atlas-service/internal/apiclient"
	"github.com/JoobyPM/feature-atlas-service/internal/auth"
	"github.com/JoobyPM/feature-atlas-service/internal/backend"
	"github.com/JoobyPM/feature-atlas-service/internal/backend/atlas"
	gitlabbackend "github.com/JoobyPM/feature-atlas-service/internal/backend/gitlab"
	"github.com/JoobyPM/feature-atlas-service/internal/cache"
	"github.com/JoobyPM/feature-atlas-service/internal/config"
	"github.com/JoobyPM/feature-atlas-service/internal/manifest"
	"github.com/JoobyPM/feature-atlas-service/internal/stringutil"
	"github.com/JoobyPM/feature-atlas-service/internal/tui"
)

// Output format constants.
const (
	outputJSON = "json"
	outputYAML = "yaml"
	outputText = "text"
)

var (
	// Global flags (for backward compatibility with Atlas mode)
	serverURL string
	caFile    string
	certFile  string
	keyFile   string

	// New global flags for dual-mode support
	flagMode           string
	flagGitLabInstance string
	flagGitLabProject  string
	flagConfigPath     string

	// Search flags
	searchLimit  int
	searchOutput string

	// Get flags
	getOutput string

	// Lint flags
	minDescLength int
	lintOffline   bool
	lintManifest  string

	// Manifest flags
	manifestPath        string
	manifestForce       bool
	manifestOutput      string
	manifestUnsynced    bool
	manifestDryRun      bool
	manifestForceLocal  bool
	manifestForceRemote bool

	// TUI flags
	tuiSync     bool
	tuiManifest string

	// Feature create flags
	featureID      string
	featureName    string
	featureSummary string
	featureOwner   string
	featureTags    string

	// Config show flags
	configShowOutput string

	// Login flags
	loginToken string

	// Client instance (legacy, for backward compatibility)
	client *apiclient.Client

	// Active backend (lazy initialized based on config mode)
	activeBackend backend.FeatureBackend

	// Global config (loaded once, used by all commands)
	cfg *config.Config
)

// Exit codes per PRD specification.
// Commands use these semantically:
//   - exitValidation: invalid input, missing required fields, ID format error
//   - exitConflict: ID already exists, or external error (server/network)
//   - exitWrite: file system write failure, lock timeout
const (
	exitValidation = 1
	exitConflict   = 2
	exitWrite      = 3
)

// ExitError is an error that carries a specific exit code.
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string {
	return e.Message
}

// exitErr creates an ExitError with the given code and message.
func exitErr(code int, msg string) error {
	return &ExitError{Code: code, Message: msg}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		var exitError *ExitError
		if errors.As(err, &exitError) {
			os.Exit(exitError.Code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "featctl",
	Short: "Feature Atlas CLI - manage and explore the feature catalog",
	Long: `featctl is a command-line interface for the Feature Atlas service.
It uses mTLS for authentication and provides commands for searching,
browsing, and validating features.

Local manifest commands (manifest, feature) work offline.
Server commands (me, search, get, tui, lint) require mTLS connection.`,
}

// initConfig loads the configuration with proper precedence.
// Called early via PersistentPreRunE on root command.
func initConfig() error {
	if cfg != nil {
		return nil
	}

	var err error
	cfg, err = config.Load(config.LoadOptions{
		ExplicitPath: flagConfigPath,
	})
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Apply CLI flag overrides (highest priority)
	cfg.ApplyCLIOverrides(config.CLIOverrides{
		Mode:           flagMode,
		AtlasServer:    serverURL,
		AtlasCert:      certFile,
		AtlasKey:       keyFile,
		AtlasCA:        caFile,
		GitLabInstance: flagGitLabInstance,
		GitLabProject:  flagGitLabProject,
	})

	return nil
}

// initBackend creates the appropriate backend based on configuration.
// This is the preferred way to initialize - all new code should use this.
func initBackend() error {
	if activeBackend != nil {
		return nil
	}

	// Ensure config is loaded
	if err := initConfig(); err != nil {
		return err
	}

	// Validate config before creating backend
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	var err error
	switch cfg.Mode {
	case config.ModeAtlas:
		activeBackend, err = atlas.New(cfg.Atlas)
		if err != nil {
			return fmt.Errorf("failed to create atlas backend: %w", err)
		}
	case config.ModeGitLab:
		activeBackend, err = gitlabbackend.New(cfg.GitLab)
		if err != nil {
			return fmt.Errorf("failed to create gitlab backend: %w", err)
		}
	default:
		return fmt.Errorf("unknown mode: %s", cfg.Mode)
	}

	return nil
}

// initClient creates the API client. Called only for server commands.
// Uses config if available, falls back to direct flag values for backward compatibility.
// Deprecated: Use initBackend() instead for new code.
func initClient() error {
	if client != nil {
		return nil
	}

	// Ensure config is loaded
	if err := initConfig(); err != nil {
		return err
	}

	// Use config values (which already have CLI overrides applied)
	var err error
	client, err = apiclient.New(
		cfg.Atlas.ServerURL,
		cfg.Atlas.CACert,
		cfg.Atlas.Cert,
		cfg.Atlas.Key,
	)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Also initialize the backend wrapper for backward compatibility
	activeBackend = atlas.NewFromClient(client)

	return nil
}

var meCmd = &cobra.Command{
	Use:   "me",
	Short: "Show authenticated client information",
	PreRunE: func(_ *cobra.Command, _ []string) error {
		return initClient()
	},
	RunE: func(_ *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		info, err := client.Me(ctx)
		if err != nil {
			return err
		}

		fmt.Printf("Name:        %s\n", info.Name)
		fmt.Printf("Role:        %s\n", info.Role)
		fmt.Printf("Fingerprint: %s\n", info.Fingerprint)
		fmt.Printf("Subject:     %s\n", info.Subject)
		return nil
	},
}

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search features in the catalog",
	Args:  cobra.MaximumNArgs(1),
	PreRunE: func(_ *cobra.Command, _ []string) error {
		return initClient()
	},
	RunE: func(_ *cobra.Command, args []string) error {
		query := ""
		if len(args) > 0 {
			query = args[0]
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		features, err := client.Search(ctx, query, searchLimit)
		if err != nil {
			return err
		}

		switch searchOutput {
		case outputJSON:
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(features)
		case outputYAML:
			return yaml.NewEncoder(os.Stdout).Encode(features)
		default:
			for _, f := range features {
				fmt.Printf("%s  %s\n", f.ID, f.Name)
				fmt.Printf("    %s\n", stringutil.Truncate(f.Summary, 70))
				fmt.Println()
			}
		}
		return nil
	},
}

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Interactive terminal UI for browsing and selecting features",
	Long: `Launch an interactive terminal interface for searching and
selecting features with multi-select support.

Navigation:
  ↑/↓          Navigate list
  Space        Toggle selection
  n            Create new feature
  Ctrl+A       Select all visible
  Ctrl+N       Deselect all
  Enter        Confirm selection
  Esc          Clear search or quit

Type to search - all keys go to search input. Selections persist across
search changes - select items from different searches and confirm all
at once. Use --sync to push them to the server immediately.

Press 'n' to open a form for creating new features directly on the server.`,
	PreRunE: func(_ *cobra.Command, _ []string) error {
		return initBackend()
	},
	RunE: func(_ *cobra.Command, _ []string) error {
		// Build TUI options with manifest state
		opts, manifestLoaded, mPath, err := buildTUIOptions()
		if err != nil {
			return err
		}

		// Run TUI with backend
		result, tuiErr := tui.Run(activeBackend, opts)
		if tuiErr != nil {
			return tuiErr
		}

		// Handle cancellation
		if result.Cancelled || len(result.Selected) == 0 {
			return nil
		}

		// Add selected features to manifest
		if err := addSelectedToManifest(result.Selected, manifestLoaded, mPath); err != nil {
			return err
		}

		// Sync if requested
		if result.SyncRequested {
			return syncAfterTUI(mPath)
		}

		return nil
	},
}

// buildTUIOptions loads manifest state and builds TUI options.
func buildTUIOptions() (tui.Options, bool, string, error) {
	opts := tui.Options{
		ManifestFeatures: make(map[string]bool),
		LocalFeatures:    make(map[string]bool),
		SyncFlag:         tuiSync,
	}

	// Try to load cache for validation hints
	cacheDir, cacheErr := cache.ResolveDir()
	if cacheErr == nil {
		c := cache.New(cacheDir)
		if loadErr := c.Load(); loadErr == nil {
			opts.Cache = c
		}
		// If cache load fails, continue without cache (non-critical)
	}

	// Try to load manifest
	mPath, discoverErr := manifest.Discover(tuiManifest)
	if discoverErr != nil {
		if errors.Is(discoverErr, manifest.ErrManifestNotFound) {
			// No manifest - that's fine, but preserve explicit --manifest path
			if tuiManifest != "" {
				return opts, false, tuiManifest, nil
			}
			return opts, false, "", nil
		}
		return opts, false, "", fmt.Errorf("discover manifest: %w", discoverErr)
	}

	m, loadErr := manifest.Load(mPath)
	if loadErr != nil {
		if errors.Is(loadErr, manifest.ErrInvalidYAML) {
			fmt.Fprintf(os.Stderr, "Warning: manifest is corrupted, ignoring\n")
			return opts, false, "", nil
		}
		return opts, false, "", fmt.Errorf("load manifest: %w", loadErr)
	}

	// Pass manifest to TUI for feature creation
	opts.Manifest = m
	opts.ManifestPath = mPath

	// Populate manifest and local features
	for id, entry := range m.Features {
		opts.ManifestFeatures[id] = true
		if !entry.Synced {
			opts.LocalFeatures[id] = true
		}
	}

	return opts, true, mPath, nil
}

// addSelectedToManifest adds selected features to the manifest.
func addSelectedToManifest(selected []backend.SuggestItem, manifestLoaded bool, mPath string) error {
	// Determine manifest path
	path := mPath
	if path == "" {
		// Create new manifest in current directory
		path = manifest.DefaultFilename
	}

	// Load or create manifest
	var m *manifest.Manifest
	if manifestLoaded {
		var loadErr error
		m, loadErr = manifest.Load(path)
		if loadErr != nil {
			return fmt.Errorf("reload manifest: %w", loadErr)
		}
	} else {
		m = manifest.New()
	}

	// Add selected features
	var added int
	for _, item := range selected {
		// Skip if already in manifest
		if m.HasFeature(item.ID) {
			fmt.Printf("  • %s already in manifest (skipped)\n", item.ID)
			continue
		}

		// Add to manifest with synced status (features from server are synced)
		m.Features[item.ID] = manifest.Entry{
			Name:     item.Name,
			Summary:  item.Summary,
			Synced:   true,
			SyncedAt: time.Now().Format(time.RFC3339),
		}
		fmt.Printf("  ✓ Added %s (%s)\n", item.ID, item.Name)
		added++
	}

	if added == 0 {
		fmt.Println("No new features to add")
		return nil
	}

	// Save manifest
	if err := m.SaveWithLock(path); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	fmt.Printf("\n✓ Added %d feature(s) to %s\n", added, path)
	return nil
}

// syncAfterTUI syncs unsynced features after TUI selection.
func syncAfterTUI(mPath string) error {
	// Determine manifest path
	path := mPath
	if path == "" {
		var discoverErr error
		path, discoverErr = manifest.Discover(tuiManifest)
		if discoverErr != nil {
			return fmt.Errorf("discover manifest for sync: %w", discoverErr)
		}
	}

	// Load manifest
	m, err := manifest.Load(path)
	if err != nil {
		return fmt.Errorf("load manifest for sync: %w", err)
	}

	// Find unsynced features
	unsynced := m.ListFeatures(true)
	if len(unsynced) == 0 {
		fmt.Println("\nNo unsynced features to sync")
		return nil
	}

	// Sort for deterministic order
	ids := make([]string, 0, len(unsynced))
	for id := range unsynced {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	fmt.Printf("\nSyncing %d feature(s) to server...\n", len(ids))

	// Use activeBackend if available, otherwise fall back to legacy client
	useBackend := activeBackend != nil

	var synced, failed int
	for _, localID := range ids {
		entry := unsynced[localID]

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var feature *backend.Feature
		var createErr error

		if useBackend {
			feature, createErr = activeBackend.CreateFeature(ctx, backend.Feature{
				Name:    entry.Name,
				Summary: entry.Summary,
				Owner:   entry.Owner,
				Tags:    entry.Tags,
			})
		} else {
			// Legacy path for backward compatibility
			var apiFeature *apiclient.Feature
			apiFeature, createErr = client.CreateFeature(ctx, apiclient.CreateFeatureRequest{
				Name:    entry.Name,
				Summary: entry.Summary,
				Owner:   entry.Owner,
				Tags:    entry.Tags,
			})
			if apiFeature != nil {
				feature = &backend.Feature{
					ID:      apiFeature.ID,
					Name:    apiFeature.Name,
					Summary: apiFeature.Summary,
					Owner:   apiFeature.Owner,
					Tags:    apiFeature.Tags,
				}
			}
		}
		cancel()

		if createErr != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", localID, createErr)
			failed++
			continue
		}

		// Update manifest: remove old ID, add new with alias
		delete(m.Features, localID)
		m.Features[feature.ID] = manifest.Entry{
			Name:     feature.Name,
			Summary:  feature.Summary,
			Owner:    feature.Owner,
			Tags:     feature.Tags,
			Synced:   true,
			SyncedAt: time.Now().Format(time.RFC3339),
			Alias:    localID,
		}

		fmt.Printf("  ✓ %s → %s (%s)\n", localID, feature.ID, feature.Name)
		synced++
	}

	// Save manifest
	if err := m.SaveWithLock(path); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to save manifest: %v\n", err)
		if synced > 0 {
			fmt.Fprintf(os.Stderr, "Warning: %d feature(s) were created on server but manifest was not updated\n", synced)
		}
		return exitErr(exitWrite, "failed to save manifest")
	}

	fmt.Printf("\nSynced: %d, Failed: %d\n", synced, failed)

	if failed > 0 {
		return exitErr(exitValidation, "partial sync failure")
	}
	return nil
}

var getCmd = &cobra.Command{
	Use:   "get <feature-id>",
	Short: "Get a feature by ID",
	Args:  cobra.ExactArgs(1),
	PreRunE: func(_ *cobra.Command, _ []string) error {
		return initClient()
	},
	RunE: func(_ *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		feature, err := client.GetFeature(ctx, args[0])
		if err != nil {
			if errors.Is(err, apiclient.ErrFeatureNotFound) {
				return fmt.Errorf("feature not found: %s", args[0])
			}
			return err
		}

		switch getOutput {
		case outputJSON:
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(feature)
		case outputYAML:
			return yaml.NewEncoder(os.Stdout).Encode(feature)
		default:
			fmt.Printf("ID:      %s\n", feature.ID)
			fmt.Printf("Name:    %s\n", feature.Name)
			fmt.Printf("Summary: %s\n", feature.Summary)
			fmt.Printf("Owner:   %s\n", feature.Owner)
			fmt.Printf("Tags:    %s\n", strings.Join(feature.Tags, ", "))
		}
		return nil
	},
}

var lintCmd = &cobra.Command{
	Use:   "lint <file>",
	Short: "Validate a YAML file against the feature catalog",
	Long: `Lint validates that feature references in a YAML file exist in
the catalog. The YAML file should have a 'feature_id' field.

By default, lint checks the local manifest first, then falls back to the server.
Use --offline to only check the local manifest (no server connection).`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		data, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}

		var doc struct {
			FeatureID   string `yaml:"feature_id"`
			Description string `yaml:"description"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse YAML: %w", err)
		}

		var errs []string

		// Validate feature_id
		if doc.FeatureID == "" {
			errs = append(errs, "missing required field: feature_id")
		} else {
			found, checkErr := checkFeatureExists(doc.FeatureID)
			if checkErr != nil {
				return checkErr
			}
			if !found {
				errs = append(errs, fmt.Sprintf("feature_id '%s' not found in catalog", doc.FeatureID))
			}
		}

		// Validate description
		if len(doc.Description) < minDescLength {
			errs = append(errs, fmt.Sprintf("description must be at least %d characters (got %d)", minDescLength, len(doc.Description)))
		}

		if len(errs) > 0 {
			fmt.Fprintf(os.Stderr, "Validation failed for %s:\n", args[0])
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "  ✗ %s\n", e)
			}
			return exitErr(exitValidation, "validation failed")
		}

		fmt.Printf("✓ %s is valid\n", args[0])
		return nil
	},
}

// checkFeatureExists checks if a feature exists in manifest or server.
// Resolution order: manifest first, then server (unless --offline).
func checkFeatureExists(fid string) (bool, error) {
	// Try manifest first
	manifestLoaded := false
	mPath, discoverErr := manifest.Discover(lintManifest)
	if discoverErr == nil {
		m, loadErr := manifest.Load(mPath)
		if loadErr == nil {
			if m.HasFeature(fid) {
				return true, nil
			}
			manifestLoaded = true
		} else if !errors.Is(loadErr, manifest.ErrInvalidYAML) {
			// Real I/O error (permissions, etc.) - surface it
			return false, fmt.Errorf("load manifest: %w", loadErr)
		}
		// ErrInvalidYAML: manifest is corrupted, fall through to server check
	} else if !errors.Is(discoverErr, manifest.ErrManifestNotFound) {
		// Real discovery error (not just "not found") - surface it
		return false, fmt.Errorf("discover manifest: %w", discoverErr)
	}

	// If --offline, don't check server
	if lintOffline {
		if !manifestLoaded && errors.Is(discoverErr, manifest.ErrManifestNotFound) {
			return false, exitErr(exitValidation, "manifest not found (required for --offline)")
		}
		return false, nil
	}

	// Fall back to server
	if initErr := initClient(); initErr != nil {
		return false, fmt.Errorf("init client: %w", initErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, serverErr := client.FeatureExists(ctx, fid)
	if serverErr != nil {
		return false, fmt.Errorf("check feature: %w", serverErr)
	}
	return exists, nil
}

// manifestCmd is the parent command for manifest operations.
var manifestCmd = &cobra.Command{
	Use:   "manifest",
	Short: "Manage local feature manifest",
	Long: `The manifest command group manages the local feature catalog file
(.feature-atlas.yaml). These commands work offline without server connection.`,
}

var manifestInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a new manifest in the current directory",
	Long: `Initialize a new .feature-atlas.yaml manifest file in the current directory.
The manifest stores local feature definitions for offline validation.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		path := manifest.DefaultFilename
		if manifestPath != "" {
			path = manifestPath
		}

		// Check if exists
		if _, err := os.Stat(path); err == nil {
			if !manifestForce {
				fmt.Fprintf(os.Stderr, "Error: manifest already exists: %s\n", path)
				fmt.Fprintln(os.Stderr, "Use --force to overwrite")
				return exitErr(exitValidation, "manifest already exists")
			}
		}

		// Create new manifest
		m := manifest.New()
		if err := m.Save(path); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to write manifest: %v\n", err)
			return exitErr(exitWrite, "failed to write manifest")
		}

		fmt.Printf("✓ Created %s\n", path)
		return nil
	},
}

var manifestListCmd = &cobra.Command{
	Use:   "list",
	Short: "List features in the local manifest",
	RunE: func(_ *cobra.Command, _ []string) error {
		path, err := manifest.Discover(manifestPath)
		if err != nil {
			if errors.Is(err, manifest.ErrManifestNotFound) {
				fmt.Fprintln(os.Stderr, "Error: manifest not found")
				fmt.Fprintln(os.Stderr, "Run 'featctl manifest init' to create one")
				return exitErr(exitValidation, "manifest not found")
			}
			return err
		}

		m, err := manifest.Load(path)
		if err != nil {
			if errors.Is(err, manifest.ErrInvalidYAML) {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return exitErr(exitValidation, "invalid YAML")
			}
			return err
		}

		features := m.ListFeatures(manifestUnsynced)

		// Sort by ID for stable output
		ids := make([]string, 0, len(features))
		for id := range features {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		switch manifestOutput {
		case outputJSON:
			output := make(map[string]manifest.Entry)
			for _, id := range ids {
				output[id] = features[id]
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(output)
		case outputYAML:
			output := make(map[string]manifest.Entry)
			for _, id := range ids {
				output[id] = features[id]
			}
			return yaml.NewEncoder(os.Stdout).Encode(output)
		default:
			if len(ids) == 0 {
				fmt.Println("No features in manifest")
				return nil
			}

			fmt.Printf("Features in %s:\n\n", path)
			for _, id := range ids {
				entry := features[id]
				status := "[local]"
				if entry.Synced {
					status = "[synced]"
				}
				fmt.Printf("  %s  %s  %s\n", id, entry.Name, status)
				if entry.Summary != "" {
					fmt.Printf("      %s\n", stringutil.Truncate(entry.Summary, 60))
				}
			}
			fmt.Printf("\nTotal: %d feature(s)\n", len(ids))
		}
		return nil
	},
}

var manifestAddCmd = &cobra.Command{
	Use:   "add <feature-id>",
	Short: "Add a server feature to the local manifest",
	Long: `Fetch a feature from the server by ID and add it to the local manifest.
This allows offline validation of existing server features.

The feature ID must be a valid server ID format (FT-NNNNNN).
Requires mTLS connection to the server.`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(_ *cobra.Command, _ []string) error {
		return initClient()
	},
	RunE: func(_ *cobra.Command, args []string) error {
		targetID := args[0]

		// Validate server ID format
		if err := manifest.ValidateServerID(targetID); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintln(os.Stderr, "Server IDs must match format: FT-NNNNNN (e.g., FT-000123)")
			return exitErr(exitValidation, "invalid server ID format")
		}

		// Find manifest
		path, err := manifest.Discover(manifestPath)
		if err != nil {
			if errors.Is(err, manifest.ErrManifestNotFound) {
				fmt.Fprintln(os.Stderr, "Error: manifest not found")
				fmt.Fprintln(os.Stderr, "Run 'featctl manifest init' first")
				return exitErr(exitValidation, "manifest not found")
			}
			return err
		}

		// Load manifest
		m, err := manifest.Load(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			if errors.Is(err, manifest.ErrInvalidYAML) {
				return exitErr(exitValidation, "invalid manifest YAML")
			}
			return exitErr(exitValidation, "failed to load manifest")
		}

		// Check if already in manifest
		if m.HasFeature(targetID) {
			fmt.Printf("Feature %s already in manifest (skipped)\n", targetID)
			return nil
		}

		// Fetch from server
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		feature, err := client.GetFeature(ctx, targetID)
		if err != nil {
			if errors.Is(err, apiclient.ErrFeatureNotFound) {
				fmt.Fprintf(os.Stderr, "Error: feature not found on server: %s\n", targetID)
				return exitErr(exitValidation, "feature not found")
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return exitErr(exitConflict, "server error")
		}

		// Add to manifest with synced status
		m.Features[feature.ID] = manifest.Entry{
			Name:     feature.Name,
			Summary:  feature.Summary,
			Owner:    feature.Owner,
			Tags:     feature.Tags,
			Synced:   true,
			SyncedAt: time.Now().Format(time.RFC3339),
		}

		// Save
		if err := m.SaveWithLock(path); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to save manifest: %v\n", err)
			return exitErr(exitWrite, "failed to save manifest")
		}

		fmt.Printf("✓ Added %s (%s) to %s\n", feature.ID, feature.Name, path)
		return nil
	},
}

var manifestSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync local features with the remote backend",
	Long: `Sync local features with the remote backend.

For Atlas mode:
  Push all unsynced local features (FT-LOCAL-*) to the server.
  The server assigns canonical IDs (FT-NNNNNN) and the manifest is updated.
  Requires admin mTLS certificate.

For GitLab mode:
  Create merge requests for new local features.
  Check status of pending MRs and update local manifest when merged.
  Pull remote changes to local manifest.

Flags:
  --dry-run       Show what would be synced without making changes
  --force-local   Push local changes via MR (instead of pulling remote)
  --force-remote  Overwrite local changes with remote (no warning)`,
	PreRunE: func(_ *cobra.Command, _ []string) error {
		return initBackend()
	},
	RunE: func(_ *cobra.Command, _ []string) error {
		// Check mode
		if cfg.Mode == config.ModeGitLab {
			return syncGitLab()
		}
		return syncAtlas()
	},
}

// syncAtlas handles sync for Atlas backend.
func syncAtlas() error {
	// Find manifest
	path, err := manifest.Discover(manifestPath)
	if err != nil {
		if errors.Is(err, manifest.ErrManifestNotFound) {
			fmt.Fprintln(os.Stderr, "Error: manifest not found")
			fmt.Fprintln(os.Stderr, "Run 'featctl manifest init' first")
			return exitErr(exitValidation, "manifest not found")
		}
		return err
	}

	// Load manifest
	m, err := manifest.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		if errors.Is(err, manifest.ErrInvalidYAML) {
			return exitErr(exitValidation, "invalid manifest YAML")
		}
		return exitErr(exitValidation, "failed to load manifest")
	}

	// Find unsynced features
	unsynced := m.ListFeatures(true)
	if len(unsynced) == 0 {
		fmt.Println("No unsynced features to sync")
		return nil
	}

	// Sort for deterministic order
	ids := make([]string, 0, len(unsynced))
	for id := range unsynced {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	if manifestDryRun {
		fmt.Printf("Would sync %d feature(s):\n", len(ids))
		for _, id := range ids {
			entry := unsynced[id]
			fmt.Printf("  %s  %s\n", id, entry.Name)
		}
		return nil
	}

	fmt.Printf("Syncing %d feature(s) to server...\n", len(ids))

	var synced, failed int
	for _, localID := range ids {
		entry := unsynced[localID]

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		feature, createErr := activeBackend.CreateFeature(ctx, backend.Feature{
			Name:    entry.Name,
			Summary: entry.Summary,
			Owner:   entry.Owner,
			Tags:    entry.Tags,
		})
		cancel()

		if createErr != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", localID, createErr)
			failed++
			continue
		}

		// Update manifest: remove old ID, add new with alias
		delete(m.Features, localID)
		m.Features[feature.ID] = manifest.Entry{
			Name:     feature.Name,
			Summary:  feature.Summary,
			Owner:    feature.Owner,
			Tags:     feature.Tags,
			Synced:   true,
			SyncedAt: time.Now().Format(time.RFC3339),
			Alias:    localID,
		}

		fmt.Printf("  ✓ %s → %s (%s)\n", localID, feature.ID, feature.Name)
		synced++
	}

	// Save manifest
	if err := m.SaveWithLock(path); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to save manifest: %v\n", err)
		if synced > 0 {
			fmt.Fprintf(os.Stderr, "Warning: %d feature(s) were created on server but manifest was not updated\n", synced)
			fmt.Fprintln(os.Stderr, "Run 'featctl manifest add <id>' to manually add the server features")
		}
		return exitErr(exitWrite, "failed to save manifest")
	}

	fmt.Printf("\nSynced: %d, Failed: %d\n", synced, failed)

	if failed > 0 {
		return exitErr(exitValidation, "partial sync failure")
	}
	return nil
}

// syncGitLab handles sync for GitLab backend.
func syncGitLab() error {
	// Cast to GitLab backend
	glBackend, ok := activeBackend.(*gitlabbackend.Backend)
	if !ok {
		return exitErr(exitConflict, "invalid backend type for GitLab mode")
	}

	// Find manifest
	path, err := manifest.Discover(manifestPath)
	if err != nil {
		if errors.Is(err, manifest.ErrManifestNotFound) {
			fmt.Fprintln(os.Stderr, "Error: manifest not found")
			fmt.Fprintln(os.Stderr, "Run 'featctl manifest init' first")
			return exitErr(exitValidation, "manifest not found")
		}
		return err
	}

	// Load manifest
	m, err := manifest.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		if errors.Is(err, manifest.ErrInvalidYAML) {
			return exitErr(exitValidation, "invalid manifest YAML")
		}
		return exitErr(exitValidation, "failed to load manifest")
	}

	// Convert manifest features to LocalFeature map
	localFeatures := make(map[string]gitlabbackend.LocalFeature)
	for id, entry := range m.Features {
		var syncedAt time.Time
		if entry.SyncedAt != "" {
			parsed, parseErr := time.Parse(time.RFC3339, entry.SyncedAt)
			if parseErr == nil {
				syncedAt = parsed
			}
		}
		// Note: LocalFeature.UpdatedAt is left as zero since manifest.Entry doesn't track
		// local modification time. The sync logic uses SyncedAt for comparison with remote.
		localFeatures[id] = gitlabbackend.LocalFeature{
			Name:     entry.Name,
			Summary:  entry.Summary,
			Owner:    entry.Owner,
			Tags:     entry.Tags,
			Synced:   entry.Synced,
			SyncedAt: syncedAt,
			// UpdatedAt intentionally omitted (zero value) - manifest doesn't track local edits
		}
	}

	// Plan sync
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, planErr := glBackend.PlanSync(ctx, localFeatures, manifestForceLocal)
	if planErr != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to plan sync: %v\n", planErr)
		return exitErr(exitConflict, "failed to plan sync")
	}

	// Show warnings for new remote features
	for _, action := range result.Actions {
		if action.Type == gitlabbackend.ActionWarnNew {
			result.Warnings = append(result.Warnings, action.Description)
		}
	}

	// Dry run mode
	if manifestDryRun {
		fmt.Println("Sync plan (dry-run):")
		printSyncPlan(result)
		return nil
	}

	// Force remote mode - discard local changes
	if manifestForceRemote {
		return syncGitLabForceRemote(ctx, glBackend, m, path)
	}

	// Execute sync actions
	return executeSyncActions(ctx, glBackend, m, path, result)
}

// printSyncPlan displays the planned sync actions.
func printSyncPlan(result *gitlabbackend.SyncResult) {
	if len(result.Actions) == 0 {
		fmt.Println("  No actions needed")
		return
	}

	for _, action := range result.Actions {
		symbol := actionSymbol(action.Type)
		fmt.Printf("  %s %s\n", symbol, action.Description)
	}

	if len(result.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range result.Warnings {
			fmt.Printf("  ⚠ %s\n", w)
		}
	}
}

// actionSymbol returns a symbol for the action type.
func actionSymbol(t gitlabbackend.SyncActionType) string {
	switch t {
	case gitlabbackend.ActionNone:
		return " "
	case gitlabbackend.ActionCreateMR:
		return "+"
	case gitlabbackend.ActionUpdateRemote:
		return "↑"
	case gitlabbackend.ActionUpdateLocal:
		return "↓"
	case gitlabbackend.ActionMRMerged:
		return "✓"
	case gitlabbackend.ActionPendingMR:
		return "⏳"
	case gitlabbackend.ActionConflict:
		return "⚠"
	case gitlabbackend.ActionWarnNew:
		return "?"
	default:
		return " "
	}
}

// syncGitLabForceRemote overwrites local manifest with remote data.
func syncGitLabForceRemote(ctx context.Context, glBackend *gitlabbackend.Backend, m *manifest.Manifest, path string) error {
	fmt.Println("Forcing remote → local (discarding local changes)...")

	// Get all remote features
	remoteFeatures, err := glBackend.ListAll(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to list remote features: %v\n", err)
		return exitErr(exitConflict, "failed to list remote features")
	}

	// Replace manifest features with remote
	m.Features = make(map[string]manifest.Entry)
	for _, f := range remoteFeatures {
		m.Features[f.ID] = manifest.Entry{
			Name:     f.Name,
			Summary:  f.Summary,
			Owner:    f.Owner,
			Tags:     f.Tags,
			Synced:   true,
			SyncedAt: time.Now().Format(time.RFC3339),
		}
	}

	// Save manifest
	if saveErr := m.SaveWithLock(path); saveErr != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to save manifest: %v\n", saveErr)
		return exitErr(exitWrite, "failed to save manifest")
	}

	fmt.Printf("✓ Synced %d feature(s) from remote\n", len(remoteFeatures))
	return nil
}

// executeSyncActions executes the planned sync actions.
func executeSyncActions(ctx context.Context, glBackend *gitlabbackend.Backend, m *manifest.Manifest, path string, result *gitlabbackend.SyncResult) error {
	var created, updated, pending, skipped int

	for _, action := range result.Actions {
		switch action.Type {
		case gitlabbackend.ActionCreateMR:
			fmt.Printf("Creating MR for %s...\n", action.LocalID)
			if execErr := glBackend.ExecuteAction(ctx, action); execErr != nil {
				fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", action.LocalID, execErr)
				skipped++
				continue
			}
			fmt.Printf("  ✓ MR created for %s\n", action.LocalID)
			created++

		case gitlabbackend.ActionMRMerged:
			// Update local manifest with server ID
			if action.ServerID != "" && action.ServerID != "unknown" {
				entry := m.Features[action.LocalID]
				delete(m.Features, action.LocalID)
				m.Features[action.ServerID] = manifest.Entry{
					Name:     entry.Name,
					Summary:  entry.Summary,
					Owner:    entry.Owner,
					Tags:     entry.Tags,
					Synced:   true,
					SyncedAt: time.Now().Format(time.RFC3339),
					Alias:    action.LocalID,
				}
				fmt.Printf("  ✓ %s → %s (merged)\n", action.LocalID, action.ServerID)
			}
			// Clean up pending MR
			if execErr := glBackend.ExecuteAction(ctx, action); execErr != nil {
				fmt.Fprintf(os.Stderr, "  ⚠ Failed to clean up pending MR: %v\n", execErr)
			}
			updated++

		case gitlabbackend.ActionUpdateLocal:
			// Pull remote changes
			if action.Feature != nil {
				m.Features[action.ServerID] = manifest.Entry{
					Name:     action.Feature.Name,
					Summary:  action.Feature.Summary,
					Owner:    action.Feature.Owner,
					Tags:     action.Feature.Tags,
					Synced:   true,
					SyncedAt: time.Now().Format(time.RFC3339),
				}
				fmt.Printf("  ↓ Updated %s from remote\n", action.ServerID)
			}
			updated++

		case gitlabbackend.ActionUpdateRemote:
			fmt.Printf("Creating update MR for %s...\n", action.ServerID)
			if execErr := glBackend.ExecuteAction(ctx, action); execErr != nil {
				fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", action.ServerID, execErr)
				skipped++
				continue
			}
			fmt.Printf("  ✓ Update MR created for %s\n", action.ServerID)
			created++

		case gitlabbackend.ActionPendingMR:
			fmt.Printf("  ⏳ %s\n", action.Description)
			pending++

		case gitlabbackend.ActionConflict:
			fmt.Fprintf(os.Stderr, "  ⚠ %s\n", action.Description)
			skipped++

		case gitlabbackend.ActionWarnNew:
			// Already shown in warnings
			continue

		case gitlabbackend.ActionNone:
			continue
		}
	}

	// Save manifest
	if saveErr := m.SaveWithLock(path); saveErr != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to save manifest: %v\n", saveErr)
		return exitErr(exitWrite, "failed to save manifest")
	}

	// Print summary
	fmt.Println()
	fmt.Printf("Summary: MRs created: %d, Updated: %d, Pending: %d, Skipped: %d\n",
		created, updated, pending, skipped)

	if len(result.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range result.Warnings {
			fmt.Printf("  ⚠ %s\n", w)
		}
	}

	if skipped > 0 {
		return exitErr(exitValidation, "some actions were skipped")
	}
	return nil
}

var manifestPendingCmd = &cobra.Command{
	Use:   "pending",
	Short: "Show pending merge requests (GitLab mode)",
	Long: `Display the status of pending merge requests created for feature sync.

Only applicable in GitLab mode. Shows MRs that have been created but not yet merged.`,
	PreRunE: func(_ *cobra.Command, _ []string) error {
		return initBackend()
	},
	RunE: func(_ *cobra.Command, _ []string) error {
		if cfg.Mode != config.ModeGitLab {
			fmt.Fprintln(os.Stderr, "Error: pending command only available in GitLab mode")
			return exitErr(exitValidation, "not in GitLab mode")
		}

		pending, err := gitlabbackend.LoadPendingMRs()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return exitErr(exitValidation, "failed to load pending MRs")
		}

		if pending.IsEmpty() {
			fmt.Println("No pending merge requests")
			return nil
		}

		fmt.Printf("Pending merge requests (%d):\n\n", pending.Count())
		for _, mr := range pending.List() {
			fmt.Printf("  Local ID:   %s\n", mr.LocalID)
			if mr.ServerID != "" {
				fmt.Printf("  Server ID:  %s\n", mr.ServerID)
			}
			fmt.Printf("  Operation:  %s\n", mr.Operation)
			if mr.MRIID > 0 {
				fmt.Printf("  MR #%d:     %s\n", mr.MRIID, mr.MRURL)
			}
			fmt.Printf("  Branch:     %s\n", mr.Branch)
			fmt.Printf("  Created:    %s\n", mr.CreatedAt.Format(time.RFC3339))
			fmt.Println()
		}

		return nil
	},
}

// featureCmd is the parent command for feature operations.
var featureCmd = &cobra.Command{
	Use:   "feature",
	Short: "Manage features",
	Long:  `The feature command group manages individual features in the local manifest.`,
}

var featureCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new local feature in the manifest",
	Long: `Create a new feature entry in the local manifest. The feature ID must
start with FT-LOCAL- and contain only lowercase letters, numbers, and hyphens.

Examples:
  featctl feature create --id FT-LOCAL-auth --name "Authentication" --summary "User login flow"
  featctl feature create --id FT-LOCAL-billing-v2 --name "Billing V2" --summary "New billing" --owner "Payments" --tags "billing,payments"`,
	RunE: func(_ *cobra.Command, _ []string) error {
		// Validate required flags
		if featureID == "" {
			fmt.Fprintln(os.Stderr, "Error: --id is required")
			return exitErr(exitValidation, "--id is required")
		}
		if featureName == "" {
			fmt.Fprintln(os.Stderr, "Error: --name is required")
			return exitErr(exitValidation, "--name is required")
		}
		if featureSummary == "" {
			fmt.Fprintln(os.Stderr, "Error: --summary is required")
			return exitErr(exitValidation, "--summary is required")
		}

		// Validate ID format
		if err := manifest.ValidateLocalID(featureID); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintln(os.Stderr, "ID must match: FT-LOCAL-[a-z0-9-]{1,64}")
			fmt.Fprintln(os.Stderr, "Example: FT-LOCAL-auth-flow")
			return exitErr(exitValidation, "invalid ID format")
		}

		// Find or create manifest
		path, err := manifest.Discover(manifestPath)
		if err != nil {
			if errors.Is(err, manifest.ErrManifestNotFound) {
				fmt.Fprintln(os.Stderr, "Error: manifest not found")
				fmt.Fprintln(os.Stderr, "Run 'featctl manifest init' first")
				return exitErr(exitValidation, "manifest not found")
			}
			return err
		}

		// Load manifest
		m, err := manifest.Load(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			if errors.Is(err, manifest.ErrInvalidYAML) {
				return exitErr(exitValidation, "invalid manifest YAML")
			}
			return exitErr(exitValidation, "failed to load manifest")
		}

		// Parse tags
		var tags []string
		if featureTags != "" {
			for _, tag := range strings.Split(featureTags, ",") {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					tags = append(tags, tag)
				}
			}
		}

		// Add feature
		if err := m.AddFeature(featureID, featureName, featureSummary, featureOwner, tags); err != nil {
			if errors.Is(err, manifest.ErrIDExists) {
				fmt.Fprintf(os.Stderr, "Error: feature ID already exists: %s\n", featureID)
				return exitErr(exitConflict, "feature ID already exists")
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return exitErr(exitValidation, "failed to add feature")
		}

		// Save with lock
		if err := m.SaveWithLock(path); err != nil {
			if errors.Is(err, manifest.ErrLockTimeout) {
				fmt.Fprintln(os.Stderr, "Error: manifest locked by another process")
				return exitErr(exitWrite, "manifest locked")
			}
			fmt.Fprintf(os.Stderr, "Error: failed to save manifest: %v\n", err)
			return exitErr(exitWrite, "failed to save manifest")
		}

		fmt.Printf("✓ Created feature %s in %s\n", featureID, path)
		return nil
	},
}

// configCmd is the parent command for configuration operations.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage featctl configuration",
	Long: `The config command group manages featctl configuration.

Configuration is loaded from multiple sources with the following precedence (highest to lowest):
1. CLI flags (--mode, --server, --gitlab-project, etc.)
2. Environment variables (FEATCTL_MODE, FEATCTL_GITLAB_PROJECT, etc.)
3. Project config (.featctl.yaml in repo root)
4. Global config (~/.config/featctl/config.yaml)
5. Built-in defaults`,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show resolved configuration",
	Long: `Display the fully resolved configuration after applying all sources.

This shows the effective configuration that would be used by featctl commands,
including defaults, config files, environment variables, and CLI flags.
Sensitive values (tokens) are redacted.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := initConfig(); err != nil {
			return err
		}

		switch configShowOutput {
		case outputJSON:
			// For JSON output, use the package-level types
			token := ""
			if cfg.GitLab.Token != "" {
				token = "[REDACTED]"
			}

			display := jsonConfigDisplay{
				Version: cfg.Version,
				Mode:    cfg.Mode,
				Atlas:   cfg.Atlas,
				GitLab: jsonGitLabConfig{
					Instance:             cfg.GitLab.Instance,
					Project:              cfg.GitLab.Project,
					MainBranch:           cfg.GitLab.MainBranch,
					OAuthClientID:        cfg.GitLab.OAuthClientID,
					MRLabels:             cfg.GitLab.MRLabels,
					MRRemoveSourceBranch: cfg.GitLab.MRRemoveSourceBranch,
					DefaultAssignee:      cfg.GitLab.DefaultAssignee,
					Token:                token,
				},
				Cache: cfg.Cache,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(display)
		default:
			// YAML output (default) - use String() which handles redaction
			fmt.Print(cfg.String())

			// Show discovered config file paths
			global, project := config.DiscoveredPaths()
			fmt.Println("\n# Configuration sources:")
			if global != "" {
				fmt.Printf("# - Global: %s\n", global)
			} else {
				fmt.Println("# - Global: (not found)")
			}
			if project != "" {
				fmt.Printf("# - Project: %s\n", project)
			} else {
				fmt.Println("# - Project: (not found)")
			}
			if flagConfigPath != "" {
				fmt.Printf("# - Explicit: %s\n", flagConfigPath)
			}
		}
		return nil
	},
}

// jsonConfigDisplay is used for JSON output of config show command.
type jsonConfigDisplay struct {
	Version string             `json:"version"`
	Mode    string             `json:"mode"`
	Atlas   config.AtlasConfig `json:"atlas"`
	GitLab  jsonGitLabConfig   `json:"gitlab"`
	Cache   config.CacheConfig `json:"cache"`
}

// jsonGitLabConfig is used for JSON output of config show command.
type jsonGitLabConfig struct {
	Instance             string   `json:"instance"`
	Project              string   `json:"project"`
	MainBranch           string   `json:"main_branch"`
	OAuthClientID        string   `json:"oauth_client_id"`
	MRLabels             []string `json:"mr_labels"`
	MRRemoveSourceBranch bool     `json:"mr_remove_source_branch"`
	DefaultAssignee      string   `json:"default_assignee"`
	Token                string   `json:"token,omitempty"`
}

// loginCmd handles GitLab authentication.
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with GitLab",
	Long: `Authenticate with GitLab using OAuth2 Device Authorization Grant flow.

This command initiates an interactive login flow:
1. You'll receive a code and URL to visit
2. Go to the URL and enter the code
3. Approve the authorization in your browser
4. The CLI will automatically receive the access token

The token is stored securely in your OS keyring.

For CI/CD environments, use --token to provide a Personal Access Token or
set the FEATCTL_GITLAB_TOKEN environment variable instead.

Prerequisites:
  Register an OAuth application in GitLab (Settings → Applications):
  - Name: featctl
  - Confidential: No
  - Scopes: api (or read_api for read-only)
  Copy the Application ID to gitlab.oauth_client_id in your config.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := initConfig(); err != nil {
			return err
		}

		// Check if using PAT directly
		if loginToken != "" {
			return loginWithPAT()
		}

		// Check for CI environment
		if auth.IsHeadless() {
			fmt.Fprintln(os.Stderr, "Error: interactive login not available in headless environment")
			fmt.Fprintln(os.Stderr, "Use --token flag or set FEATCTL_GITLAB_TOKEN environment variable")
			return exitErr(exitValidation, "headless environment")
		}

		// Check for OAuth client ID
		if cfg.GitLab.OAuthClientID == "" {
			fmt.Fprintln(os.Stderr, "Error: gitlab.oauth_client_id not configured")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "To use interactive login, register an OAuth application in GitLab:")
			fmt.Fprintln(os.Stderr, "  1. Go to GitLab → Settings → Applications")
			fmt.Fprintln(os.Stderr, "  2. Create application with name 'featctl', Confidential: No, Scopes: api")
			fmt.Fprintln(os.Stderr, "  3. Copy the Application ID to your config:")
			fmt.Fprintln(os.Stderr, "     gitlab:")
			fmt.Fprintln(os.Stderr, "       oauth_client_id: <your-app-id>")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Or use --token to provide a Personal Access Token directly.")
			return exitErr(exitValidation, "oauth_client_id not configured")
		}

		return loginWithDeviceFlow()
	},
}

// loginWithPAT stores a Personal Access Token.
func loginWithPAT() error {
	token := auth.CreateTokenFromPAT(loginToken)
	if err := auth.StoreToken(cfg.GitLab.Instance, token); err != nil {
		if errors.Is(err, auth.ErrKeyringNotAvail) {
			fmt.Fprintln(os.Stderr, "Warning: keyring not available, token not stored")
			fmt.Fprintln(os.Stderr, "Set FEATCTL_GITLAB_TOKEN environment variable for persistent auth")
			return nil
		}
		return fmt.Errorf("store token: %w", err)
	}
	fmt.Printf("✓ Token stored for %s\n", cfg.GitLab.Instance)
	return nil
}

// loginWithDeviceFlow performs OAuth2 Device Authorization Grant flow.
func loginWithDeviceFlow() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	gitlabAuth := auth.NewGitLabAuth(cfg.GitLab.Instance, cfg.GitLab.OAuthClientID)

	// Start device flow
	fmt.Printf("Initiating login to %s...\n", cfg.GitLab.Instance)
	deviceResp, err := gitlabAuth.StartDeviceFlow(ctx)
	if err != nil {
		if errors.Is(err, auth.ErrDeviceFlowNotSupported) {
			fmt.Fprintln(os.Stderr, "Error: Device Authorization Grant not supported")
			fmt.Fprintln(os.Stderr, "This feature requires GitLab 17.2 or later.")
			fmt.Fprintln(os.Stderr, "Use --token to provide a Personal Access Token instead.")
			return exitErr(exitValidation, "device flow not supported")
		}
		return fmt.Errorf("start device flow: %w", err)
	}

	// Display instructions
	fmt.Println()
	fmt.Printf("To complete authentication, visit:\n")
	fmt.Printf("  %s\n\n", deviceResp.VerificationURI)
	fmt.Printf("And enter this code: %s\n\n", deviceResp.UserCode)
	fmt.Println("Waiting for authorization...")

	// Poll for token
	pollInterval := time.Duration(deviceResp.Interval) * time.Second
	if pollInterval < time.Second {
		pollInterval = auth.DefaultPollInterval
	}

	token, err := gitlabAuth.PollForToken(ctx, deviceResp.DeviceCode, pollInterval)
	if err != nil {
		if errors.Is(err, auth.ErrAuthorizationDenied) {
			fmt.Fprintln(os.Stderr, "\n✗ Authorization denied")
			return exitErr(exitValidation, "authorization denied")
		}
		if errors.Is(err, auth.ErrExpiredToken) {
			fmt.Fprintln(os.Stderr, "\n✗ Authorization timed out")
			fmt.Fprintln(os.Stderr, "Run 'featctl login' again to retry")
			return exitErr(exitValidation, "authorization expired")
		}
		return fmt.Errorf("poll for token: %w", err)
	}

	// Store token
	if err := auth.StoreToken(cfg.GitLab.Instance, token); err != nil {
		if !errors.Is(err, auth.ErrKeyringNotAvail) {
			return fmt.Errorf("store token: %w", err)
		}
		// Keyring not available - warn but continue (token valid for this session)
		fmt.Fprintln(os.Stderr, "\nWarning: keyring not available, token not stored persistently")
		fmt.Fprintln(os.Stderr, "You'll need to login again in a new session")
	}

	fmt.Println()
	fmt.Printf("✓ Successfully authenticated with %s\n", cfg.GitLab.Instance)
	return nil
}

// logoutCmd removes stored GitLab credentials.
var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored GitLab credentials",
	Long: `Remove the stored GitLab access token from your OS keyring.

This does not revoke the token on GitLab's side. To fully revoke access,
visit your GitLab Settings → Access Tokens page.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := initConfig(); err != nil {
			return err
		}

		if err := auth.DeleteToken(cfg.GitLab.Instance); err != nil {
			if errors.Is(err, auth.ErrKeyringNotAvail) {
				fmt.Println("No stored credentials (keyring not available)")
				return nil
			}
			return fmt.Errorf("delete token: %w", err)
		}

		fmt.Printf("✓ Credentials removed for %s\n", cfg.GitLab.Instance)
		return nil
	},
}

// authStatusCmd shows authentication status.
var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show authentication status",
	Long:  `Display the current authentication status for all configured backends.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := initConfig(); err != nil {
			return err
		}

		fmt.Println("Authentication Status")
		fmt.Println("=====================")
		fmt.Println()

		// Atlas status
		fmt.Printf("Atlas (%s)\n", cfg.Atlas.ServerURL)
		if cfg.Atlas.Cert != "" {
			fmt.Printf("  Certificate: %s\n", cfg.Atlas.Cert)
			if _, err := os.Stat(cfg.Atlas.Cert); err == nil {
				fmt.Println("  Status: Configured ✓")
			} else {
				fmt.Println("  Status: Certificate file not found ✗")
			}
		} else {
			fmt.Println("  Status: Not configured")
		}
		fmt.Println()

		// GitLab status
		fmt.Printf("GitLab (%s)\n", cfg.GitLab.Instance)

		// Check env token
		if os.Getenv(config.EnvGitLabToken) != "" {
			fmt.Println("  Token: From environment variable ✓")
			fmt.Println("  Status: Ready")
		} else if os.Getenv(config.EnvCIJobToken) != "" {
			fmt.Println("  Token: CI Job Token ✓")
			fmt.Println("  Status: Ready (CI environment)")
		} else {
			// Check keyring
			token, err := auth.LoadToken(cfg.GitLab.Instance)
			if err != nil {
				if errors.Is(err, auth.ErrNoCredential) {
					fmt.Println("  Token: Not stored")
					fmt.Println("  Status: Not authenticated")
					fmt.Println("  Run 'featctl login' to authenticate")
				} else if errors.Is(err, auth.ErrKeyringNotAvail) {
					fmt.Println("  Token: Keyring not available")
					fmt.Println("  Status: Set FEATCTL_GITLAB_TOKEN environment variable")
				} else {
					fmt.Printf("  Status: Error loading token: %v\n", err)
				}
			} else {
				if token.IsExpired() {
					fmt.Println("  Token: Expired ✗")
					fmt.Println("  Status: Re-authentication required")
					fmt.Println("  Run 'featctl login' to refresh")
				} else if token.IsExpiringSoon(auth.TokenRefreshBuffer) {
					fmt.Println("  Token: Expiring soon")
					fmt.Printf("  Expires: %s\n", token.ExpiresAt.Format(time.RFC3339))
					fmt.Println("  Status: Will auto-refresh on next use")
				} else {
					fmt.Println("  Token: Valid ✓")
					if !token.ExpiresAt.IsZero() {
						fmt.Printf("  Expires: %s\n", token.ExpiresAt.Format(time.RFC3339))
					}
					fmt.Println("  Status: Ready")
				}
			}
		}

		return nil
	},
}

// authCmd is the parent command for authentication operations.
var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication",
	Long:  `The auth command group manages authentication for featctl backends.`,
}

func init() {
	// Global flags for dual-mode support
	rootCmd.PersistentFlags().StringVar(&flagMode, "mode", "", "Backend mode: atlas or gitlab")
	rootCmd.PersistentFlags().StringVar(&flagConfigPath, "config", "", "Custom config file path")
	rootCmd.PersistentFlags().StringVar(&flagGitLabInstance, "gitlab-instance", "", "GitLab instance URL")
	rootCmd.PersistentFlags().StringVar(&flagGitLabProject, "gitlab-project", "", "GitLab project path or ID")

	// Global flags for server commands (backward compatible with Atlas mode)
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "", "Feature Atlas server URL")
	rootCmd.PersistentFlags().StringVar(&caFile, "ca", "", "CA certificate file")
	rootCmd.PersistentFlags().StringVar(&certFile, "cert", "", "Client certificate file")
	rootCmd.PersistentFlags().StringVar(&keyFile, "key", "", "Client private key file")

	// Search flags
	searchCmd.Flags().IntVarP(&searchLimit, "limit", "l", 20, "Maximum number of results")
	searchCmd.Flags().StringVarP(&searchOutput, "output", "o", "text", "Output format (text, json, yaml)")

	// Get flags
	getCmd.Flags().StringVarP(&getOutput, "output", "o", "text", "Output format (text, json, yaml)")

	// TUI flags
	tuiCmd.Flags().BoolVar(&tuiSync, "sync", false, "Sync added features to server immediately")
	tuiCmd.Flags().StringVar(&tuiManifest, "manifest", "", "Custom manifest path")

	// Lint flags
	lintCmd.Flags().IntVar(&minDescLength, "min-desc-length", 10, "Minimum description length")
	lintCmd.Flags().BoolVar(&lintOffline, "offline", false, "Only check local manifest (no server connection)")
	lintCmd.Flags().StringVar(&lintManifest, "manifest", "", "Custom manifest path")

	// Manifest init flags
	manifestInitCmd.Flags().StringVar(&manifestPath, "manifest", "", "Custom manifest path")
	manifestInitCmd.Flags().BoolVar(&manifestForce, "force", false, "Overwrite existing manifest")

	// Manifest list flags
	manifestListCmd.Flags().StringVar(&manifestPath, "manifest", "", "Custom manifest path")
	manifestListCmd.Flags().StringVarP(&manifestOutput, "output", "o", "text", "Output format (text, json, yaml)")
	manifestListCmd.Flags().BoolVar(&manifestUnsynced, "unsynced", false, "Show only unsynced features")

	// Manifest add flags
	manifestAddCmd.Flags().StringVar(&manifestPath, "manifest", "", "Custom manifest path")

	// Manifest sync flags
	manifestSyncCmd.Flags().StringVar(&manifestPath, "manifest", "", "Custom manifest path")
	manifestSyncCmd.Flags().BoolVar(&manifestDryRun, "dry-run", false, "Show what would be synced without changes")
	manifestSyncCmd.Flags().BoolVar(&manifestForceLocal, "force-local", false, "Push local changes via MR (GitLab mode)")
	manifestSyncCmd.Flags().BoolVar(&manifestForceRemote, "force-remote", false, "Overwrite local changes with remote (GitLab mode)")

	// Feature create flags
	featureCreateCmd.Flags().StringVar(&manifestPath, "manifest", "", "Custom manifest path")
	featureCreateCmd.Flags().StringVar(&featureID, "id", "", "Feature ID (required, must start with FT-LOCAL-)")
	featureCreateCmd.Flags().StringVar(&featureName, "name", "", "Feature name (required)")
	featureCreateCmd.Flags().StringVar(&featureSummary, "summary", "", "Feature summary (required)")
	featureCreateCmd.Flags().StringVar(&featureOwner, "owner", "", "Feature owner")
	featureCreateCmd.Flags().StringVar(&featureTags, "tags", "", "Comma-separated tags")

	// Config show flags
	configShowCmd.Flags().StringVarP(&configShowOutput, "output", "o", "yaml", "Output format (yaml, json)")

	// Login flags
	loginCmd.Flags().StringVar(&loginToken, "token", "", "Personal Access Token (for non-interactive login)")

	// Build command tree
	manifestCmd.AddCommand(manifestInitCmd)
	manifestCmd.AddCommand(manifestListCmd)
	manifestCmd.AddCommand(manifestAddCmd)
	manifestCmd.AddCommand(manifestSyncCmd)
	manifestCmd.AddCommand(manifestPendingCmd)
	featureCmd.AddCommand(featureCreateCmd)
	configCmd.AddCommand(configShowCmd)
	authCmd.AddCommand(authStatusCmd)

	// Add commands to root
	rootCmd.AddCommand(meCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(tuiCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(lintCmd)
	rootCmd.AddCommand(manifestCmd)
	rootCmd.AddCommand(featureCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(logoutCmd)
	rootCmd.AddCommand(authCmd)
}
