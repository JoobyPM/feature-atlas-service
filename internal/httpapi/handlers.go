package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/JoobyPM/feature-atlas-service/internal/store"
)

// Server holds the application state and provides HTTP handlers.
type Server struct {
	Store *store.Store
}

// Routes returns the HTTP handler with all routes configured.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public API (auth middleware will wrap)
	mux.HandleFunc("/api/v1/me", s.handleMe)
	mux.HandleFunc("/api/v1/features", s.handleFeatures)
	mux.HandleFunc("/api/v1/features/", s.handleFeatureByID)
	mux.HandleFunc("/api/v1/suggest", s.handleSuggest)

	// Admin API (auth + admin middleware will wrap)
	mux.HandleFunc("/admin/v1/clients", s.handleClients)
	mux.HandleFunc("/admin/v1/features/seed", s.handleSeed)

	return mux
}

// HealthRoutes returns routes that don't require authentication.
func (s *Server) HealthRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	return mux
}

// handleHealthz returns basic liveness status.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handleReadyz returns readiness status including feature count.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	features := s.Store.SearchFeatures("", 1)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"features_loaded": len(features) > 0,
	})
}

// handleMe returns information about the authenticated client.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	client := ClientFromContext(r.Context())
	cert := CertFromContext(r.Context())

	resp := map[string]any{
		"name":        client.Name,
		"role":        client.Role,
		"fingerprint": client.Fingerprint,
		"subject":     "",
	}
	if cert != nil {
		resp["subject"] = cert.Subject.String()
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleFeatures handles feature search requests.
func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query().Get("query")
	limit := atoiDefault(r.URL.Query().Get("limit"), 20)

	items := s.Store.SearchFeatures(q, limit)
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
}

// handleFeatureByID handles requests for a specific feature by ID.
func (s *Server) handleFeatureByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/v1/features/")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	f, ok := s.Store.GetFeature(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// handleSuggest handles autocomplete/suggestion requests.
func (s *Server) handleSuggest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query().Get("query")
	limit := atoiDefault(r.URL.Query().Get("limit"), 10)

	items := s.Store.Suggest(q, limit)

	type sugg struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Summary string `json:"summary"`
	}
	out := make([]sugg, 0, len(items))
	for _, f := range items {
		out = append(out, sugg{ID: f.ID, Name: f.Name, Summary: f.Summary})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// handleSeed handles requests to reseed the feature catalog.
func (s *Server) handleSeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	count := atoiDefault(r.URL.Query().Get("count"), 200)
	s.Store.SeedFeatures(count)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "seeded": count})
}

// handleClients handles client registration and listing.
func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"items": s.Store.ListClients()})
		return

	case http.MethodPost:
		body, err := readAllLimit(r.Body, 1<<20)
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		var req struct {
			Name    string `json:"name"`
			Role    string `json:"role"`
			CertPEM string `json:"cert_pem"`
		}
		if unmarshalErr := json.Unmarshal(body, &req); unmarshalErr != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" || strings.TrimSpace(req.CertPEM) == "" {
			http.Error(w, "name and cert_pem required", http.StatusBadRequest)
			return
		}

		cert, err := parseCertPEM(req.CertPEM)
		if err != nil {
			http.Error(w, "invalid cert_pem: "+err.Error(), http.StatusBadRequest)
			return
		}

		role := store.RoleUser
		if strings.ToLower(req.Role) == "admin" {
			role = store.RoleAdmin
		}

		fp := store.FingerprintSHA256(cert)
		s.Store.UpsertClient(store.Client{
			Fingerprint: fp,
			Name:        req.Name,
			Role:        role,
			CreatedAt:   time.Now(),
		})

		writeJSON(w, http.StatusCreated, map[string]any{
			"fingerprint": fp,
			"name":        req.Name,
			"role":        role,
			"subject":     cert.Subject.String(),
		})
		return

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	//nolint:errchkjson // response writer errors handled by server
	json.NewEncoder(w).Encode(v)
}

// atoiDefault parses a string as an integer, returning def on error.
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
