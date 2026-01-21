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
	// Global flags
	serverURL string
	caFile    string
	certFile  string
	keyFile   string

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
	manifestPath     string
	manifestForce    bool
	manifestOutput   string
	manifestUnsynced bool
	manifestDryRun   bool

	// TUI flags
	tuiSync     bool
	tuiManifest string

	// Feature create flags
	featureID      string
	featureName    string
	featureSummary string
	featureOwner   string
	featureTags    string

	// Client instance (lazy initialized)
	client *apiclient.Client
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

// initClient creates the API client. Called only for server commands.
func initClient() error {
	if client != nil {
		return nil
	}
	var err error
	client, err = apiclient.New(serverURL, caFile, certFile, keyFile)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
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
  Ctrl+A       Select all visible
  Ctrl+N       Deselect all
  Enter        Confirm selection
  Esc          Clear search or quit

Type to search - all keys go to search input. Selections persist across
search changes - select items from different searches and confirm all
at once. Use --sync to push them to the server immediately.`,
	PreRunE: func(_ *cobra.Command, _ []string) error {
		return initClient()
	},
	RunE: func(_ *cobra.Command, _ []string) error {
		// Build TUI options with manifest state
		opts, manifestLoaded, mPath, err := buildTUIOptions()
		if err != nil {
			return err
		}

		// Run TUI
		result, tuiErr := tui.Run(client, opts)
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

	// Try to load manifest
	mPath, discoverErr := manifest.Discover(tuiManifest)
	if discoverErr != nil {
		if errors.Is(discoverErr, manifest.ErrManifestNotFound) {
			// No manifest - that's fine, features will show as "on server"
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
func addSelectedToManifest(selected []apiclient.SuggestItem, manifestLoaded bool, mPath string) error {
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

	var synced, failed int
	for _, localID := range ids {
		entry := unsynced[localID]

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		feature, createErr := client.CreateFeature(ctx, apiclient.CreateFeatureRequest{
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
	Short: "Sync unsynced local features to the server",
	Long: `Push all unsynced local features (FT-LOCAL-*) to the server.
The server assigns canonical IDs (FT-NNNNNN) and the manifest is updated.

Requires admin mTLS certificate.`,
	PreRunE: func(_ *cobra.Command, _ []string) error {
		return initClient()
	},
	RunE: func(_ *cobra.Command, _ []string) error {
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
			feature, createErr := client.CreateFeature(ctx, apiclient.CreateFeatureRequest{
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

func init() {
	// Global flags for server commands
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "https://localhost:8443", "Feature Atlas server URL")
	rootCmd.PersistentFlags().StringVar(&caFile, "ca", "certs/ca.crt", "CA certificate file")
	rootCmd.PersistentFlags().StringVar(&certFile, "cert", "certs/alice.crt", "Client certificate file")
	rootCmd.PersistentFlags().StringVar(&keyFile, "key", "certs/alice.key", "Client private key file")

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

	// Feature create flags
	featureCreateCmd.Flags().StringVar(&manifestPath, "manifest", "", "Custom manifest path")
	featureCreateCmd.Flags().StringVar(&featureID, "id", "", "Feature ID (required, must start with FT-LOCAL-)")
	featureCreateCmd.Flags().StringVar(&featureName, "name", "", "Feature name (required)")
	featureCreateCmd.Flags().StringVar(&featureSummary, "summary", "", "Feature summary (required)")
	featureCreateCmd.Flags().StringVar(&featureOwner, "owner", "", "Feature owner")
	featureCreateCmd.Flags().StringVar(&featureTags, "tags", "", "Comma-separated tags")

	// Build command tree
	manifestCmd.AddCommand(manifestInitCmd)
	manifestCmd.AddCommand(manifestListCmd)
	manifestCmd.AddCommand(manifestAddCmd)
	manifestCmd.AddCommand(manifestSyncCmd)
	featureCmd.AddCommand(featureCreateCmd)

	// Add commands to root
	rootCmd.AddCommand(meCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(tuiCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(lintCmd)
	rootCmd.AddCommand(manifestCmd)
	rootCmd.AddCommand(featureCmd)
}
