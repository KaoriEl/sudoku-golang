package cleanup

import (
	"context"
	"errors"
	"slices"
	"sudoku-golang/internal/engine"
	"testing"
	"time"
)

type fakeEngine struct {
	df       engine.DF
	removedC []string
	removedI []string
	pruned   []string
	failRef  string
}

func (f *fakeEngine) DiskDetail(context.Context) (engine.DF, error) { return f.df, nil }

func (f *fakeEngine) RemoveContainer(_ context.Context, id string) error {
	f.removedC = append(f.removedC, id)
	return nil
}

func (f *fakeEngine) RemoveImage(_ context.Context, ref string) error {
	if ref == f.failRef {
		return errors.New("conflict")
	}
	f.removedI = append(f.removedI, ref)
	return nil
}

func (f *fakeEngine) PruneBuildCache(_ context.Context, until string) (uint64, int, error) {
	f.pruned = append(f.pruned, until)
	return 100, 2, nil
}

const (
	exited      = "exited"
	imgDangling = "sha256:dangling"
	imgMulti    = "sha256:multi"
	idPlain     = "plain"
)

var now = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

func sample() engine.DF {
	old := now.Add(-10 * 24 * time.Hour)
	recent := now.Add(-time.Hour)
	return engine.DF{
		Containers: []engine.DFContainer{
			{ID: "run", Names: []string{"/web"}, Image: "nginx", State: "running"},
			{ID: idPlain, Names: []string{"/worker"}, Image: "app-worker", State: exited, SizeRw: 50},
			{ID: "vol", Names: []string{"/cache"}, Image: "app-cache", State: exited, Mounts: []engine.DFMount{{Type: "volume", Name: "abc"}}},
			{
				ID: "pg", Names: []string{"/pgsql_x"}, Image: "x-db", State: exited,
				Mounts: []engine.DFMount{{Type: "bind", Destination: "/var/lib/postgresql/data"}},
			},
			{ID: "pgname", Names: []string{"/other"}, Image: "postgres:16", State: "created"},
		},
		Images: []engine.DFImage{
			{ID: "sha256:used", RepoTags: []string{"nginx:1"}, Size: 10, Containers: 1},
			{ID: imgDangling, RepoTags: []string{"<none>:<none>"}, Size: 30, SharedSize: 10},
			{ID: imgMulti, RepoTags: []string{"a:1", "b:1"}, Size: 40},
		},
		Volumes: []engine.DFVolume{
			{Name: "used", UsageData: struct{ Size, RefCount int64 }{5, 1}},
			{Name: "orphan", UsageData: struct{ Size, RefCount int64 }{7, 0}},
		},
		BuildCache: []engine.DFBuildCache{
			{ID: "old", Size: 1000, LastUsedAt: &old},
			{ID: "new", Size: 200, LastUsedAt: &recent},
			{ID: "busy", Size: 300, InUse: true},
			{ID: "shared", Size: 400, Shared: true, LastUsedAt: &old},
		},
	}
}

func TestBuildPreview(t *testing.T) {
	p := Build(sample(), now)
	if len(p.Containers) != 4 {
		t.Fatalf("stopped containers = %d, want 4", len(p.Containers))
	}
	if c := p.Containers[0]; c.ID != idPlain || c.Protected {
		t.Fatalf("first container should be the unprotected one, got %+v", c)
	}
	reasons := map[string]string{}
	for _, c := range p.Containers {
		reasons[c.ID] = c.Reason
	}
	if reasons["vol"] != ReasonVolume || reasons["pg"] != ReasonDatabase || reasons["pgname"] != ReasonDatabase {
		t.Fatalf("reasons = %v", reasons)
	}
	if len(p.Images) != 2 || p.Images[0].ID != imgMulti || !p.Images[1].Dangling || p.Images[1].Unique != 20 {
		t.Fatalf("images = %+v", p.Images)
	}
	if p.Volumes.Count != 2 || p.Volumes.Unused != 1 || p.Volumes.UnusedSize != 7 {
		t.Fatalf("volumes = %+v", p.Volumes)
	}
	bc := p.BuildCache
	if bc.InUse != 300 || bc.Reclaimable != 1200 || bc.Buckets[0].Size != 1200 || bc.Buckets[2].Size != 1000 || bc.Buckets[2].Count != 2 {
		t.Fatalf("build cache = %+v", bc)
	}
}

func TestExecuteRespectsProtection(t *testing.T) {
	f := &fakeEngine{df: sample()}
	res, err := Execute(context.Background(), f, Request{
		Containers: []string{idPlain, "vol", "pg", "run", idPlain},
		Images:     []string{imgDangling, "sha256:used", imgMulti},
		BuildCache: "168h",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.removedC, []string{idPlain}) {
		t.Fatalf("removed containers = %v", f.removedC)
	}
	if !slices.Equal(f.removedI, []string{imgDangling, "a:1", "b:1"}) {
		t.Fatalf("removed images = %v", f.removedI)
	}
	if !slices.Equal(f.pruned, []string{"168h"}) {
		t.Fatalf("pruned = %v", f.pruned)
	}
	ok := 0
	for _, it := range res.Items {
		if it.OK {
			ok++
		}
	}
	if len(res.Items) != 8 || ok != 4 {
		t.Fatalf("items = %+v", res.Items)
	}
	if res.Freed != 50+20+40+100 {
		t.Fatalf("freed = %d", res.Freed)
	}
}

func TestExecuteValidates(t *testing.T) {
	f := &fakeEngine{df: sample()}
	if _, err := Execute(context.Background(), f, Request{}, now); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("empty request err = %v", err)
	}
	if _, err := Execute(context.Background(), f, Request{BuildCache: "1s"}, now); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("bad mode err = %v", err)
	}
	res, _ := Execute(context.Background(), &fakeEngine{df: sample(), failRef: imgDangling}, Request{Images: []string{imgDangling}}, now)
	if res.Items[0].OK || res.Freed != 0 {
		t.Fatalf("failed delete must not count: %+v", res)
	}
}
