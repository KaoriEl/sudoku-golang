// Package updates checks whether newer versions of the images used by compose services exist in their
// registries. It asks the registry for the manifest digest through the engine (/distribution), so
// nothing is pulled; locally built images are skipped.
package updates

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sudoku-golang/internal/engine"
	"sync"
	"time"
)

const (
	checkWorkers = 4
	checkTimeout = 30 * time.Second
)

var ErrBusy = errors.New("проверка обновлений уже идёт")

type Status struct {
	Image      string    `json:"image"`
	Local      string    `json:"local"`
	Remote     string    `json:"remote"`
	Available  bool      `json:"available"`
	LocalBuild bool      `json:"localBuild"`
	Error      string    `json:"error"`
	CheckedAt  time.Time `json:"checkedAt"`
}

type Engine interface {
	ImageInspect(ctx context.Context, ref string) (engine.Image, error)
	DistributionInspect(ctx context.Context, ref, registryAuth string) (string, error)
}

type Checker struct {
	eng     Engine
	mu      sync.Mutex
	results map[string]Status
	running bool
	last    time.Time
}

func NewChecker(eng Engine) *Checker {
	return &Checker{eng: eng, results: map[string]Status{}}
}

// Results is the last known status per image reference.
func (c *Checker) Results() map[string]Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]Status, len(c.results))
	for k, v := range c.results {
		out[k] = v
	}
	return out
}

func (c *Checker) State() (bool, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running, c.last
}

// Forget drops the result for image (after a pull the old answer is stale).
func (c *Checker) Forget(image string) {
	c.mu.Lock()
	delete(c.results, image)
	c.mu.Unlock()
}

func repoOf(ref string) string {
	ref, _, _ = strings.Cut(ref, "@")
	slash := strings.LastIndex(ref, "/")
	if colon := strings.LastIndex(ref, ":"); colon > slash {
		ref = ref[:colon]
	}
	return ref
}

func (c *Checker) checkOne(ctx context.Context, ref string) Status {
	st := Status{Image: ref, CheckedAt: time.Now().UTC()}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	im, err := c.eng.ImageInspect(ctx, ref)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	if len(im.RepoDigests) == 0 {
		st.LocalBuild = true
		return st
	}
	repo := repoOf(ref)
	for _, d := range im.RepoDigests {
		if r, digest, ok := strings.Cut(d, "@"); ok && (st.Local == "" || strings.HasSuffix(r, repo)) {
			st.Local = digest
		}
	}
	remote, err := c.eng.DistributionInspect(ctx, ref, registryAuth(ctx, ref))
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Remote = remote
	st.Available = true
	for _, d := range im.RepoDigests {
		if strings.HasSuffix(d, "@"+remote) {
			st.Available = false
		}
	}
	return st
}

// Check queries every image (deduplicated) and stores the results. One check at a time.
func (c *Checker) Check(ctx context.Context, images []string) ([]Status, error) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil, ErrBusy
	}
	c.running = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.running = false
		c.last = time.Now().UTC()
		c.mu.Unlock()
	}()

	uniq := map[string]bool{}
	var refs []string
	for _, im := range images {
		if im != "" && !uniq[im] && !strings.HasPrefix(im, "sha256:") {
			uniq[im] = true
			refs = append(refs, im)
		}
	}
	sort.Strings(refs)
	out := make([]Status, len(refs))
	sem := make(chan struct{}, checkWorkers)
	var wg sync.WaitGroup
	for i, ref := range refs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, ref string) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = c.checkOne(ctx, ref)
		}(i, ref)
	}
	wg.Wait()

	c.mu.Lock()
	for _, st := range out {
		c.results[st.Image] = st
	}
	c.mu.Unlock()
	return out, nil
}
