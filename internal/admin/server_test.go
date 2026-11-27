package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sudoku-golang/internal/paths"
	"sync"
	"testing"
)

const (
	loopback  = "127.0.0.1:7777"
	pathState = "/api/state"
	pathStop  = "/api/actions/stop"
	fetchSite = "Sec-Fetch-Site"
)

func guarded() http.Handler {
	s := &Server{log: slog.New(slog.DiscardHandler)}
	return s.guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }))
}

func TestGuard(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		path    string
		host    string
		headers map[string]string
		want    int
	}{
		{"plain get", http.MethodGet, pathState, loopback, nil, http.StatusTeapot},
		{"dns rebinding host", http.MethodGet, pathState, "evil.example:7777", nil, http.StatusForbidden},
		{"cross-site get", http.MethodGet, "/api/containers/x/logs", loopback, map[string]string{fetchSite: "cross-site"}, http.StatusForbidden},
		{"same-site other port", http.MethodGet, pathState, "localhost:7777", map[string]string{fetchSite: "same-site"}, http.StatusForbidden},
		{"same-origin get", http.MethodGet, pathState, loopback, map[string]string{fetchSite: "same-origin"}, http.StatusTeapot},
		{"cross-site page navigation is fine", http.MethodGet, "/", loopback, map[string]string{fetchSite: "cross-site"}, http.StatusTeapot},
		{"post without json", http.MethodPost, pathStop, loopback, nil, http.StatusUnsupportedMediaType},
		{
			"post foreign origin", http.MethodPost, pathStop, loopback,
			map[string]string{"Origin": "http://evil.example", "Content-Type": "application/json"},
			http.StatusForbidden,
		},
		{
			"post same origin", http.MethodPost, pathStop, loopback,
			map[string]string{"Origin": "http://" + loopback, "Content-Type": "application/json"},
			http.StatusTeapot,
		},
	}
	h := guarded()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, strings.NewReader("{}"))
			r.Host = tc.host
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d", w.Code, tc.want)
			}
			if w.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
				t.Fatal("anti-framing headers missing")
			}
		})
	}
}

func TestRunManagerExclusive(t *testing.T) {
	m := newRunManager(nil, nil)
	if err := m.tryExclusive(); err != nil {
		t.Fatal(err)
	}
	if !m.busy() {
		t.Fatal("exclusive must make the manager busy")
	}
	if _, err := m.begin(runPlan{action: actionStart}, func([]string) string { return "" }); !errors.Is(err, errExclusiveBusy) {
		t.Fatalf("run during exclusive: got %v", err)
	}
	if err := m.tryExclusive(); !errors.Is(err, errExclusiveBusy) {
		t.Fatalf("second exclusive: got %v", err)
	}
	m.endExclusive()

	run, err := m.begin(runPlan{action: actionStart}, func([]string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if err := m.tryExclusive(); !errors.Is(err, ErrRunInProgress) {
		t.Fatalf("exclusive during run: got %v", err)
	}
	m.finish(run.ID)
	if err := m.tryExclusive(); err != nil {
		t.Fatalf("after run: %v", err)
	}
}

func TestLatestSummaryDropsOutput(t *testing.T) {
	m := newRunManager(nil, nil)
	run, _ := m.begin(runPlan{action: actionStart, steps: []stepPlan{{file: ComposeFile{Path: "/x"}}}}, func([]string) string { return "" })
	m.updateStep(run.ID, 0, func(s *Step) { s.Output, s.Error = strings.Repeat("x", 1000), "boom" })
	sum := m.latestSummary()
	if sum.Steps[0].Output != "" || sum.Steps[0].Error != "" {
		t.Fatal("summary must not carry step output")
	}
	if full := m.get(run.ID); full.Steps[0].Output == "" {
		t.Fatal("full run lost its output")
	}
}

func TestPumpLinesBoundsLongLines(t *testing.T) {
	huge := strings.Repeat("a", 5*maxLogLine) + "\nnext\n"
	out := make(chan logLine, 10)
	var wg sync.WaitGroup
	wg.Add(1)
	pumpLines(context.Background(), strings.NewReader(huge), "out", out, &wg)
	close(out)
	var got []logLine
	for l := range out {
		got = append(got, l)
	}
	if len(got) != 2 || got[1].Msg != "next" {
		t.Fatalf("got %d lines: %+v", len(got), got)
	}
	if len(got[0].Msg) > maxLogLine+8 || !strings.HasSuffix(got[0].Msg, "…") {
		t.Fatalf("long line not truncated: %d bytes", len(got[0].Msg))
	}
}

func TestPumpLinesStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan logLine) // nobody reads
	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		pumpLines(ctx, strings.NewReader("a\nb\n"), "out", out, &wg)
		close(done)
	}()
	cancel()
	<-done
}

func TestMatchComposeFileUsesSameNormalization(t *testing.T) {
	dir := t.TempDir()
	file := dir + "/compose.yaml"
	if err := os.WriteFile(file, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	known := map[string]string{paths.Normalize(file): file}
	got, ok := matchComposeFile(map[string]string{labelConfigFiles: file}, known)
	if !ok || got != file {
		t.Fatalf("a container from a discovered compose file must be managed: %q %v", got, ok)
	}
}
