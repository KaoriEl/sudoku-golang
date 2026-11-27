package admin

import (
	"context"
	"net/http"
	"sudoku-golang/internal/engine"
	"sync"
	"time"
)

const (
	diskUsageTTL     = 60 * time.Second
	diskUsageTimeout = 30 * time.Second
)

type HostMetrics struct {
	Info         *engine.Info      `json:"info"`
	InfoError    string            `json:"infoError"`
	Disk         *engine.DiskUsage `json:"disk"`
	DiskError    string            `json:"diskError"`
	DiskAt       *time.Time        `json:"diskAt"`
	Socket       string            `json:"socket"`
	LastSample   *time.Time        `json:"lastSample"`
	CollectError string            `json:"collectError"`
	Background   bool              `json:"background"`
	LiveInterval int64             `json:"liveInterval"`
	BgInterval   int64             `json:"backgroundInterval"`
}

type diskCache struct {
	mu  sync.Mutex
	at  time.Time
	val *engine.DiskUsage
	err string
}

func (d *diskCache) invalidate() {
	d.mu.Lock()
	d.at = time.Time{}
	d.mu.Unlock()
}

// diskUsage serves /system/df from a 60 s cache; the mutex also collapses concurrent refreshes into
// one engine call (df makes Docker walk every layer and volume).
func (s *Server) diskUsage(ctx context.Context, refresh bool) (*engine.DiskUsage, string, time.Time) {
	d := &s.svc.df
	d.mu.Lock()
	defer d.mu.Unlock()
	if !refresh && time.Since(d.at) < diskUsageTTL {
		return d.val, d.err, d.at
	}
	dctx, cancel := context.WithTimeout(ctx, diskUsageTimeout)
	defer cancel()
	du, err := s.svc.engine.DiskUsage(dctx)
	d.at = time.Now().UTC()
	if err != nil {
		d.err = err.Error()
		return d.val, d.err, d.at
	}
	d.val, d.err = &du, ""
	return d.val, "", d.at
}

func (s *Server) handleMetricsHost(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()

	live, bg := s.svc.metrics.Intervals()
	out := HostMetrics{
		Socket:       s.svc.engine.Socket(),
		Background:   s.svc.metrics.Background(),
		LiveInterval: live.Milliseconds(),
		BgInterval:   bg.Milliseconds(),
	}
	if info, err := s.svc.engine.Info(ctx); err != nil {
		out.InfoError = err.Error()
	} else {
		out.Info = &info
	}
	du, derr, at := s.diskUsage(ctx, r.URL.Query().Get("refresh") == "1")
	out.Disk, out.DiskError = du, derr
	if !at.IsZero() {
		out.DiskAt = &at
	}
	if last, cerr := s.svc.metrics.Status(); !last.IsZero() {
		lt := last.UTC()
		out.LastSample, out.CollectError = &lt, cerr
	} else {
		out.CollectError = cerr
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleMetricsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeNoStream(w)
		return
	}
	ch, history := s.svc.metrics.Subscribe()
	defer s.svc.metrics.Unsubscribe(ch)

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := writeSSE(w, "history", history); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(logHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case tick := <-ch:
			if err := writeSSE(w, "tick", tick); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
