package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sudoku-golang/internal/composeedit"
	"time"
)

const (
	containerActionTimeout = 2 * time.Minute
	snapshotTimeout        = 20 * time.Second
	readHeaderTimeout      = 10 * time.Second
	idleTimeout            = 2 * time.Minute
	maxHeaderBytes         = 64 * 1024
	shutdownTimeout        = 10 * time.Second
)

// contentSecurityPolicy: the SPA is fully self-hosted (Vite bundle, inline styles from React).
// frame-ancestors 'none' stops a random site from framing the panel and clickjacking its buttons
// (the loopback Host check alone does not: an iframe sends the right Host).
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; " +
	"base-uri 'none'; form-action 'self'; object-src 'none'"

type Server struct {
	log     *slog.Logger
	svc     *Service
	ui      http.Handler
	baseCtx context.Context
	unix    bool
}

type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func NewServer(log *slog.Logger, svc *Service, ui http.Handler) *Server {
	return &Server{log: log, svc: svc, ui: ui}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/alerts", s.handleAlerts)
	mux.HandleFunc("POST /api/containers/{id}/{action}", s.handleContainerAction)
	mux.HandleFunc("POST /api/actions/{action}", s.handleBulk)
	mux.HandleFunc("GET /api/runs", s.handleRuns)
	mux.HandleFunc("GET /api/runs/{id}", s.handleRun)
	mux.HandleFunc("GET /api/containers/{id}/logs", s.handleLogs)
	mux.HandleFunc("GET /api/containers/{id}/logs/download", s.handleLogsDownload)
	mux.HandleFunc("GET /api/logs/project", s.handleProjectLogs)
	mux.HandleFunc("GET /api/containers/{id}/exec", s.handleExec)
	mux.HandleFunc("GET /api/containers/{id}/commands", s.handleCommands)
	mux.HandleFunc("GET /api/containers/{id}/config", s.handleServiceConfig)
	mux.HandleFunc("POST /api/containers/{id}/rebuild", s.handleRebuild)
	mux.HandleFunc("GET /api/ports", s.handlePorts)
	mux.HandleFunc("POST /api/ports", s.handleAddPort)
	mux.HandleFunc("POST /api/ports/remove", s.handleRemovePort)
	mux.HandleFunc("POST /api/ports/apply", s.handleApplyPorts)
	mux.HandleFunc("POST /api/preflight", s.handlePreflight)
	mux.HandleFunc("GET /api/domains", s.handleDomains)
	mux.HandleFunc("POST /api/domains/hosts", s.handleWriteHosts)
	mux.HandleFunc("POST /api/domains/cert", s.handleIssueCert)
	mux.HandleFunc("GET /api/settings", s.handleSettings)
	mux.HandleFunc("POST /api/settings/prefs", s.handlePrefs)
	mux.HandleFunc("POST /api/services/disabled", s.handleToggleDisabled)
	mux.HandleFunc("POST /api/selection", s.handleSelection)
	mux.HandleFunc("GET /api/sets", s.handleSets)
	mux.HandleFunc("POST /api/sets", s.handleSaveSet)
	mux.HandleFunc("POST /api/sets/delete", s.handleDeleteSet)
	mux.HandleFunc("POST /api/sets/{name}/{action}", s.handleRunSet)
	mux.HandleFunc("GET /api/updates", s.handleUpdates)
	mux.HandleFunc("POST /api/updates/check", s.handleCheckUpdates)
	mux.HandleFunc("GET /api/backups", s.handleBackups)
	mux.HandleFunc("POST /api/backups", s.handleCreateBackups)
	mux.HandleFunc("POST /api/backups/restore", s.handleRestoreBackup)
	mux.HandleFunc("POST /api/backups/delete", s.handleDeleteBackup)
	mux.HandleFunc("GET /api/backups/download", s.handleDownloadBackup)
	mux.HandleFunc("GET /api/doctor", s.handleDoctor)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/config", s.handleSaveConfig)
	mux.HandleFunc("GET /api/metrics/stream", s.handleMetricsStream)
	mux.HandleFunc("GET /api/metrics/host", s.handleMetricsHost)
	mux.HandleFunc("POST /api/settings/autostart", s.handleAutostart)
	mux.HandleFunc("POST /api/settings/metrics", s.handleMetricsPrefs)
	mux.HandleFunc("GET /api/cleanup", s.handleCleanupPreview)
	mux.HandleFunc("POST /api/cleanup", s.handleCleanupRun)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, apiError{Error: "not found", Code: codeNotFound})
	})
	mux.Handle("/", s.ui)
	return s.guard(s.auth(mux))
}

func isLoopbackHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	h = strings.Trim(h, "[]")
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// crossSiteFetch reports a request that a browser made on behalf of another site. Fetch metadata is
// sent by all current browsers on every request, so this also covers GETs that start work on the
// server (an <img> or EventSource pointed at /api/containers/x/logs spawns `docker logs`). Requests
// without the header (curl, old browsers) fall through to the Origin/Content-Type checks.
func crossSiteFetch(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site", "same-site":
		return true
	}
	return false
}

func securityHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Content-Security-Policy", contentSecurityPolicy)
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w.Header())
		if !s.unix && !isLoopbackHost(r.Host) {
			writeJSON(w, http.StatusForbidden, apiError{Error: "host not allowed", Code: "forbidden_host"})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && crossSiteFetch(r) {
			writeJSON(w, http.StatusForbidden, apiError{Error: "cross-site request rejected", Code: "forbidden_origin"})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					writeJSON(w, http.StatusForbidden, apiError{Error: "cross-origin request rejected", Code: "forbidden_origin"})
					return
				}
			}
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				writeJSON(w, http.StatusUnsupportedMediaType, apiError{Error: "content-type must be application/json", Code: "bad_content_type"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnknownAction):
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error(), Code: "unknown_action"})
	case errors.Is(err, ErrContainerNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Error: err.Error(), Code: "container_not_found"})
	case errors.Is(err, ErrNotManaged):
		writeJSON(w, http.StatusForbidden, apiError{Error: err.Error(), Code: "not_managed"})
	case errors.Is(err, ErrConfigInvalid):
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error(), Code: codeInvalid})
	case errors.Is(err, errRestartBusy), errors.Is(err, errExclusiveBusy):
		writeJSON(w, http.StatusConflict, apiError{Error: err.Error(), Code: "busy"})
	case errors.Is(err, ErrRunInProgress):
		writeJSON(w, http.StatusConflict, apiError{Error: err.Error(), Code: "run_in_progress"})
	case errors.Is(err, errNotRunning):
		writeJSON(w, http.StatusConflict, apiError{Error: err.Error(), Code: "not_running"})
	case errors.Is(err, ErrPortConflict), errors.Is(err, ErrStale):
		writeJSON(w, http.StatusConflict, apiError{Error: err.Error(), Code: "conflict"})
	case errors.Is(err, ErrInvalidPort), errors.Is(err, errBadBody), errors.Is(err, ErrBadInterval),
		errors.Is(err, composeedit.ErrInvalidName), errors.Is(err, composeedit.ErrEntryPointExists),
		errors.Is(err, composeedit.ErrFlowStyle):
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error(), Code: codeInvalid})
	case errors.Is(err, ErrUnknownService):
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error(), Code: "unknown_service"})
	case errors.Is(err, ErrNoInfraCompose), errors.Is(err, ErrNoTraefik):
		writeJSON(w, http.StatusNotFound, apiError{Error: err.Error(), Code: "no_infra"})
	case errors.As(err, new(*userError)):
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error(), Code: codeInvalid})
	default:
		s.log.Error("request failed", slog.Any("error", err))
		writeJSON(w, http.StatusBadGateway, apiError{Error: err.Error(), Code: "docker_error"})
	}
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()

	snap, err := s.svc.Snapshot(ctx)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleContainerAction(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), containerActionTimeout)
	defer cancel()

	c, err := s.svc.ContainerAction(ctx, r.PathValue("id"), r.PathValue("action"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleBulk(w http.ResponseWriter, r *http.Request) {
	run, err := s.svc.StartBulk(r.Context(), r.PathValue("action"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	run := s.svc.Run(r.PathValue("id"))
	if run == nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "run not found", Code: "run_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

var errNotLoopback = errors.New("админка слушает только loopback-адрес (127.0.0.1, ::1, localhost) или unix-сокет")

const (
	unixPrefix   = "unix:"
	rebindWindow = 10 * time.Second
)

// IsUnixAddr reports an ADMIN_ADDR / --addr of the form unix:/path/to.sock.
func IsUnixAddr(addr string) bool {
	return strings.HasPrefix(addr, unixPrefix)
}

func unixPath(addr string) string {
	return strings.TrimPrefix(strings.TrimPrefix(addr, unixPrefix), "//")
}

// DisplayURL is what to print for the operator.
func DisplayURL(addr string) string {
	if IsUnixAddr(addr) {
		return "unix://" + unixPath(addr) + " (curl --unix-socket … http://sudoku/)"
	}
	return "http://" + addr
}

// listen binds addr. After a config-triggered re-exec the old process may still hold the port for a
// moment (always on Windows, where it is a separate process), so it retries for a few seconds.
func listen(ctx context.Context, addr string) (net.Listener, error) {
	var lc net.ListenConfig
	if IsUnixAddr(addr) {
		path := unixPath(addr)
		if st, err := os.Stat(path); err == nil && st.Mode()&os.ModeSocket != 0 {
			_ = os.Remove(path) // stale socket of a previous run
		}
		l, err := lc.Listen(ctx, "unix", path)
		if err != nil {
			return nil, fmt.Errorf("listen %s: %w", path, err)
		}
		_ = os.Chmod(path, 0o600) // only the owner may talk to the admin
		return l, nil
	}
	deadline := time.Now().Add(rebindWindow)
	for {
		l, err := lc.Listen(ctx, "tcp", addr)
		if err == nil || os.Getenv("SUDOKU_REEXEC") != "1" || time.Now().After(deadline) {
			if err != nil {
				return nil, fmt.Errorf("listen %s: %w", addr, err)
			}
			return l, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	if !IsUnixAddr(addr) {
		if host, _, err := net.SplitHostPort(addr); err != nil || host == "" || !isLoopbackHost(host) {
			return fmt.Errorf("%w: %q", errNotLoopback, addr)
		}
	}
	s.unix = IsUnixAddr(addr)
	s.baseCtx = ctx
	s.svc.runCtx = ctx
	s.svc.addr = addr
	l, err := listen(ctx, addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(l) }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve %s: %w", addr, err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		if s.unix {
			_ = os.Remove(unixPath(addr))
		}
		return nil
	}
}

var errBadBody = errors.New("invalid request body")

const maxBodySize = 64 * 1024

func decodeBody(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBodySize))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %w", errBadBody, err)
	}
	return nil
}

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	out, err := s.svc.ContainerCommands(ctx, r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRebuild(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NoCache bool `json:"noCache"`
	}
	if err := decodeBody(r, &body); err != nil {
		s.writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	run, err := s.svc.RebuildService(ctx, r.PathValue("id"), body.NoCache)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handlePorts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	view, err := s.svc.Ports(ctx)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleAddPort(w http.ResponseWriter, r *http.Request) {
	var req AddPortRequest
	if err := decodeBody(r, &req); err != nil {
		s.writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	view, err := s.svc.AddPort(ctx, req)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleRemovePort(w http.ResponseWriter, r *http.Request) {
	var req RemovePortRequest
	if err := decodeBody(r, &req); err != nil {
		s.writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	view, err := s.svc.RemovePort(ctx, req)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleApplyPorts(w http.ResponseWriter, _ *http.Request) {
	run, err := s.svc.ApplyPorts()
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.Settings())
}

func (s *Server) handleAutostart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeBody(r, &body); err != nil {
		s.writeError(w, err)
		return
	}
	st, err := s.svc.SetAutostart(body.Enabled)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleToggleDisabled(w http.ResponseWriter, r *http.Request) {
	var req ToggleRequest
	if err := decodeBody(r, &req); err != nil {
		s.writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	res, err := s.svc.SetDisabled(ctx, req)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handlePrefs(w http.ResponseWriter, r *http.Request) {
	var body PrefsUpdate
	if err := decodeBody(r, &body); err != nil {
		s.writeError(w, err)
		return
	}
	st, err := s.svc.UpdatePrefs(body)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleMetricsPrefs(w http.ResponseWriter, r *http.Request) {
	var body MetricsUpdate
	if err := decodeBody(r, &body); err != nil {
		s.writeError(w, err)
		return
	}
	st, err := s.svc.UpdateMetrics(body)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleSelection(w http.ResponseWriter, r *http.Request) {
	var req SelectionRequest
	if err := decodeBody(r, &req); err != nil {
		s.writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	res, err := s.svc.ApplySelection(ctx, req)
	if err != nil {
		s.writeError(w, err)
		return
	}
	status := http.StatusOK
	if res.Run != nil {
		status = http.StatusAccepted
	}
	writeJSON(w, status, res)
}

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.Config())
}

func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	var body ConfigUpdate
	if err := decodeBody(r, &body); err != nil {
		s.writeError(w, err)
		return
	}
	res, err := s.svc.SaveConfig(body)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
