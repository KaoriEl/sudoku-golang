package containers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sudoku-golang/internal/engine"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeDocker serves the two Engine endpoints containerSource uses over a unix socket.
type fakeDocker struct {
	mu       sync.Mutex
	status   map[string]string
	inspects atomic.Int32
}

func (f *fakeDocker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/containers/json" {
		if r.URL.Query().Get("all") != "true" {
			http.Error(w, "all expected", http.StatusBadRequest)
			return
		}
		list := make([]map[string]string, 0, len(f.status))
		for id, st := range f.status {
			list = append(list, map[string]string{"Id": id, "State": "running", "Status": st})
		}
		_ = json.NewEncoder(w).Encode(list)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/containers/"), "/json")
	if _, ok := f.status[id]; !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"No such container"}`)
		return
	}
	f.inspects.Add(1)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"Id":     id,
		"Name":   "/" + id,
		"State":  map[string]any{"Status": "running", "Running": true},
		"Config": map[string]any{"Labels": map[string]string{"com.docker.compose.project": "p"}},
	})
}

func startFakeDocker(t *testing.T, f *fakeDocker) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "skd") // short path: unix socket paths are limited to ~104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: f} //nolint:gosec // test server
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return sock
}

func TestContainerSourceCachesInspect(t *testing.T) {
	f := &fakeDocker{status: map[string]string{"a": "Up 1 minute", "b": "Up 2 minutes"}}
	src := NewSource(slog.New(slog.DiscardHandler), engine.New(engine.UnixEndpoint(startFakeDocker(t, f))), nil)
	ctx := context.Background()

	list, err := src.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("first list: %v, %d containers", err, len(list))
	}
	if n := f.inspects.Load(); n != 2 {
		t.Fatalf("first list inspected %d containers, want 2", n)
	}

	if _, err := src.List(ctx); err != nil {
		t.Fatal(err)
	}
	if n := f.inspects.Load(); n != 2 {
		t.Fatalf("unchanged containers were re-inspected: %d calls", n)
	}

	f.mu.Lock()
	f.status["a"] = "Up 1 minute (unhealthy)"
	delete(f.status, "b")
	f.mu.Unlock()
	list, err = src.List(ctx)
	if err != nil || len(list) != 1 || list[0].ID != "a" {
		t.Fatalf("after change: %v, %+v", err, list)
	}
	if n := f.inspects.Load(); n != 3 {
		t.Fatalf("status change must re-inspect exactly one container, got %d calls", n)
	}
	src.mu.Lock()
	_, stale := src.cache["b"]
	src.mu.Unlock()
	if stale {
		t.Fatal("removed container must be dropped from the cache")
	}

	if _, err := src.Inspect(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id: got %v, want ErrNotFound", err)
	}
}
