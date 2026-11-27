package metrics

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sudoku-golang/internal/engine"
	"sync/atomic"
	"testing"
	"time"
)

func fakeEngine(t *testing.T) (*engine.Engine, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	var cpu atomic.Uint64
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"Id": "abc", "Names": []string{"/db"}, "Labels": map[string]string{"com.docker.compose.project": "p", "com.docker.compose.service": "db"},
		}})
	})
	mux.HandleFunc("/containers/abc/stats", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		c := cpu.Add(50_000_000)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"read": time.Now().Format(time.RFC3339Nano),
			"cpu_stats": map[string]any{
				"cpu_usage": map[string]any{"total_usage": c}, "system_cpu_usage": c * 10, "online_cpus": 2,
			},
			"memory_stats": map[string]any{"usage": 1 << 20, "limit": 1 << 30},
		})
	})

	dir, err := os.MkdirTemp("/tmp", "sdk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return engine.New(engine.UnixEndpoint(sock)), &calls
}

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCollectorIdleWithoutSubscribers(t *testing.T) {
	engine, calls := fakeEngine(t)
	c := NewCollector(quietLog(), engine)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go c.Run(ctx)

	time.Sleep(300 * time.Millisecond)
	if n := calls.Load(); n != 0 {
		t.Fatalf("idle collector hit docker %d times", n)
	}

	ch, _ := c.Subscribe()
	select {
	case tick := <-ch:
		s := tick.Containers["abc"].Sample
		if s.CPU <= 0 || s.Mem != 1<<20 {
			t.Fatalf("unexpected sample %+v", s)
		}
	case <-time.After(FastInterval + 3*time.Second):
		t.Fatal("no tick after subscribe")
	}

	c.Unsubscribe(ch)
	time.Sleep(FastInterval + 500*time.Millisecond)
	before := calls.Load()
	time.Sleep(FastInterval + 500*time.Millisecond)
	if after := calls.Load(); after != before {
		t.Fatalf("collector kept polling after last unsubscribe: %d -> %d", before, after)
	}
}

func TestCollectorBackground(t *testing.T) {
	engine, calls := fakeEngine(t)
	c := NewCollector(quietLog(), engine)
	c.SetBackground(true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go c.Run(ctx)

	time.Sleep(300 * time.Millisecond)
	if calls.Load() == 0 {
		t.Fatal("background collector did not poll")
	}
	if !c.Active() {
		t.Fatal("background collector should be active")
	}
}

func TestCollectorIntervals(t *testing.T) {
	engine, _ := fakeEngine(t)
	c := NewCollector(quietLog(), engine)
	c.SetIntervals(time.Second, 0)
	if live, bg := c.Intervals(); live != time.Second || bg != SlowInterval {
		t.Fatalf("intervals %v %v", live, bg)
	}
	c.SetIntervals(time.Millisecond, 10*time.Hour)
	if live, bg := c.Intervals(); live != MinLiveInterval || bg != MaxBackgroundInterval {
		t.Fatalf("clamping failed: %v %v", live, bg)
	}

	c.SetIntervals(time.Second, 0)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go c.Run(ctx)
	ch, _ := c.Subscribe()
	defer c.Unsubscribe(ch)

	var stamps []int64
	deadline := time.After(6 * time.Second)
	for len(stamps) < 3 {
		select {
		case tick := <-ch:
			stamps = append(stamps, tick.T)
		case <-deadline:
			t.Fatalf("got only %d ticks", len(stamps))
		}
	}
	if gap := stamps[2] - stamps[1]; gap < 800 || gap > 1600 {
		t.Fatalf("tick gap %dms, want ~1000ms", gap)
	}
}
