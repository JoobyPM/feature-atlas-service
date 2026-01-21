package store

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	s := New()
	if s == nil {
		t.Fatal("New() returned nil")
	}
	if s.clients == nil {
		t.Error("clients map not initialized")
	}
	if s.features == nil {
		t.Error("features map not initialized")
	}
}

func TestFingerprintSHA256(t *testing.T) {
	// Create a minimal certificate for testing
	cert := &x509.Certificate{
		Raw: []byte("test certificate data"),
	}

	fp := FingerprintSHA256(cert)

	// Should be 64 hex chars (256 bits = 32 bytes = 64 hex chars)
	if len(fp) != 64 {
		t.Errorf("fingerprint length = %d, want 64", len(fp))
	}

	// Same cert should produce same fingerprint
	fp2 := FingerprintSHA256(cert)
	if fp != fp2 {
		t.Error("same cert produced different fingerprints")
	}
}

func TestClientOperations(t *testing.T) {
	s := New()

	// Test GetClient on empty store
	_, ok := s.GetClient("nonexistent")
	if ok {
		t.Error("GetClient should return false for nonexistent client")
	}

	// Test UpsertClient
	client := Client{
		Fingerprint: "abc123",
		Name:        "test-client",
		Role:        RoleUser,
		CreatedAt:   time.Now(),
	}
	s.UpsertClient(client)

	// Test GetClient
	got, ok := s.GetClient("abc123")
	if !ok {
		t.Fatal("GetClient should return true for existing client")
	}
	if got.Name != "test-client" {
		t.Errorf("got name %q, want %q", got.Name, "test-client")
	}
	if got.Role != RoleUser {
		t.Errorf("got role %q, want %q", got.Role, RoleUser)
	}

	// Test upsert (update existing)
	client.Name = "updated-name"
	s.UpsertClient(client)
	got, _ = s.GetClient("abc123")
	if got.Name != "updated-name" {
		t.Errorf("upsert didn't update: got %q, want %q", got.Name, "updated-name")
	}
}

func TestListClients(t *testing.T) {
	s := New()

	// Empty store
	clients := s.ListClients()
	if len(clients) != 0 {
		t.Errorf("ListClients on empty store returned %d clients", len(clients))
	}

	// Add clients out of order
	s.UpsertClient(Client{Fingerprint: "c", Name: "charlie", Role: RoleUser})
	s.UpsertClient(Client{Fingerprint: "a", Name: "alice", Role: RoleAdmin})
	s.UpsertClient(Client{Fingerprint: "b", Name: "bob", Role: RoleUser})

	clients = s.ListClients()
	if len(clients) != 3 {
		t.Fatalf("ListClients returned %d clients, want 3", len(clients))
	}

	// Should be sorted by name
	if clients[0].Name != "alice" || clients[1].Name != "bob" || clients[2].Name != "charlie" {
		t.Errorf("clients not sorted by name: %v", clients)
	}
}

func TestSeedFeatures(t *testing.T) {
	s := New()

	s.SeedFeatures(10)

	// Check feature count
	features := s.SearchFeatures("", 100)
	if len(features) != 10 {
		t.Errorf("SeedFeatures(10) created %d features, want 10", len(features))
	}

	// Check feature IDs are correct format
	f, ok := s.GetFeature("FT-000001")
	if !ok {
		t.Error("feature FT-000001 not found")
	}
	if f.ID != "FT-000001" {
		t.Errorf("feature ID = %q, want %q", f.ID, "FT-000001")
	}

	// Reseed should replace features
	s.SeedFeatures(5)
	features = s.SearchFeatures("", 100)
	if len(features) != 5 {
		t.Errorf("reseed with 5 resulted in %d features", len(features))
	}
}

func TestGetFeature(t *testing.T) {
	s := New()
	s.SeedFeatures(5)

	// Existing feature
	f, ok := s.GetFeature("FT-000001")
	if !ok {
		t.Fatal("GetFeature should return true for existing feature")
	}
	if f.ID != "FT-000001" {
		t.Errorf("wrong feature ID: %s", f.ID)
	}

	// Non-existent feature
	_, ok = s.GetFeature("FT-999999")
	if ok {
		t.Error("GetFeature should return false for non-existent feature")
	}
}

func TestSearchFeatures(t *testing.T) {
	s := New()

	// Manually add features for predictable testing
	s.mu.Lock()
	s.features = map[string]Feature{
		"FT-001": {ID: "FT-001", Name: "Authentication", Summary: "User login flow"},
		"FT-002": {ID: "FT-002", Name: "Authorization", Summary: "Permission checks"},
		"FT-003": {ID: "FT-003", Name: "Billing", Summary: "Payment processing"},
	}
	s.featureIDs = []string{"FT-001", "FT-002", "FT-003"}
	s.mu.Unlock()

	tests := []struct {
		name  string
		query string
		limit int
		want  int
	}{
		{"empty query returns all", "", 10, 3},
		{"limit works", "", 2, 2},
		{"search by ID", "FT-001", 10, 1},
		{"search by name", "auth", 10, 2},       // matches Authentication and Authorization
		{"search by summary", "payment", 10, 1}, // matches Billing
		{"case insensitive", "AUTHENTICATION", 10, 1},
		{"no match", "xyz", 10, 0},
		{"zero limit uses default", "", 0, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.SearchFeatures(tt.query, tt.limit)
			if len(got) != tt.want {
				t.Errorf("SearchFeatures(%q, %d) returned %d results, want %d",
					tt.query, tt.limit, len(got), tt.want)
			}
		})
	}
}

func TestSuggest(t *testing.T) {
	s := New()
	s.SeedFeatures(20)

	// Suggest should return same results as SearchFeatures
	suggestions := s.Suggest("", 5)
	if len(suggestions) != 5 {
		t.Errorf("Suggest returned %d items, want 5", len(suggestions))
	}
}

func TestLeftPadInt(t *testing.T) {
	tests := []struct {
		n     int
		width int
		want  string
	}{
		{1, 6, "000001"},
		{123, 6, "000123"},
		{123456, 6, "123456"},
		{1234567, 6, "1234567"}, // longer than width
		{0, 3, "000"},
	}

	for _, tt := range tests {
		got := leftPadInt(tt.n, tt.width)
		if got != tt.want {
			t.Errorf("leftPadInt(%d, %d) = %q, want %q", tt.n, tt.width, got, tt.want)
		}
	}
}

func TestToTitle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello world", "Hello World"},
		{"HELLO WORLD", "Hello World"},
		{"hello", "Hello"},
		{"", ""},
		{"a b c", "A B C"},
	}

	for _, tt := range tests {
		got := toTitle(tt.input)
		if got != tt.want {
			t.Errorf("toTitle(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMatchFeature(t *testing.T) {
	f := Feature{
		ID:      "FT-001",
		Name:    "Authentication",
		Summary: "User login flow",
	}

	// Note: matchFeature expects query to already be lowercased
	// (SearchFeatures does the lowercasing before calling matchFeature)
	tests := []struct {
		query string
		want  bool
	}{
		{"ft-001", true},         // ID match
		{"auth", true},           // name match
		{"login", true},          // summary match
		{"xyz", false},           // no match
		{"ft", true},             // partial ID (lowercase)
		{"authentication", true}, // full name match (lowercase)
	}

	for _, tt := range tests {
		got := matchFeature(f, tt.query)
		if got != tt.want {
			t.Errorf("matchFeature(f, %q) = %v, want %v", tt.query, got, tt.want)
		}
	}
}

func TestConcurrentAccess(_ *testing.T) {
	s := New()
	s.SeedFeatures(100)

	done := make(chan bool)

	// Concurrent reads
	for range 10 {
		go func() {
			for range 100 {
				s.SearchFeatures("", 10)
				s.GetFeature("FT-000001")
				s.ListClients()
			}
			done <- true
		}()
	}

	// Concurrent writes
	for i := range 5 {
		go func(id int) {
			for j := range 50 {
				s.UpsertClient(Client{
					Fingerprint: "fp" + string(rune(id)) + string(rune(j)),
					Name:        "test",
					Role:        RoleUser,
				})
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for range 15 {
		<-done
	}
}

// TestFingerprintDeterministic ensures fingerprint is deterministic
func TestFingerprintDeterministic(t *testing.T) {
	cert := &x509.Certificate{
		Raw: []byte("deterministic test data"),
		Subject: pkix.Name{
			CommonName: "test",
		},
	}

	fp1 := FingerprintSHA256(cert)
	fp2 := FingerprintSHA256(cert)

	if fp1 != fp2 {
		t.Error("fingerprint should be deterministic")
	}
}
