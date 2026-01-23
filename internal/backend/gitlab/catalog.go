// Package gitlab provides the GitLab backend implementation for featctl.
// It implements the FeatureBackend interface using the GitLab API
// to read/write features from a Git repository catalog.
package gitlab

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/JoobyPM/feature-atlas-service/internal/backend"
)

// Catalog directory and file constants.
const (
	// FeaturesDir is the directory containing feature YAML files.
	FeaturesDir = "features"
	// FeatureFileExt is the file extension for feature files.
	FeatureFileExt = ".yaml"
)

// FeatureFile represents a feature as stored in the Git catalog.
// This is the YAML format used in feature files.
type FeatureFile struct {
	ID        string   `yaml:"id"`
	Name      string   `yaml:"name"`
	Summary   string   `yaml:"summary"`
	Owner     string   `yaml:"owner,omitempty"`
	Tags      []string `yaml:"tags,omitempty"`
	CreatedAt string   `yaml:"created_at"` // RFC3339 format
	UpdatedAt string   `yaml:"updated_at"` // RFC3339 format
}

// featureIDPattern matches valid feature IDs: FT-NNNNNN or FT-LOCAL-*
var featureIDPattern = regexp.MustCompile(`^FT-(\d{6}|LOCAL-.+)$`)

// ParseFeatureFile parses a YAML feature file content into a Feature.
func ParseFeatureFile(content []byte) (*backend.Feature, error) {
	var ff FeatureFile
	if err := yaml.Unmarshal(content, &ff); err != nil {
		return nil, fmt.Errorf("parse feature yaml: %w", err)
	}

	if ff.ID == "" {
		return nil, errors.New("feature file missing id")
	}
	if ff.Name == "" {
		return nil, errors.New("feature file missing name")
	}

	// Parse timestamps
	var createdAt, updatedAt time.Time
	var err error

	if ff.CreatedAt != "" {
		createdAt, err = time.Parse(time.RFC3339, ff.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
	}

	if ff.UpdatedAt != "" {
		updatedAt, err = time.Parse(time.RFC3339, ff.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse updated_at: %w", err)
		}
	}

	return &backend.Feature{
		ID:        ff.ID,
		Name:      ff.Name,
		Summary:   ff.Summary,
		Owner:     ff.Owner,
		Tags:      ff.Tags,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

// FormatFeatureFile converts a Feature to YAML content for storage.
func FormatFeatureFile(f *backend.Feature) ([]byte, error) {
	ff := FeatureFile{
		ID:      f.ID,
		Name:    f.Name,
		Summary: f.Summary,
		Owner:   f.Owner,
		Tags:    f.Tags,
	}

	// Format timestamps
	if !f.CreatedAt.IsZero() {
		ff.CreatedAt = f.CreatedAt.Format(time.RFC3339)
	} else {
		ff.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	if !f.UpdatedAt.IsZero() {
		ff.UpdatedAt = f.UpdatedAt.Format(time.RFC3339)
	} else {
		ff.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	content, err := yaml.Marshal(&ff)
	if err != nil {
		return nil, fmt.Errorf("marshal feature yaml: %w", err)
	}
	return content, nil
}

// FeatureFilePath returns the path for a feature file in the catalog.
func FeatureFilePath(id string) string {
	return path.Join(FeaturesDir, id+FeatureFileExt)
}

// FeatureIDFromPath extracts the feature ID from a file path.
// Returns empty string if the path is not a valid feature file.
func FeatureIDFromPath(filePath string) string {
	// Get base name without extension
	base := path.Base(filePath)
	if !strings.HasSuffix(base, FeatureFileExt) {
		return ""
	}

	id := strings.TrimSuffix(base, FeatureFileExt)
	if !featureIDPattern.MatchString(id) {
		return ""
	}
	return id
}

// IsValidFeatureID checks if a string is a valid feature ID format.
func IsValidFeatureID(id string) bool {
	return featureIDPattern.MatchString(id)
}

// ParseFeatureIDNumber extracts the numeric part from a feature ID.
// Returns -1 if the ID is not a numeric FT-NNNNNN format.
func ParseFeatureIDNumber(id string) int {
	if !strings.HasPrefix(id, "FT-") {
		return -1
	}
	numStr := strings.TrimPrefix(id, "FT-")
	if strings.HasPrefix(numStr, "LOCAL-") {
		return -1
	}
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return -1
	}
	return num
}

// NextFeatureID returns the next available feature ID given a list of existing IDs.
// It finds the maximum numeric ID and increments by 1.
func NextFeatureID(existingIDs []string) string {
	maxNum := 0
	for _, id := range existingIDs {
		num := ParseFeatureIDNumber(id)
		if num > maxNum {
			maxNum = num
		}
	}
	return fmt.Sprintf("FT-%06d", maxNum+1)
}

// FilterFeatures filters features by a query string.
// Matches against ID, Name, and Summary (case-insensitive).
func FilterFeatures(features []backend.Feature, query string, limit int) []backend.Feature {
	if query == "" {
		if limit > 0 && limit < len(features) {
			return features[:limit]
		}
		return features
	}

	query = strings.ToLower(query)
	var result []backend.Feature

	for _, f := range features {
		if matchesQuery(f, query) {
			result = append(result, f)
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result
}

// matchesQuery checks if a feature matches the query string.
func matchesQuery(f backend.Feature, query string) bool {
	return strings.Contains(strings.ToLower(f.ID), query) ||
		strings.Contains(strings.ToLower(f.Name), query) ||
		strings.Contains(strings.ToLower(f.Summary), query)
}

// SuggestFromFeatures creates suggest items from features filtered by prefix.
// For autocomplete, we prioritize ID prefix matching, then name prefix matching.
func SuggestFromFeatures(features []backend.Feature, query string, limit int) []backend.SuggestItem {
	query = strings.ToLower(query)

	// Score features by match quality
	type scoredItem struct {
		item  backend.SuggestItem
		score int
	}
	var scoredItems []scoredItem

	for _, f := range features {
		item := backend.SuggestItem{
			ID:      f.ID,
			Name:    f.Name,
			Summary: f.Summary,
		}

		score := matchScore(f, query)
		if score > 0 {
			scoredItems = append(scoredItems, scoredItem{item: item, score: score})
		}
	}

	// Sort by score descending, then by ID ascending for stability
	sort.Slice(scoredItems, func(i, j int) bool {
		if scoredItems[i].score != scoredItems[j].score {
			return scoredItems[i].score > scoredItems[j].score
		}
		return scoredItems[i].item.ID < scoredItems[j].item.ID
	})

	// Extract items with limit
	result := make([]backend.SuggestItem, 0, minInt(limit, len(scoredItems)))
	for i := 0; i < len(scoredItems) && (limit <= 0 || i < limit); i++ {
		result = append(result, scoredItems[i].item)
	}
	return result
}

// matchScore returns a score for how well a feature matches the query.
// Higher score = better match.
// 0 = no match.
func matchScore(f backend.Feature, query string) int {
	if query == "" {
		return 1 // All features match empty query
	}

	idLower := strings.ToLower(f.ID)
	nameLower := strings.ToLower(f.Name)
	summaryLower := strings.ToLower(f.Summary)

	// Exact ID match - highest priority
	if idLower == query {
		return 100
	}
	// ID prefix match
	if strings.HasPrefix(idLower, query) {
		return 80
	}
	// Name prefix match
	if strings.HasPrefix(nameLower, query) {
		return 60
	}
	// ID contains
	if strings.Contains(idLower, query) {
		return 40
	}
	// Name contains
	if strings.Contains(nameLower, query) {
		return 30
	}
	// Summary contains
	if strings.Contains(summaryLower, query) {
		return 10
	}

	return 0
}

// minInt returns the smaller of two integers.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
