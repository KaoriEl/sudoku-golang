package admin

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sudoku-golang/internal/engine"
	"sudoku-golang/internal/notify"
	"sync"
	"time"
)

const (
	eventsBackoffMin = 2 * time.Second
	eventsBackoffMax = 30 * time.Second
	expectWindow     = 45 * time.Second
	alertCooldown    = 60 * time.Second
	alertsKept       = 50

	AlertCrash     = "crash"
	AlertUnhealthy = "unhealthy"
	AlertOOM       = "oom"
)

// watchEvents follows the Docker event stream for compose containers. Every lifecycle change
// invalidates the inspect cache for that container and pushes a fresh snapshot to open tabs; crashes,
// failed healthchecks and OOM kills become alerts. On a broken stream it reconnects with backoff and
// the UI falls back to slow polling meanwhile.
func (s *Service) watchEvents(ctx context.Context) {
	backoff := eventsBackoffMin
	filters := map[string][]string{"type": {"container"}, "label": {labelProject}}
	for ctx.Err() == nil {
		connected := time.Now()
		s.hub.live.Store(true)
		s.hub.markState() // something may have changed while disconnected
		err := s.engine.Events(ctx, filters, s.onDockerEvent)
		s.hub.live.Store(false)
		if ctx.Err() != nil {
			return
		}
		if time.Since(connected) > time.Minute {
			backoff = eventsBackoffMin
		}
		s.log.Debug("docker events stream ended, reconnecting", slog.Any("error", err), slog.Duration("in", backoff))
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, eventsBackoffMax)
	}
}

var ignoredActions = []string{
	"exec_create", "exec_start", "exec_die", "exec_detach", "top", "attach", "resize", "export", "commit", "copy",
	"archive-path", "extract-to-dir",
}

func (s *Service) onDockerEvent(ev engine.Event) {
	for _, a := range ignoredActions {
		if strings.HasPrefix(ev.Action, a) {
			return
		}
	}
	s.source.Forget(ev.Actor.ID)
	s.alerts.observe(ev)
	s.hub.markState()
}

type Alert struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Container string    `json:"container"`
	Project   string    `json:"project"`
	Service   string    `json:"service"`
	ExitCode  string    `json:"exitCode"`
	At        time.Time `json:"at"`
}

// alerter turns raw events into operator alerts. A "die" is a crash only when nothing asked the
// container to stop: `docker stop`/compose stop emit "kill" (or our own action marks it expected)
// right before the "die".
type alerter struct {
	s        *Service
	mu       sync.Mutex
	stopping map[string]time.Time
	last     map[string]time.Time
	recent   []Alert
}

func newAlerter(s *Service) *alerter {
	return &alerter{s: s, stopping: map[string]time.Time{}, last: map[string]time.Time{}}
}

// expect marks a container as being stopped/restarted on purpose.
func (a *alerter) expect(id string) {
	a.mu.Lock()
	a.stopping[id] = time.Now()
	a.mu.Unlock()
}

func (a *alerter) observe(ev engine.Event) {
	id := ev.Actor.ID
	attrs := ev.Actor.Attributes
	var kind string
	switch {
	case ev.Action == "kill" || ev.Action == "stop" || ev.Action == "destroy":
		a.expect(id)
		return
	case ev.Action == "die":
		code := attrs["exitCode"]
		if code == "" || code == "0" || a.expected(id) {
			return
		}
		kind = AlertCrash
	case ev.Action == "oom":
		kind = AlertOOM
	case strings.HasPrefix(ev.Action, "health_status") && strings.Contains(ev.Action, "unhealthy"):
		kind = AlertUnhealthy
	default:
		return
	}
	at := time.Now().UTC()
	if ev.TimeNano > 0 {
		at = time.Unix(0, ev.TimeNano).UTC()
	}
	alert := Alert{
		ID: newID(), Kind: kind, Container: attrs["name"], Project: attrs[labelProject], Service: attrs[labelService],
		ExitCode: attrs["exitCode"], At: at,
	}
	if !a.record(id, alert) {
		return
	}
	a.s.hub.broadcast("alert", alert)
	a.s.log.Warn("container alert", slog.String("kind", kind), slog.String("container", alert.Container), slog.String("exitCode", alert.ExitCode))
	go a.desktop(alert)
}

func (a *alerter) expected(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	t, ok := a.stopping[id]
	if ok && time.Since(t) < expectWindow {
		return true
	}
	delete(a.stopping, id)
	return false
}

// record applies the per-container cooldown and keeps the recent list.
func (a *alerter) record(id string, al Alert) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := id + "/" + al.Kind
	if t, ok := a.last[key]; ok && time.Since(t) < alertCooldown {
		return false
	}
	a.last[key] = time.Now()
	for k, t := range a.stopping {
		if time.Since(t) > expectWindow {
			delete(a.stopping, k)
		}
	}
	a.recent = append(a.recent, al)
	if len(a.recent) > alertsKept {
		a.recent = append([]Alert(nil), a.recent[len(a.recent)-alertsKept:]...)
	}
	return true
}

func (a *alerter) list() []Alert {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Alert, len(a.recent))
	for i, al := range a.recent {
		out[len(a.recent)-1-i] = al
	}
	return out
}

var alertTitles = map[string]string{
	AlertCrash:     "Контейнер упал",
	AlertUnhealthy: "Healthcheck не проходит",
	AlertOOM:       "Нехватка памяти (OOM)",
}

func (a *alerter) desktop(al Alert) {
	p, err := a.s.prefs.Get()
	if err != nil || !p.Notifications.Desktop {
		return
	}
	body := al.Container
	if al.ExitCode != "" && al.Kind == AlertCrash {
		body += " · код " + al.ExitCode
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := notify.Send(ctx, "sudoku · "+alertTitles[al.Kind], body); err != nil {
		a.s.log.Debug("desktop notification failed", slog.Any("error", err))
	}
}

func (s *Server) handleAlerts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.alerts.list())
}
