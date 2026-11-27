// Package containers lists and inspects compose containers for the CLI and the admin panel.
package containers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/engine"
	"sync"
	"sync/atomic"
)

const inspectWorkers = 8

// ErrNotFound is returned for an unknown container ID or name.
var ErrNotFound = errors.New("container not found")

// Source lists and inspects compose containers. It talks to the Docker Engine API over the
// unix socket (no process spawn per poll) and caches inspect results per container ID until the
// container's State/Status line changes; when the socket is unreachable it falls back to the docker
// CLI so a remote or unusual setup keeps working.
//
// /api/state is polled every 2–3 s; before this, every poll forked `docker ps` + `docker inspect`
// (two Go binaries starting and parsing their config each time).
type Source struct {
	log    *slog.Logger
	engine *engine.Engine
	cli    *docker.Runner

	mu    sync.Mutex
	cache map[string]cachedInspect

	warned atomic.Bool
}

type cachedInspect struct {
	key string
	c   docker.Container
}

func NewSource(log *slog.Logger, engine *engine.Engine, cli *docker.Runner) *Source {
	return &Source{log: log, engine: engine, cli: cli, cache: map[string]cachedInspect{}}
}

func (s *Source) fallback(ctx context.Context, err error) bool {
	if s.engine == nil {
		return true
	}
	if ctx.Err() != nil {
		return false // the caller gave up; the socket is fine
	}
	if engine.IsEngineError(err) {
		return false
	}
	if !s.warned.Swap(true) {
		s.log.Warn("docker socket unavailable, falling back to docker CLI", slog.Any("error", err))
	}
	return true
}

// List returns every compose container (running or not), inspected.
func (s *Source) List(ctx context.Context) ([]docker.Container, error) {
	if s.engine == nil {
		return s.cli.ListComposeContainers(ctx) //nolint:wrapcheck // CommandError is descriptive
	}
	summaries, err := s.engine.ComposeContainers(ctx)
	if err != nil {
		if s.fallback(ctx, err) {
			return s.cli.ListComposeContainers(ctx) //nolint:wrapcheck // CommandError is descriptive
		}
		return nil, fmt.Errorf("list containers: %w", err)
	}
	s.warned.Store(false)

	out := make([]docker.Container, len(summaries))
	found := make([]bool, len(summaries))
	var missing []int

	s.mu.Lock()
	live := make(map[string]bool, len(summaries))
	for i, sm := range summaries {
		live[sm.ID] = true
		if e, ok := s.cache[sm.ID]; ok && e.key == sm.State+"|"+sm.Status {
			out[i], found[i] = e.c, true
			continue
		}
		missing = append(missing, i)
	}
	for id := range s.cache {
		if !live[id] {
			delete(s.cache, id)
		}
	}
	s.mu.Unlock()

	if len(missing) > 0 {
		s.inspectMissing(ctx, summaries, missing, out, found)
	}

	result := out[:0]
	for i := range out {
		if found[i] {
			result = append(result, out[i])
		}
	}
	return result, nil
}

func (s *Source) inspectMissing(
	ctx context.Context, summaries []engine.ContainerSummary, missing []int, out []docker.Container, found []bool,
) {
	sem := make(chan struct{}, inspectWorkers)
	var wg sync.WaitGroup
	for _, i := range missing {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			c, err := s.engine.InspectContainer(ctx, summaries[i].ID)
			if err != nil {
				// Removed between list and inspect, or a transient error: skip it this round.
				return
			}
			out[i], found[i] = c, true
			s.mu.Lock()
			s.cache[summaries[i].ID] = cachedInspect{key: summaries[i].State + "|" + summaries[i].Status, c: c}
			s.mu.Unlock()
		}(i)
	}
	wg.Wait()
}

// Inspect returns one container by ID or name; it always asks Docker (no cache) since callers act on
// the result.
func (s *Source) Inspect(ctx context.Context, id string) (docker.Container, error) {
	if s.engine != nil {
		c, err := s.engine.InspectContainer(ctx, id)
		if err == nil {
			return c, nil
		}
		if engine.IsNotFound(err) {
			return docker.Container{}, ErrNotFound
		}
		if !s.fallback(ctx, err) {
			return docker.Container{}, fmt.Errorf("inspect %s: %w", id, err)
		}
	}
	found, err := s.cli.Inspect(ctx, id)
	if err != nil || len(found) == 0 {
		return docker.Container{}, ErrNotFound
	}
	return found[0], nil
}

// Forget drops a cached entry after an action on the container, so the next list re-inspects it even
// if Docker reports the same status line within the same second.
func (s *Source) Forget(id string) {
	s.mu.Lock()
	delete(s.cache, id)
	s.mu.Unlock()
}
