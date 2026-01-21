// Package store provides an in-memory data store for clients and features.
package store

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brianvoe/gofakeit/v7"
)

// Role represents the authorization level of a client.
type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

// Client represents a registered mTLS client.
type Client struct {
	Fingerprint string    `json:"fingerprint"`
	Name        string    `json:"name"`
	Role        Role      `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
}

// Feature represents a feature catalog entry.
type Feature struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Summary   string    `json:"summary"`
	Owner     string    `json:"owner"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
}

// Store is a thread-safe in-memory data store.
type Store struct {
	mu         sync.RWMutex
	clients    map[string]Client
	features   map[string]Feature
	featureIDs []string // stable ordering for demo output
}

// New creates a new empty Store.
func New() *Store {
	return &Store{
		clients:  make(map[string]Client),
		features: make(map[string]Feature),
	}
}

// FingerprintSHA256 computes the SHA-256 fingerprint of an X.509 certificate.
func FingerprintSHA256(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// UpsertClient adds or updates a client in the store.
func (s *Store) UpsertClient(c Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c.Fingerprint] = c
}

// GetClient retrieves a client by fingerprint.
func (s *Store) GetClient(fp string) (Client, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.clients[fp]
	return c, ok
}

// ListClients returns all registered clients sorted by name.
func (s *Store) ListClients() []Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Client, 0, len(s.clients))
	for _, c := range s.clients {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SeedFeatures populates the store with fake feature data.
func (s *Store) SeedFeatures(count int) {
	gofakeit.Seed(time.Now().UnixNano())

	s.mu.Lock()
	defer s.mu.Unlock()

	s.features = make(map[string]Feature, count)
	s.featureIDs = make([]string, 0, count)

	now := time.Now()
	for i := 1; i <= count; i++ {
		id := "FT-" + leftPadInt(i, 6)
		f := Feature{
			ID:        id,
			Name:      toTitle(gofakeit.BS()),
			Summary:   gofakeit.Sentence(12),
			Owner:     toTitle(gofakeit.BuzzWord()),
			Tags:      []string{gofakeit.Noun(), gofakeit.Verb(), gofakeit.Adjective()},
			CreatedAt: now,
		}
		s.features[id] = f
		s.featureIDs = append(s.featureIDs, id)
	}
}

// GetFeature retrieves a feature by ID.
func (s *Store) GetFeature(id string) (Feature, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.features[id]
	return f, ok
}

// SearchFeatures performs a case-insensitive search across feature fields.
func (s *Store) SearchFeatures(query string, limit int) []Feature {
	if limit <= 0 {
		limit = 20
	}
	q := strings.ToLower(strings.TrimSpace(query))

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Feature, 0, limit)
	for _, id := range s.featureIDs {
		f := s.features[id]
		if q == "" || matchFeature(f, q) {
			out = append(out, f)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// Suggest returns features matching the query for autocomplete purposes.
func (s *Store) Suggest(query string, limit int) []Feature {
	return s.SearchFeatures(query, limit)
}

// matchFeature checks if a feature matches the query string.
func matchFeature(f Feature, q string) bool {
	id := strings.ToLower(f.ID)
	name := strings.ToLower(f.Name)
	sum := strings.ToLower(f.Summary)
	return strings.Contains(id, q) || strings.Contains(name, q) || strings.Contains(sum, q)
}

// leftPadInt pads an integer with leading zeros.
func leftPadInt(n, width int) string {
	s := strconv.Itoa(n)
	if len(s) >= width {
		return s
	}
	return strings.Repeat("0", width-len(s)) + s
}

// toTitle converts a string to title case.
func toTitle(s string) string {
	if len(s) == 0 {
		return s
	}
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(string(w[0])) + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
}

// ErrNotFound is returned when a requested item does not exist.
var ErrNotFound = errors.New("not found")
