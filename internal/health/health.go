// Package health waits until the containers of a compose file pass their healthchecks — used to start
// projects only after the infrastructure (databases, brokers) is actually ready.
package health

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/paths"
	"time"
)

const (
	labelConfigFiles = "com.docker.compose.project.config_files"
	labelService     = "com.docker.compose.service"
	DefaultPoll      = time.Second
)

var (
	ErrTimeout   = errors.New("healthcheck timeout")
	ErrUnhealthy = errors.New("containers are unhealthy or exited")
)

// Lister returns the current compose containers (inspected).
type Lister func(ctx context.Context) ([]docker.Container, error)

// BelongsTo reports whether c was created from composeFile.
func BelongsTo(c docker.Container, composeFile string) bool {
	want := paths.Normalize(composeFile)
	for _, part := range strings.Split(c.Config.Labels[labelConfigFiles], ",") {
		if part = strings.TrimSpace(part); part != "" && paths.Normalize(part) == want {
			return true
		}
	}
	return false
}

type Result struct {
	Healthy   []string
	Failed    []string
	Pending   []string
	NoCheck   int
	Elapsed   time.Duration
	TimedOut  bool
	Checked   int
	Unhealthy bool
}

func (r Result) Summary() string {
	switch {
	case r.Checked == 0:
		return "нет контейнеров с healthcheck — ждать нечего"
	case len(r.Failed) > 0:
		return fmt.Sprintf("не прошли healthcheck: %s", strings.Join(r.Failed, ", "))
	case r.TimedOut:
		return fmt.Sprintf("таймаут %s, всё ещё ждём: %s", r.Elapsed.Round(time.Second), strings.Join(r.Pending, ", "))
	}
	return fmt.Sprintf("готово за %s: %s", r.Elapsed.Round(100*time.Millisecond), strings.Join(r.Healthy, ", "))
}

func name(c docker.Container) string {
	if s := c.Config.Labels[labelService]; s != "" {
		return s
	}
	return strings.TrimPrefix(c.Name, "/")
}

// classify splits the containers of a file by health state. Containers without a healthcheck do not
// block; a stopped/dead/unhealthy container fails the wait.
func classify(list []docker.Container, file string, services map[string]bool) Result {
	var r Result
	for _, c := range list {
		if !BelongsTo(c, file) {
			continue
		}
		if len(services) > 0 && !services[c.Config.Labels[labelService]] {
			continue
		}
		n := name(c)
		switch {
		case c.State.Health == nil && c.State.Running:
			r.NoCheck++
			continue
		case c.State.Health == nil:
			continue // stopped and without healthcheck: not ours to wait for (disabled service)
		}
		r.Checked++
		switch {
		case !c.State.Running || c.State.Restarting || c.State.Dead:
			r.Failed = append(r.Failed, n+" (остановлен)")
		case c.State.Health.Status == "healthy":
			r.Healthy = append(r.Healthy, n)
		case c.State.Health.Status == "unhealthy":
			r.Unhealthy = true
			r.Pending = append(r.Pending, n)
		default:
			r.Pending = append(r.Pending, n)
		}
	}
	sort.Strings(r.Healthy)
	sort.Strings(r.Failed)
	sort.Strings(r.Pending)
	return r
}

// Wait polls until every container of file that has a healthcheck is healthy, one of them stops, or
// timeout passes. "unhealthy" is not final by itself (a database may be slow on first boot), so it
// only fails at the deadline. progress is called with the containers still being waited for.
func Wait(ctx context.Context, list Lister, file string, services []string, timeout, poll time.Duration, progress func(Result)) (Result, error) {
	if poll <= 0 {
		poll = DefaultPoll
	}
	only := map[string]bool{}
	for _, s := range services {
		only[s] = true
	}
	start := time.Now()
	deadline := start.Add(timeout)
	var last Result
	for {
		containers, err := list(ctx)
		if err != nil {
			return last, fmt.Errorf("list containers: %w", err)
		}
		last = classify(containers, file, only)
		last.Elapsed = time.Since(start)
		if progress != nil {
			progress(last)
		}
		switch {
		case len(last.Failed) > 0:
			return last, ErrUnhealthy
		case len(last.Pending) == 0:
			return last, nil
		case time.Now().After(deadline):
			last.TimedOut = true
			return last, ErrTimeout
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err() //nolint:wrapcheck // cancellation
		case <-time.After(poll):
		}
	}
}
