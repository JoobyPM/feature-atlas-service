// Package main provides the CLI entry point for featctl.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/JoobyPM/feature-atlas-service/internal/apiclient"
	"github.com/JoobyPM/feature-atlas-service/internal/tui"
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

	// Client instance
	client *apiclient.Client
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
browsing, and validating features.`,
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		var err error
		client, err = apiclient.New(serverURL, caFile, certFile, keyFile)
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		return nil
	},
}

var meCmd = &cobra.Command{
	Use:   "me",
	Short: "Show authenticated client information",
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
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(features)
		case "yaml":
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
	RunE: func(_ *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		feature, err := client.GetFeature(ctx, args[0])
		if err != nil {
			return err
		}

		if feature == nil {
			return fmt.Errorf("feature not found: %s", args[0])
		}

		switch getOutput {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(feature)
		case "yaml":
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

		var errors []string

		// Validate feature_id
		if doc.FeatureID == "" {
			errors = append(errors, "missing required field: feature_id")
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			exists, err := client.FeatureExists(ctx, doc.FeatureID)
			if err != nil {
				return fmt.Errorf("check feature: %w", err)
			}
			if !exists {
				errors = append(errors, fmt.Sprintf("feature_id '%s' not found in catalog", doc.FeatureID))
			}
		}

		// Validate description
		if len(doc.Description) < minDescLength {
			errors = append(errors, fmt.Sprintf("description must be at least %d characters (got %d)", minDescLength, len(doc.Description)))
		}

		if len(errors) > 0 {
			fmt.Fprintf(os.Stderr, "Validation failed for %s:\n", args[0])
			for _, e := range errors {
				fmt.Fprintf(os.Stderr, "  ✗ %s\n", e)
			}
			os.Exit(1)
		}

		fmt.Printf("✓ %s is valid\n", args[0])
		return nil
	},
}

func init() {
	// Global flags
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

	// Add commands
	rootCmd.AddCommand(meCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(tuiCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(lintCmd)
}

// truncate shortens a string to maxLen with ellipsis.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
