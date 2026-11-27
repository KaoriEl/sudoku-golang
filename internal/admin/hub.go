package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const (
	stateDebounce   = 250 * time.Millisecond
	runDebounce     = 300 * time.Millisecond
	hubBuffer       = 16
	eventsSnapshotT = 20 * time.Second
)

type hubMsg struct {
	event string
	data  []byte
}

// hub fans server-side changes out to every open tab over one SSE stream (/api/events):
//   - "state": the full snapshot, recomputed once per debounce window no matter how many tabs listen;
//   - "run":   the active run with live output while it executes;
//   - "alert": a container crashed, went unhealthy or hit OOM.
//
// Docker events mark the state dirty, so the UI updates within ~250 ms of a change instead of
// polling /api/state every 3 s. Nothing is computed while nobody is subscribed.
type hub struct {
	mu   sync.Mutex
	subs map[chan hubMsg]struct{}

	stateDirty chan struct{}
	runDirty   chan string
	live       atomic.Bool
}

func newHub() *hub {
	return &hub{subs: map[chan hubMsg]struct{}{}, stateDirty: make(chan struct{}, 1), runDirty: make(chan string, 1)}
}

func (h *hub) subscribe() chan hubMsg {
	ch := make(chan hubMsg, hubBuffer)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) unsubscribe(ch chan hubMsg) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

func (h *hub) listeners() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// broadcast never blocks: a subscriber whose buffer is full misses this message (the next state
// snapshot supersedes it anyway).
func (h *hub) broadcast(event string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	msg := hubMsg{event: event, data: data}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (h *hub) markState() {
	select {
	case h.stateDirty <- struct{}{}:
	default:
	}
}

func (h *hub) markRun(id string) {
	select {
	case h.runDirty <- id:
	default:
	}
}

// eventsLive tells the UI whether pushes are flowing (Docker events connected), so it can poll slowly.
func (h *hub) eventsLive() bool {
	return h.live.Load()
}

func (h *hub) start(ctx context.Context, s *Service) {
	go h.loop(ctx, s)
}

func (h *hub) loop(ctx context.Context, s *Service) {
	var stateTimer, runTimer <-chan time.Time
	pendingRun := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stateDirty:
			if stateTimer == nil {
				stateTimer = time.After(stateDebounce)
			}
		case id := <-h.runDirty:
			pendingRun = id
			if runTimer == nil {
				runTimer = time.After(runDebounce)
			}
		case <-stateTimer:
			stateTimer = nil
			if h.listeners() == 0 {
				continue
			}
			sctx, cancel := context.WithTimeout(ctx, eventsSnapshotT)
			snap, err := s.Snapshot(sctx)
			cancel()
			if err == nil {
				h.broadcast("state", snap)
			}
		case <-runTimer:
			runTimer = nil
			if h.listeners() == 0 || pendingRun == "" {
				continue
			}
			if r := s.runs.get(pendingRun); r != nil {
				h.broadcast("run", r)
			}
		}
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeNoStream(w)
		return
	}
	ch := s.svc.hub.subscribe()
	defer s.svc.hub.unsubscribe(ch)

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if snap, err := s.svc.Snapshot(r.Context()); err == nil {
		if err := writeSSE(w, "state", snap); err != nil {
			return
		}
	}
	flusher.Flush()

	heartbeat := time.NewTicker(logHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if _, err := w.Write([]byte("event: " + msg.event + "\ndata: ")); err != nil {
				return
			}
			if _, err := w.Write(msg.data); err != nil {
				return
			}
			if _, err := w.Write([]byte("\n\n")); err != nil {
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
