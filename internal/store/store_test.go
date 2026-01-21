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

func TestCreateFeature(t *testing.T) {
	s := New()

	// Create first feature
	f1 := s.CreateFeature("Auth", "User login flow", "Security Team", []string{"auth", "security"})
	if f1.ID != "FT-000001" {
		t.Errorf("first feature ID = %q, want %q", f1.ID, "FT-000001")
	}
	if f1.Name != "Auth" {
		t.Errorf("name = %q, want %q", f1.Name, "Auth")
	}
	if f1.Summary != "User login flow" {
		t.Errorf("summary = %q, want %q", f1.Summary, "User login flow")
	}
	if f1.Owner != "Security Team" {
		t.Errorf("owner = %q, want %q", f1.Owner, "Security Team")
	}
	if len(f1.Tags) != 2 || f1.Tags[0] != "auth" || f1.Tags[1] != "security" {
		t.Errorf("tags = %v, want [auth, security]", f1.Tags)
	}
	if f1.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}

	// Create second feature - should auto-increment
	f2 := s.CreateFeature("Billing", "Payment processing", "", nil)
	if f2.ID != "FT-000002" {
		t.Errorf("second feature ID = %q, want %q", f2.ID, "FT-000002")
	}

	// Verify features are retrievable
	got1, ok := s.GetFeature("FT-000001")
	if !ok {
		t.Fatal("first feature should be retrievable")
	}
	if got1.Name != "Auth" {
		t.Errorf("retrieved name = %q, want %q", got1.Name, "Auth")
	}

	got2, ok := s.GetFeature("FT-000002")
	if !ok {
		t.Fatal("second feature should be retrievable")
	}
	if got2.Name != "Billing" {
		t.Errorf("retrieved name = %q, want %q", got2.Name, "Billing")
	}
}

func TestCreateFeature_WithExistingFeatures(t *testing.T) {
	s := New()
	s.SeedFeatures(5) // Seeds FT-000001 through FT-000005

	// New feature should get FT-000006
	f := s.CreateFeature("New Feature", "Test", "", nil)
	if f.ID != "FT-000006" {
		t.Errorf("feature ID = %q, want %q", f.ID, "FT-000006")
	}

	// Verify we now have 6 features
	features := s.SearchFeatures("", 100)
	if len(features) != 6 {
		t.Errorf("total features = %d, want 6", len(features))
	}
}

func TestCreateFeature_Concurrent(t *testing.T) {
	s := New()
	done := make(chan string, 10)

	// Create features concurrently
	for i := range 10 {
		go func(idx int) {
			name := "Feature " + string(rune('A'+idx))
			f := s.CreateFeature(name, "Summary", "", nil)
			done <- f.ID
		}(i)
	}

	// Collect all IDs
	ids := make(map[string]bool)
	for range 10 {
		id := <-done
		if ids[id] {
			t.Errorf("duplicate ID created: %s", id)
		}
		ids[id] = true
	}

	// Should have 10 unique IDs
	if len(ids) != 10 {
		t.Errorf("created %d unique IDs, want 10", len(ids))
	}
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkSearchFeatures(b *testing.B) {
	s := New()
	s.SeedFeatures(200)

	b.ResetTimer()
	for range b.N {
		_ = s.SearchFeatures("enable", 20)
	}
}

func BenchmarkSearchFeatures_EmptyQuery(b *testing.B) {
	s := New()
	s.SeedFeatures(200)

	b.ResetTimer()
	for range b.N {
		_ = s.SearchFeatures("", 20)
	}
}

func BenchmarkSearchFeatures_NoMatch(b *testing.B) {
	s := New()
	s.SeedFeatures(200)

	b.ResetTimer()
	for range b.N {
		_ = s.SearchFeatures("xyznonexistent", 20)
	}
}

func BenchmarkGetFeature(b *testing.B) {
	s := New()
	s.SeedFeatures(200)

	b.ResetTimer()
	for range b.N {
		_, _ = s.GetFeature("FT-000100")
	}
}

func BenchmarkCreateFeature(b *testing.B) {
	s := New()

	b.ResetTimer()
	for range b.N {
		s.CreateFeature("Test", "Summary", "Owner", []string{"tag"})
	}
}

func BenchmarkUpsertClient(b *testing.B) {
	s := New()
	client := Client{
		Fingerprint: "benchmark-fp",
		Name:        "benchmark-client",
		Role:        RoleUser,
		CreatedAt:   time.Now(),
	}

	b.ResetTimer()
	for range b.N {
		s.UpsertClient(client)
	}
}

func BenchmarkGetClient(b *testing.B) {
	s := New()
	s.UpsertClient(Client{
		Fingerprint: "benchmark-fp",
		Name:        "benchmark-client",
		Role:        RoleUser,
		CreatedAt:   time.Now(),
	})

	b.ResetTimer()
	for range b.N {
		_, _ = s.GetClient("benchmark-fp")
	}
}

func BenchmarkListClients(b *testing.B) {
	s := New()
	for i := range 100 {
		s.UpsertClient(Client{
			Fingerprint: "fp-" + leftPadInt(i, 3),
			Name:        "client-" + leftPadInt(i, 3),
			Role:        RoleUser,
			CreatedAt:   time.Now(),
		})
	}

	b.ResetTimer()
	for range b.N {
		_ = s.ListClients()
	}
}

func BenchmarkMatchFeature(b *testing.B) {
	f := Feature{
		ID:      "FT-000123",
		Name:    "Enable Dark Mode Toggle",
		Summary: "Allow users to switch between light and dark themes",
	}

	b.ResetTimer()
	for range b.N {
		_ = matchFeature(f, "dark")
	}
}

func BenchmarkFingerprintSHA256(b *testing.B) {
	cert := &x509.Certificate{
		Raw: []byte("benchmark certificate data for fingerprinting"),
	}

	b.ResetTimer()
	for range b.N {
		_ = FingerprintSHA256(cert)
	}
}

func BenchmarkLeftPadInt(b *testing.B) {
	for range b.N {
		_ = leftPadInt(12345, 6)
	}
}

func BenchmarkSeedFeatures(b *testing.B) {
	s := New()

	b.ResetTimer()
	for range b.N {
		s.SeedFeatures(200)
	}
}
