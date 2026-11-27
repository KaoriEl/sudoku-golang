package health

import (
	"context"
	"errors"
	"sudoku-golang/internal/docker"
	"testing"
	"time"
)

func ctr(file, svc string, running bool, health string) docker.Container {
	c := docker.Container{Name: "/" + svc}
	c.Config.Labels = map[string]string{labelConfigFiles: file, labelService: svc}
	c.State.Running = running
	if health != "" {
		c.State.Health = &docker.Health{Status: health}
	}
	return c
}

func TestWaitBecomesHealthy(t *testing.T) {
	calls := 0
	list := func(context.Context) ([]docker.Container, error) {
		calls++
		state := "starting"
		if calls >= 3 {
			state = "healthy"
		}
		return []docker.Container{
			ctr("/r/compose.yaml", "db", true, state),
			ctr("/r/compose.yaml", "web", true, ""),
			ctr("/r/other.yaml", "x", true, "starting"),
		}, nil
	}
	res, err := Wait(context.Background(), list, "/r/compose.yaml", nil, time.Minute, time.Millisecond, nil)
	if err != nil || len(res.Healthy) != 1 || res.NoCheck != 1 || calls != 3 {
		t.Fatalf("res=%+v err=%v calls=%d", res, err, calls)
	}
}

func TestWaitFailsOnStopped(t *testing.T) {
	list := func(context.Context) ([]docker.Container, error) {
		return []docker.Container{ctr("/r/compose.yaml", "db", false, "unhealthy")}, nil
	}
	if _, err := Wait(context.Background(), list, "/r/compose.yaml", nil, time.Minute, time.Millisecond, nil); !errors.Is(err, ErrUnhealthy) {
		t.Fatalf("got %v", err)
	}
}

func TestWaitTimeout(t *testing.T) {
	list := func(context.Context) ([]docker.Container, error) {
		return []docker.Container{ctr("/r/compose.yaml", "db", true, "unhealthy")}, nil
	}
	res, err := Wait(context.Background(), list, "/r/compose.yaml", nil, 5*time.Millisecond, time.Millisecond, nil)
	if !errors.Is(err, ErrTimeout) || !res.TimedOut {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestWaitOnlySelectedServices(t *testing.T) {
	list := func(context.Context) ([]docker.Container, error) {
		return []docker.Container{ctr("/r/compose.yaml", "db", true, "healthy"), ctr("/r/compose.yaml", "slow", true, "starting")}, nil
	}
	if _, err := Wait(context.Background(), list, "/r/compose.yaml", []string{"db"}, time.Minute, time.Millisecond, nil); err != nil {
		t.Fatal(err)
	}
}
