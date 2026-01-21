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
	"unicode/utf8"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/JoobyPM/feature-atlas-service/internal/apiclient"
	"github.com/JoobyPM/feature-atlas-service/internal/manifest"
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

	// Manifest flags
	manifestPath     string
	manifestForce    bool
	manifestOutput   string
	manifestUnsynced bool

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
const (
	exitSuccess         = 0
	exitValidationError = 1
	exitIDExists        = 2
	exitWriteError      = 3
)

func main() {
	if err := rootCmd.Execute(); err != nil {
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
				fmt.Printf("    %s\n", truncate(f.Summary, 70))
				fmt.Println()
			}
		}
		return nil
	},
}

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Interactive terminal UI for browsing features",
	Long: `Launch an interactive terminal interface for searching and
selecting features. Use arrow keys to navigate and Enter to select.`,
	PreRunE: func(_ *cobra.Command, _ []string) error {
		return initClient()
	},
	RunE: func(_ *cobra.Command, _ []string) error {
		selected, err := tui.Run(client)
		if err != nil {
			return err
		}

		if selected != nil {
			fmt.Printf("\nSelected: %s - %s\n", selected.ID, selected.Name)
		}
		return nil
	},
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
the catalog. The YAML file should have a 'feature_id' field.`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(_ *cobra.Command, _ []string) error {
		return initClient()
	},
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
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			exists, err := client.FeatureExists(ctx, doc.FeatureID)
			if err != nil {
				return fmt.Errorf("check feature: %w", err)
			}
			if !exists {
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
			os.Exit(1)
		}

		fmt.Printf("✓ %s is valid\n", args[0])
		return nil
	},
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
				os.Exit(exitValidationError)
			}
		}

		// Create new manifest
		m := manifest.New()
		if err := m.Save(path); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to write manifest: %v\n", err)
			os.Exit(exitWriteError)
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
				os.Exit(exitValidationError)
			}
			return err
		}

		m, err := manifest.Load(path)
		if err != nil {
			if errors.Is(err, manifest.ErrInvalidYAML) {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(exitIDExists) // Exit 2 for parse error per PRD
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
					fmt.Printf("      %s\n", truncate(entry.Summary, 60))
				}
			}
			fmt.Printf("\nTotal: %d feature(s)\n", len(ids))
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
			os.Exit(exitValidationError)
		}
		if featureName == "" {
			fmt.Fprintln(os.Stderr, "Error: --name is required")
			os.Exit(exitValidationError)
		}
		if featureSummary == "" {
			fmt.Fprintln(os.Stderr, "Error: --summary is required")
			os.Exit(exitValidationError)
		}

		// Validate ID format
		if err := manifest.ValidateLocalID(featureID); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintln(os.Stderr, "ID must match: FT-LOCAL-[a-z0-9-]{1,64}")
			fmt.Fprintln(os.Stderr, "Example: FT-LOCAL-auth-flow")
			os.Exit(exitValidationError)
		}

		// Find or create manifest
		path, err := manifest.Discover(manifestPath)
		if err != nil {
			if errors.Is(err, manifest.ErrManifestNotFound) {
				fmt.Fprintln(os.Stderr, "Error: manifest not found")
				fmt.Fprintln(os.Stderr, "Run 'featctl manifest init' first")
				os.Exit(exitValidationError)
			}
			return err
		}

		// Load manifest
		m, err := manifest.Load(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(exitWriteError)
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
				os.Exit(exitIDExists)
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(exitValidationError)
		}

		// Save with lock
		if err := m.SaveWithLock(path); err != nil {
			if errors.Is(err, manifest.ErrLockTimeout) {
				fmt.Fprintln(os.Stderr, "Error: manifest locked by another process")
				os.Exit(exitWriteError)
			}
			fmt.Fprintf(os.Stderr, "Error: failed to save manifest: %v\n", err)
			os.Exit(exitWriteError)
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

	// Lint flags
	lintCmd.Flags().IntVar(&minDescLength, "min-desc-length", 10, "Minimum description length")

	// Manifest init flags
	manifestInitCmd.Flags().StringVar(&manifestPath, "manifest", "", "Custom manifest path")
	manifestInitCmd.Flags().BoolVar(&manifestForce, "force", false, "Overwrite existing manifest")

	// Manifest list flags
	manifestListCmd.Flags().StringVar(&manifestPath, "manifest", "", "Custom manifest path")
	manifestListCmd.Flags().StringVarP(&manifestOutput, "output", "o", "text", "Output format (text, json, yaml)")
	manifestListCmd.Flags().BoolVar(&manifestUnsynced, "unsynced", false, "Show only unsynced features")

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

// truncate shortens a string to maxLen runes with ellipsis.
// Uses rune count for proper UTF-8 handling.
func truncate(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen-3]) + "..."
}
