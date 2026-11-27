package metrics

import (
	"context"
	"log/slog"
	"strings"
	"sudoku-golang/internal/engine"
	"sync"
	"time"
)

const (
	FastInterval = 2 * time.Second
	SlowInterval = 10 * time.Second

	MinLiveInterval       = time.Second
	MaxLiveInterval       = time.Minute
	MinBackgroundInterval = 2 * time.Second
	MaxBackgroundInterval = time.Hour
	Window                = 30 * time.Minute
	statsWorkers          = 8
	tickTimeout           = 8 * time.Second
	subBuffer             = 4
)

type Sample struct {
	T        int64   `json:"t"`
	CPU      float64 `json:"cpu"`
	Mem      uint64  `json:"mem"`
	MemLimit uint64  `json:"memLimit"`
	NetRx    float64 `json:"netRx"`
	NetTx    float64 `json:"netTx"`
	BlkR     float64 `json:"blkR"`
	BlkW     float64 `json:"blkW"`
	PIDs     uint64  `json:"pids"`
}

type Meta struct {
	Name    string `json:"name"`
	Project string `json:"project"`
	Service string `json:"service"`
}

type Series struct {
	Meta
	Samples []Sample `json:"samples"`
}

type History struct {
	Interval   int64              `json:"interval"`
	Background int64              `json:"background"`
	GapMs      int64              `json:"gapMs"`
	Window     int64              `json:"window"`
	Totals     []Sample           `json:"totals"`
	Containers map[string]*Series `json:"containers"`
}

type TickContainer struct {
	Meta
	Sample Sample `json:"sample"`
}

type Tick struct {
	T          int64                    `json:"t"`
	Totals     Sample                   `json:"totals"`
	Containers map[string]TickContainer `json:"containers"`
}

type counters struct {
	t       time.Time
	cpu     uint64
	system  uint64
	rx, tx  uint64
	rd, wr  uint64
	hasPrev bool
}

type Collector struct {
	log    *slog.Logger
	engine *engine.Engine

	mu         sync.RWMutex
	series     map[string]*Series
	totals     []Sample
	prev       map[string]counters
	subs       map[chan Tick]struct{}
	wake       chan struct{}
	background bool
	live       time.Duration
	bg         time.Duration
	lastOK     time.Time
	lastErr    string
}

func NewCollector(log *slog.Logger, engine *engine.Engine) *Collector {
	return &Collector{
		log:    log,
		engine: engine,
		series: map[string]*Series{},
		prev:   map[string]counters{},
		subs:   map[chan Tick]struct{}{},
		wake:   make(chan struct{}, 1),
		live:   FastInterval,
		bg:     SlowInterval,
	}
}

func clampDuration(d, lo, hi time.Duration) time.Duration {
	return max(lo, min(hi, d))
}

func (c *Collector) SetIntervals(live, background time.Duration) {
	c.mu.Lock()
	if live > 0 {
		c.live = clampDuration(live, MinLiveInterval, MaxLiveInterval)
	}
	if background > 0 {
		c.bg = clampDuration(background, MinBackgroundInterval, MaxBackgroundInterval)
	}
	c.mu.Unlock()
	c.poke()
}

func (c *Collector) Intervals() (time.Duration, time.Duration) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.live, c.bg
}

func (c *Collector) gapLocked() time.Duration {
	slowest := c.live
	if c.background {
		slowest = max(slowest, c.bg)
	}
	return slowest*5/2 + time.Second
}

func (c *Collector) staleAfter() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return 3*max(c.live, c.bg) + time.Second
}

func (c *Collector) Engine() *engine.Engine {
	return c.engine
}

func (c *Collector) Subscribe() (chan Tick, History) {
	ch := make(chan Tick, subBuffer)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	h := c.historyLocked()
	c.mu.Unlock()
	c.poke()
	return ch, h
}

func (c *Collector) Unsubscribe(ch chan Tick) {
	c.mu.Lock()
	delete(c.subs, ch)
	c.mu.Unlock()
}

func (c *Collector) Status() (time.Time, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastOK, c.lastErr
}

func (c *Collector) historyLocked() History {
	h := History{
		Interval:   c.live.Milliseconds(),
		Background: c.bg.Milliseconds(),
		GapMs:      c.gapLocked().Milliseconds(),
		Window:     Window.Milliseconds(),
		Totals:     append([]Sample(nil), c.totals...),
		Containers: make(map[string]*Series, len(c.series)),
	}
	for id, s := range c.series {
		h.Containers[id] = &Series{Meta: s.Meta, Samples: append([]Sample(nil), s.Samples...)}
	}
	return h
}

func (c *Collector) SetBackground(on bool) {
	c.mu.Lock()
	c.background = on
	c.mu.Unlock()
	c.poke()
}

func (c *Collector) Background() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.background
}

func (c *Collector) Active() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.background || len(c.subs) > 0
}

func (c *Collector) poke() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Collector) interval() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.subs) > 0 {
		return c.live
	}
	return c.bg
}

func (c *Collector) Run(ctx context.Context) {
	var last time.Time
	for {
		var wait <-chan time.Time
		var timer *time.Timer
		if c.Active() {
			iv := c.interval()
			if last.IsZero() || time.Since(last) >= iv/2 {
				c.collect(ctx)
				last = time.Now()
			}
			timer = time.NewTimer(max(0, iv-time.Since(last)))
			wait = timer.C
		}
		select {
		case <-ctx.Done():
		case <-c.wake:
		case <-wait:
		}
		if timer != nil {
			timer.Stop()
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func metaOf(l engine.ListedContainer) Meta {
	name := ""
	if len(l.Names) > 0 {
		name = strings.TrimPrefix(l.Names[0], "/")
	}
	return Meta{Name: name, Project: l.Labels["com.docker.compose.project"], Service: l.Labels["com.docker.compose.service"]}
}

func memUsed(s engine.Stats) uint64 {
	used := s.MemoryStats.Usage
	st := s.MemoryStats.Stats
	for _, v := range []*uint64{st.InactiveFile, st.TotalInactiveFile, st.Cache} {
		if v != nil && *v < used {
			return used - *v
		}
	}
	return used
}

func readCounters(s engine.Stats) counters {
	k := counters{t: s.Read, cpu: s.CPUStats.CPUUsage.TotalUsage, system: s.CPUStats.SystemUsage, hasPrev: true}
	if k.t.IsZero() {
		k.t = time.Now()
	}
	for _, n := range s.Networks {
		k.rx += n.RxBytes
		k.tx += n.TxBytes
	}
	for _, b := range s.BlkioStats.IOServiceBytes {
		switch strings.ToLower(b.Op) {
		case "read":
			k.rd += b.Value
		case "write":
			k.wr += b.Value
		}
	}
	return k
}

func rate(cur, prev uint64, dt float64) float64 {
	if cur < prev || dt <= 0 {
		return 0
	}
	return float64(cur-prev) / dt
}

func toSample(now time.Time, s engine.Stats, cur, prev counters) Sample {
	sm := Sample{T: now.UnixMilli(), Mem: memUsed(s), MemLimit: s.MemoryStats.Limit, PIDs: s.PidsStats.Current}
	if !prev.hasPrev {
		return sm
	}
	if cur.cpu >= prev.cpu && cur.system > prev.system {
		cpus := s.CPUStats.OnlineCPUs
		if cpus == 0 {
			cpus = 1
		}
		sm.CPU = float64(cur.cpu-prev.cpu) / float64(cur.system-prev.system) * float64(cpus) * 100
	}
	dt := cur.t.Sub(prev.t).Seconds()
	sm.NetRx = rate(cur.rx, prev.rx, dt)
	sm.NetTx = rate(cur.tx, prev.tx, dt)
	sm.BlkR = rate(cur.rd, prev.rd, dt)
	sm.BlkW = rate(cur.wr, prev.wr, dt)
	return sm
}

type fetched struct {
	id    string
	meta  Meta
	stats engine.Stats
	err   error
}

func (c *Collector) fetchAll(ctx context.Context, list []engine.ListedContainer) []fetched {
	results := make([]fetched, len(list))
	sem := make(chan struct{}, statsWorkers)
	var wg sync.WaitGroup
	for i, l := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, l engine.ListedContainer) {
			defer wg.Done()
			defer func() { <-sem }()
			st, err := c.engine.ContainerStats(ctx, l.ID)
			results[i] = fetched{id: l.ID, meta: metaOf(l), stats: st, err: err}
		}(i, l)
	}
	wg.Wait()
	return results
}

func (c *Collector) collect(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, tickTimeout)
	defer cancel()

	list, err := c.engine.RunningCompose(ctx)
	now := time.Now()
	if err != nil {
		c.mu.Lock()
		c.lastErr = err.Error()
		c.mu.Unlock()
		c.log.Debug("metrics list failed", slog.Any("error", err))
		return
	}
	results := c.fetchAll(ctx, list)
	stale := c.staleAfter()

	c.mu.Lock()
	tick := Tick{T: now.UnixMilli(), Totals: Sample{T: now.UnixMilli()}, Containers: map[string]TickContainer{}}
	seen := map[string]bool{}
	sampled, priming := 0, 0
	for _, r := range results {
		if r.err != nil {
			continue
		}
		seen[r.id] = true
		cur := readCounters(r.stats)
		prev, ok := c.prev[r.id]
		c.prev[r.id] = cur
		if !ok || cur.t.Sub(prev.t) > stale {
			priming++
			continue
		}
		sampled++
		sm := toSample(now, r.stats, cur, prev)

		s, ok := c.series[r.id]
		if !ok {
			s = &Series{Meta: r.meta}
			c.series[r.id] = s
		}
		s.Meta = r.meta
		s.Samples = append(s.Samples, sm)
		tick.Containers[r.id] = TickContainer{Meta: r.meta, Sample: sm}

		tick.Totals.CPU += sm.CPU
		tick.Totals.Mem += sm.Mem
		tick.Totals.NetRx += sm.NetRx
		tick.Totals.NetTx += sm.NetTx
		tick.Totals.BlkR += sm.BlkR
		tick.Totals.BlkW += sm.BlkW
		tick.Totals.PIDs += sm.PIDs
		if sm.MemLimit > tick.Totals.MemLimit {
			tick.Totals.MemLimit = sm.MemLimit
		}
	}
	for id := range c.prev {
		if !seen[id] {
			delete(c.prev, id)
		}
	}
	c.lastOK = now
	c.lastErr = ""
	if sampled == 0 && priming > 0 {
		c.mu.Unlock()
		return
	}
	c.totals = append(c.totals, tick.Totals)
	c.pruneLocked(now)
	subs := make([]chan Tick, 0, len(c.subs))
	for ch := range c.subs {
		subs = append(subs, ch)
	}
	c.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- tick:
		default:
		}
	}
}

func trim(samples []Sample, cutoff int64) []Sample {
	i := 0
	for i < len(samples) && samples[i].T < cutoff {
		i++
	}
	if i == 0 {
		return samples
	}
	if i == len(samples) {
		return nil
	}
	// Reslicing is O(1); the next append that outgrows the capacity reallocates with only the live
	// samples, so memory stays bounded at ~2× the window without copying 900 samples per tick.
	return samples[i:]
}

func (c *Collector) pruneLocked(now time.Time) {
	cutoff := now.Add(-Window).UnixMilli()
	c.totals = trim(c.totals, cutoff)
	for id, s := range c.series {
		s.Samples = trim(s.Samples, cutoff)
		if len(s.Samples) == 0 {
			delete(c.series, id)
		}
	}
}
