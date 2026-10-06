package httpserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/marchi/marchikeeper/internal/znodes"
)

type Server struct {
	store *znodes.Store
	mux   *http.ServeMux
}

func New(store *znodes.Store) *Server {
	if store == nil {
		store = znodes.New()
	}
	s := &Server{store: store, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("POST /sessions", s.handleCreateSession)
	s.mux.HandleFunc("POST /sessions/{id}/ping", s.handlePing)
	s.mux.HandleFunc("DELETE /sessions/{id}", s.handleCloseSession)
	s.mux.HandleFunc("GET /znodes", s.handleGetOrChildren)
	s.mux.HandleFunc("PUT /znodes/{path...}", s.handleCreate)
	s.mux.HandleFunc("GET /znodes/{path...}", s.handleGetOrChildren)
	s.mux.HandleFunc("POST /znodes/{path...}", s.handleSet)
	s.mux.HandleFunc("DELETE /znodes/{path...}", s.handleDelete)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.store.Expire()
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TimeoutMs int64 `json:"timeoutMs"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	id := s.store.CreateSession(time.Duration(req.TimeoutMs) * time.Millisecond)
	writeJSON(w, http.StatusCreated, map[string]any{"sessionId": id})
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.Ping(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessionId": id, "pong": true})
}

func (s *Server) handleCloseSession(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.CloseSession(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessionId": id, "closed": true})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	path := zpath(r)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	flags := znodes.CreateFlags{
		Sequential: r.URL.Query().Get("sequential") == "1",
		Ephemeral:  r.URL.Query().Get("ephemeral") == "1",
		SessionID:  sessionID(r),
	}
	created, st, err := s.store.Create(path, body, flags)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"path": created, "data": jsonData(body), "stat": st})
}

func (s *Server) handleGetOrChildren(w http.ResponseWriter, r *http.Request) {
	path, listChildren := splitChildren(zpath(r))
	if listChildren {
		kids, st, err := s.store.Children(path)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"path": path, "children": kids, "stat": st})
		return
	}
	data, st, err := s.store.Get(path)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "data": jsonData(data), "stat": st})
}

func (s *Server) handleSet(w http.ResponseWriter, r *http.Request) {
	path := zpath(r)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ver, err := queryVersion(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	st, err := s.store.Set(path, body, ver)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "data": jsonData(body), "stat": st})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	path := zpath(r)
	ver, err := queryVersion(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.Delete(path, ver); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "deleted": true})
}

func sessionID(r *http.Request) int64 {
	raw := r.Header.Get("X-Session-Id")
	if raw == "" {
		return 0
	}
	id, _ := strconv.ParseInt(raw, 10, 64)
	return id
}

func queryVersion(r *http.Request) (*int32, error) {
	raw := r.URL.Query().Get("version")
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return nil, err
	}
	v := int32(n)
	return &v, nil
}

func zpath(r *http.Request) string {
	p := r.PathValue("path")
	p = strings.Trim(p, "/")
	if p == "" {
		return "/"
	}
	return "/" + p
}

func splitChildren(path string) (string, bool) {
	if path == "/children" {
		return "/", true
	}
	if strings.HasSuffix(path, "/children") {
		return strings.TrimSuffix(path, "/children"), true
	}
	return path, false
}

func jsonData(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(b, &v) == nil {
		return v
	}
	return string(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]any{"error": err.Error()})
}

func writeStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, znodes.ErrNoNode), errors.Is(err, znodes.ErrNoSession):
		writeErr(w, http.StatusNotFound, err)
	case errors.Is(err, znodes.ErrNodeExists), errors.Is(err, znodes.ErrNotEmpty), errors.Is(err, znodes.ErrBadVersion):
		writeErr(w, http.StatusConflict, err)
	case errors.Is(err, znodes.ErrBadPath), errors.Is(err, znodes.ErrNoParent), errors.Is(err, znodes.ErrRoot), errors.Is(err, znodes.ErrNotEphemeral), errors.Is(err, znodes.ErrEphemeralChild):
		writeErr(w, http.StatusBadRequest, err)
	default:
		writeErr(w, http.StatusInternalServerError, err)
	}
}
