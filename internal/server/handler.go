package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"fsdriver/internal/engine"
)

type Server struct {
	SessionManager *SessionManager
}

type CreateSessionRequest struct {
	Directories     []string `json:"directories"`
	IncludePatterns []string `json:"include_patterns,omitempty"`
	ExcludePatterns []string `json:"exclude_patterns,omitempty"`
}

type CreateSessionResponse struct {
	SessionID string `json:"session_id"`
	StreamURL string `json:"stream_url"`
}

func NewServer(sm *SessionManager) *Server {
	return &Server{SessionManager: sm}
}

// RegisterRoutes binds all API routes to the HTTP multiplexer
func (s *Server) RegisterRoutes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /sessions", s.handleCreateSession)
	mux.HandleFunc("GET /sessions/", s.handleStreamSession)
	mux.HandleFunc("PUT /sessions/", s.handleUpdateFilters)
	mux.HandleFunc("DELETE /sessions/", s.handleDeleteSession)

	return mux
}

// handleCreateSession handles POST /sessions
func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	if len(req.Directories) == 0 {
		http.Error(w, `{"error": "at least one directory must be specified"}`, http.StatusBadRequest)
		return
	}

	filters := engine.FilterRules{
		IncludePatterns: req.IncludePatterns,
		ExcludePatterns: req.ExcludePatterns,
	}

	session, err := s.SessionManager.CreateSession(req.Directories, filters)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CreateSessionResponse{
		SessionID: session.ID,
		StreamURL: "/sessions/" + session.ID + "/stream",
	})
}

// handleStreamSession handles GET /sessions/{id}/stream (SSE)
func (s *Server) handleStreamSession(w http.ResponseWriter, r *http.Request) {
	// Expected path format: /sessions/{id}/stream
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/sessions/"), "/")
	if len(parts) < 2 || parts[1] != "stream" {
		http.NotFound(w, r)
		return
	}
	sessionID := parts[0]

	session, exists := s.SessionManager.GetSession(sessionID)
	if !exists {
		http.Error(w, `{"error": "session not found"}`, http.StatusNotFound)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send initial connection confirmation event
	_, _ = w.Write([]byte("event: connected\ndata: {\"session_id\":\"" + sessionID + "\"}\n\n"))
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			// Client disconnected HTTP connection
			return
		case <-session.Ctx.Done():
			// Session was terminated server-side
			return
		case event, ok := <-session.OutputChan:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, err = w.Write([]byte("data: " + string(data) + "\n\n"))
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleUpdateFilters handles PUT /sessions/{id}/filters (Dynamic Runtime Update)
func (s *Server) handleUpdateFilters(w http.ResponseWriter, r *http.Request) {
	// Expected path format: /sessions/{id}/filters
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/sessions/"), "/")
	if len(parts) < 2 || parts[1] != "filters" {
		http.NotFound(w, r)
		return
	}
	sessionID := parts[0]

	session, exists := s.SessionManager.GetSession(sessionID)
	if !exists {
		http.Error(w, `{"error": "session not found"}`, http.StatusNotFound)
		return
	}

	var newFilters engine.FilterRules
	if err := json.NewDecoder(r.Body).Decode(&newFilters); err != nil {
		http.Error(w, `{"error": "invalid filter rules payload"}`, http.StatusBadRequest)
		return
	}

	// Safely update filters on the fly without closing the stream
	session.Engine.UpdateFilters(newFilters)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "filters_updated"}`))
}

// handleDeleteSession handles DELETE /sessions/{id}
func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	// Expected path format: /sessions/{id}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/sessions/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	sessionID := parts[0]

	success := s.SessionManager.RemoveSession(sessionID)
	if !success {
		http.Error(w, `{"error": "session not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "session_closed"}`))
}

