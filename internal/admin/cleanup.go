package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sudoku-golang/internal/cleanup"
	"time"
)

const (
	cleanupScanTimeout = 2 * time.Minute
	cleanupRunTimeout  = 15 * time.Minute
)

const (
	codeInvalid  = "invalid"
	codeNotFound = "not_found"
	codeNoStream = "no_stream"
)

var errExclusiveBusy = errors.New("идёт очистка диска или применение конфига — дождитесь окончания")

func (s *Server) handleCleanupPreview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cleanupScanTimeout)
	defer cancel()
	p, err := cleanup.Scan(ctx, s.svc.engine, time.Now())
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleCleanupRun(w http.ResponseWriter, r *http.Request) {
	var req cleanup.Request
	if err := decodeBody(r, &req); err != nil {
		s.writeError(w, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error(), Code: codeInvalid})
		return
	}
	// Exclusive with runs: a `compose up` racing the cleanup could recreate a container or pull an
	// image while it is being deleted.
	if err := s.svc.runs.tryExclusive(); err != nil {
		if errors.Is(err, errExclusiveBusy) {
			writeJSON(w, http.StatusConflict, apiError{Error: err.Error(), Code: "cleanup_in_progress"})
			return
		}
		s.writeError(w, err)
		return
	}
	defer s.svc.runs.endExclusive()

	// Detached from the request so closing the tab does not abort a delete halfway.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), cleanupRunTimeout)
	defer cancel()
	res, err := cleanup.Execute(ctx, s.svc.engine, req, time.Now())
	if err != nil {
		s.writeError(w, err)
		return
	}
	for _, it := range res.Items {
		s.log.Info("cleanup", slog.String("kind", it.Kind), slog.String("id", it.ID), slog.String("name", it.Name),
			slog.Bool("ok", it.OK), slog.String("error", it.Error), slog.Uint64("size", it.Size))
	}
	s.svc.df.invalidate()
	writeJSON(w, http.StatusOK, res)
}

func writeNoStream(w http.ResponseWriter) {
	writeJSON(w, http.StatusInternalServerError, apiError{Error: "streaming unsupported", Code: codeNoStream})
}
