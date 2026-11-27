package admin

import (
	"context"
	"net/http"
	"sudoku-golang/internal/doctor"
	"time"
)

const doctorTimeout = 2 * time.Minute

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), doctorTimeout)
	defer cancel()
	writeJSON(w, http.StatusOK, doctor.Run(ctx, doctor.Env{Cfg: s.svc.cfg, Runner: s.svc.docker, Engine: s.svc.engine}))
}
