package admin

import (
	"context"
	"fmt"
	"log/slog"
	"sudoku-golang/internal/cleanup"
	"sudoku-golang/internal/prefs"
	"time"
)

const (
	scheduleTick    = time.Minute
	scheduleStartup = 2 * time.Minute
	cacheOff        = "off"
)

// scheduler runs the periodic jobs from admin.yaml: disk cleanup and image update checks. It checks
// once a minute whether a job is due (lastRun + everyHours), so a laptop that slept through the
// planned time runs the job soon after waking up. Jobs never overlap runs: cleanup takes the
// exclusive slot and is postponed to the next tick while a run is active.
type scheduler struct {
	s    *Service
	wake chan struct{}
}

func newScheduler(s *Service) *scheduler {
	return &scheduler{s: s, wake: make(chan struct{}, 1)}
}

func (sc *scheduler) poke() {
	select {
	case sc.wake <- struct{}{}:
	default:
	}
}

func due(last time.Time, everyHours int, now time.Time) bool {
	return last.IsZero() || now.Sub(last) >= time.Duration(everyHours)*time.Hour
}

func (sc *scheduler) run(ctx context.Context) {
	// Give the machine (and Docker) time to settle after login before the first job.
	select {
	case <-ctx.Done():
		return
	case <-time.After(scheduleStartup):
	}
	t := time.NewTicker(scheduleTick)
	defer t.Stop()
	for {
		sc.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-sc.wake:
		}
	}
}

func (sc *scheduler) tick(ctx context.Context) {
	p, err := sc.s.prefs.Get()
	if err != nil {
		return
	}
	p = p.Normalized()
	now := time.Now()
	if c := p.Schedule.Cleanup; c.Enabled && due(c.LastRun, c.EveryHours, now) {
		sc.cleanup(ctx, c)
	}
	if u := p.Schedule.Updates; u.Enabled && due(u.LastRun, u.EveryHours, now) {
		sc.updates(ctx)
	}
}

// scheduledRequest picks what the scheduled cleanup removes: only unprotected stopped containers,
// only dangling (untagged, unused) images and build cache older than the configured age.
func scheduledRequest(p cleanup.Preview, c prefs.CleanupSchedule) cleanup.Request {
	var req cleanup.Request
	if c.StoppedContainers {
		for _, ct := range p.Containers {
			if !ct.Protected {
				req.Containers = append(req.Containers, ct.ID)
			}
		}
	}
	if c.DanglingImages {
		for _, im := range p.Images {
			if im.Dangling {
				req.Images = append(req.Images, im.ID)
			}
		}
	}
	if c.BuildCache != "" && c.BuildCache != cacheOff {
		req.BuildCache = c.BuildCache
	}
	return req
}

func (sc *scheduler) cleanup(ctx context.Context, c prefs.CleanupSchedule) {
	s := sc.s
	if err := s.runs.tryExclusive(); err != nil {
		return // a run or another exclusive job is active: try again next tick
	}
	defer s.runs.endExclusive()

	ctx, cancel := context.WithTimeout(ctx, cleanupRunTimeout)
	defer cancel()
	result := ""
	preview, err := cleanup.Scan(ctx, s.engine, time.Now())
	if err == nil {
		req := scheduledRequest(preview, c)
		if req.Validate() != nil {
			result = "нечего удалять"
		} else {
			res, execErr := cleanup.Execute(ctx, s.engine, req, time.Now())
			err = execErr
			ok := 0
			for _, it := range res.Items {
				if it.OK {
					ok++
				}
			}
			result = fmt.Sprintf("удалено %d из %d, освобождено %s", ok, len(res.Items), humanBytes(res.Freed))
		}
	}
	if err != nil {
		result = "ошибка: " + err.Error()
	}
	s.log.Info("scheduled cleanup", slog.String("result", result))
	s.df.invalidate()
	_, _ = s.prefs.Update(func(p *prefs.Prefs) error {
		p.Schedule.Cleanup.LastRun = time.Now().UTC()
		p.Schedule.Cleanup.LastResult = result
		return nil
	})
}

func (sc *scheduler) updates(ctx context.Context) {
	s := sc.s
	_, err := s.CheckUpdates(ctx)
	if err != nil {
		s.log.Warn("scheduled update check failed", slog.Any("error", err))
	}
	_, _ = s.prefs.Update(func(p *prefs.Prefs) error {
		p.Schedule.Updates.LastRun = time.Now().UTC()
		return nil
	})
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d Б", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cБ", float64(n)/float64(div), []rune("КМГТП")[exp])
}
