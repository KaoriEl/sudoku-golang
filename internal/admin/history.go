package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sudoku-golang/internal/fsutil"
	"sync"
	"time"
)

const (
	historyKeep     = 100
	historyMaxLimit = 100
)

var runIDRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// RunSummary is one line of the run history.
type RunSummary struct {
	ID         string     `json:"id"`
	Action     string     `json:"action"`
	Target     string     `json:"target"`
	Status     RunStatus  `json:"status"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	Steps      int        `json:"steps"`
	Failed     int        `json:"failed"`
}

func summarize(r *Run) RunSummary {
	sum := RunSummary{
		ID: r.ID, Action: r.Action, Target: r.Target, Status: r.Status, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt,
		Steps: len(r.Steps),
	}
	for _, st := range r.Steps {
		if st.Status == StepFailed {
			sum.Failed++
		}
	}
	return sum
}

// runHistory keeps finished runs in ${ROOT}/sudoku-config/runs/<id>.json (the last 100), so "what
// failed yesterday" survives a restart of the admin. Step output is already capped at 16 KB.
type runHistory struct {
	log    *slog.Logger
	dir    string
	mu     sync.Mutex
	index  []RunSummary
	loaded bool
}

func newRunHistory(log *slog.Logger, root string) *runHistory {
	return &runHistory{log: log, dir: filepath.Join(root, "sudoku-config", "runs")}
}

func (h *runHistory) path(id string) string {
	return filepath.Join(h.dir, id+".json")
}

func (h *runHistory) loadIndexLocked() {
	if h.loaded {
		return
	}
	h.loaded = true
	entries, err := os.ReadDir(h.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".json")
		if e.IsDir() || !runIDRe.MatchString(id) {
			continue
		}
		if r := h.read(id); r != nil {
			h.index = append(h.index, summarize(r))
		}
	}
	h.sortLocked()
}

func (h *runHistory) sortLocked() {
	sort.Slice(h.index, func(i, j int) bool { return h.index[i].StartedAt.After(h.index[j].StartedAt) })
}

func (h *runHistory) read(id string) *Run {
	raw, err := os.ReadFile(h.path(id))
	if err != nil {
		return nil
	}
	var r Run
	if json.Unmarshal(raw, &r) != nil || r.ID != id {
		return nil
	}
	return &r
}

func (h *runHistory) save(r *Run) {
	raw, err := json.Marshal(r)
	if err != nil {
		return
	}
	if err := fsutil.WriteFileAtomicNoBackup(h.path(r.ID), raw); err != nil {
		h.log.Warn("run history not saved", slog.Any("error", err))
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loadIndexLocked()
	h.index = append(h.index, summarize(r))
	h.sortLocked()
	for len(h.index) > historyKeep {
		old := h.index[len(h.index)-1]
		h.index = h.index[:len(h.index)-1]
		_ = os.Remove(h.path(old.ID))
	}
}

func (h *runHistory) load(id string) *Run {
	if !runIDRe.MatchString(id) {
		return nil
	}
	return h.read(id)
}

func (h *runHistory) list(limit int) []RunSummary {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loadIndexLocked()
	n := min(limit, len(h.index))
	return append([]RunSummary{}, h.index[:n]...)
}

// recent returns the newest n runs oldest-first, for the in-memory run manager at startup.
func (h *runHistory) recent(n int) []*Run {
	sums := h.list(n)
	out := make([]*Run, 0, len(sums))
	for i := len(sums) - 1; i >= 0; i-- {
		if r := h.read(sums[i].ID); r != nil {
			out = append(out, r)
		}
	}
	return out
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	limit := historyMaxLimit
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, historyMaxLimit)
	}
	list := s.svc.history.list(limit)
	if cur := s.svc.runs.latestSummary(); cur != nil && cur.Status == RunRunning {
		list = append([]RunSummary{summarize(cur)}, list...)
	}
	writeJSON(w, http.StatusOK, list)
}
