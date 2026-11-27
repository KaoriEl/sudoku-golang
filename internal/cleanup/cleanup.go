// Package cleanup plans and performs a conservative Docker disk cleanup.
//
// Safety rules (enforced here, not only in the UI):
//   - volumes are never removed, neither directly nor through container removal (v=false);
//   - only stopped containers are candidates; containers that mount a Docker volume or look like a
//     database are protected and cannot be removed through this package;
//   - only images that no container (running or stopped) uses are candidates, and deletes are not forced;
//   - build cache that is in use is never pruned (Docker refuses it).
//
// Execute re-reads the Docker state and re-checks every requested item, so a stale preview cannot
// remove something that became protected in the meantime.
package cleanup

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sudoku-golang/internal/engine"
	"time"
)

const (
	ReasonVolume   = "volume"
	ReasonDatabase = "database"

	KindContainer  = "container"
	KindImage      = "image"
	KindBuildCache = "buildCache"

	noneTag = "<none>:<none>"

	CacheAll = "all"
)

// CacheAges are the accepted build cache prune modes: "all" or an "until" duration.
var CacheAges = []string{CacheAll, "24h", "168h", "720h"}

var (
	ErrBadRequest  = errors.New("invalid cleanup request")
	errNotEligible = errors.New("недоступно для очистки")
)

type Engine interface {
	DiskDetail(ctx context.Context) (engine.DF, error)
	RemoveContainer(ctx context.Context, id string) error
	RemoveImage(ctx context.Context, ref string) error
	PruneBuildCache(ctx context.Context, until string) (uint64, int, error)
}

type Container struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Image     string    `json:"image"`
	Project   string    `json:"project"`
	Service   string    `json:"service"`
	State     string    `json:"state"`
	Status    string    `json:"status"`
	Size      uint64    `json:"size"`
	Created   time.Time `json:"created"`
	Protected bool      `json:"protected"`
	Reason    string    `json:"reason"`
	Volumes   []string  `json:"volumes"`
}

type Image struct {
	ID       string    `json:"id"`
	Tags     []string  `json:"tags"`
	Size     uint64    `json:"size"`
	Unique   uint64    `json:"unique"`
	Created  time.Time `json:"created"`
	Dangling bool      `json:"dangling"`
	Project  string    `json:"project"`
	Service  string    `json:"service"`
}

type CacheBucket struct {
	Mode  string `json:"mode"`
	Size  uint64 `json:"size"`
	Count int    `json:"count"`
}

type BuildCache struct {
	Count       int           `json:"count"`
	Total       uint64        `json:"total"`
	InUse       uint64        `json:"inUse"`
	Reclaimable uint64        `json:"reclaimable"`
	Buckets     []CacheBucket `json:"buckets"`
}

type Volumes struct {
	Count      int    `json:"count"`
	Size       uint64 `json:"size"`
	Unused     int    `json:"unused"`
	UnusedSize uint64 `json:"unusedSize"`
}

type Preview struct {
	ScannedAt  time.Time   `json:"scannedAt"`
	Containers []Container `json:"containers"`
	Images     []Image     `json:"images"`
	BuildCache BuildCache  `json:"buildCache"`
	Volumes    Volumes     `json:"volumes"`
}

type Request struct {
	Containers []string `json:"containers"`
	Images     []string `json:"images"`
	BuildCache string   `json:"buildCache"`
}

type ItemResult struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Size  uint64 `json:"size"`
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type Result struct {
	Items []ItemResult `json:"items"`
	Freed uint64       `json:"freed"`
}

func pos(v int64) uint64 {
	if v < 0 {
		return 0
	}
	return uint64(v)
}

var reasonText = map[string]string{ReasonVolume: "подключён том Docker", ReasonDatabase: "похоже на базу данных"}

var dbWords = []string{
	"postgres", "pgsql", "postgis", "timescale", "mysql", "mariadb", "percona", "mongo", "redis", "keydb", "valkey",
	"dragonfly", "elasticsearch", "opensearch", "clickhouse", "rabbitmq", "cassandra", "scylla", "influx", "etcd",
	"minio", "kafka", "zookeeper", "neo4j", "couchdb", "couchbase", "cockroach", "mssql", "oracle", "sqlserver",
}

var dbPaths = []string{
	"/var/lib/postgresql", "/var/lib/mysql", "/data/db", "/bitnami", "/usr/share/elasticsearch/data",
	"/usr/share/opensearch/data", "/var/lib/clickhouse", "/var/lib/rabbitmq", "/var/lib/redis", "/var/opt/mssql",
}

// looksLikeDatabase is deliberately generous: a false positive only means "not offered for removal".
func looksLikeDatabase(c engine.DFContainer) bool {
	hay := strings.ToLower(strings.Join(append([]string{c.Image, c.Labels["com.docker.compose.service"]}, c.Names...), " "))
	for _, w := range dbWords {
		if strings.Contains(hay, w) {
			return true
		}
	}
	for _, m := range c.Mounts {
		for _, p := range dbPaths {
			if strings.HasPrefix(m.Destination, p) {
				return true
			}
		}
	}
	return false
}

func containerName(c engine.DFContainer) string {
	if len(c.Names) == 0 {
		return shortID(c.ID)
	}
	return strings.TrimPrefix(c.Names[0], "/")
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func isStopped(state string) bool {
	return state == "exited" || state == "created" || state == "dead"
}

func buildContainer(c engine.DFContainer) Container {
	out := Container{
		ID: c.ID, Name: containerName(c), Image: c.Image, State: c.State, Status: c.Status, Size: pos(c.SizeRw),
		Created: time.Unix(c.Created, 0).UTC(), Project: c.Labels["com.docker.compose.project"],
		Service: c.Labels["com.docker.compose.service"], Volumes: []string{},
	}
	for _, m := range c.Mounts {
		if m.Type == "volume" {
			name := m.Name
			if name == "" {
				name = m.Destination
			}
			out.Volumes = append(out.Volumes, name)
		}
	}
	switch {
	case len(out.Volumes) > 0:
		out.Protected, out.Reason = true, ReasonVolume
	case looksLikeDatabase(c):
		out.Protected, out.Reason = true, ReasonDatabase
	}
	return out
}

func imageTags(im engine.DFImage) []string {
	tags := make([]string, 0, len(im.RepoTags))
	for _, t := range im.RepoTags {
		if t != "" && t != noneTag {
			tags = append(tags, t)
		}
	}
	return tags
}

func buildImage(im engine.DFImage) Image {
	tags := imageTags(im)
	unique := pos(im.Size)
	if im.SharedSize > 0 {
		unique = pos(im.Size - im.SharedSize)
	}
	return Image{
		ID: im.ID, Tags: tags, Size: pos(im.Size), Unique: unique, Created: time.Unix(im.Created, 0).UTC(),
		Dangling: len(tags) == 0, Project: im.Labels["com.docker.compose.project"], Service: im.Labels["com.docker.compose.service"],
	}
}

func cacheMatches(b engine.DFBuildCache, mode string, now time.Time) bool {
	if b.InUse {
		return false
	}
	if mode == CacheAll {
		return true
	}
	d, err := time.ParseDuration(mode)
	if err != nil {
		return false
	}
	last := b.CreatedAt
	if b.LastUsedAt != nil {
		last = *b.LastUsedAt
	}
	return now.Sub(last) >= d
}

func buildCache(items []engine.DFBuildCache, now time.Time) BuildCache {
	out := BuildCache{Count: len(items), Buckets: make([]CacheBucket, 0, len(CacheAges))}
	for _, b := range items {
		out.Total += pos(b.Size)
		if b.InUse {
			out.InUse += pos(b.Size)
		} else if !b.Shared {
			out.Reclaimable += pos(b.Size)
		}
	}
	for _, mode := range CacheAges {
		bucket := CacheBucket{Mode: mode}
		for _, b := range items {
			if cacheMatches(b, mode, now) {
				bucket.Count++
				if !b.Shared {
					bucket.Size += pos(b.Size)
				}
			}
		}
		out.Buckets = append(out.Buckets, bucket)
	}
	return out
}

// Build turns a /system/df listing into the cleanup preview.
func Build(df engine.DF, now time.Time) Preview {
	p := Preview{ScannedAt: now.UTC(), Containers: []Container{}, Images: []Image{}}
	for _, c := range df.Containers {
		if isStopped(c.State) {
			p.Containers = append(p.Containers, buildContainer(c))
		}
	}
	for _, im := range df.Images {
		if im.Containers == 0 {
			p.Images = append(p.Images, buildImage(im))
		}
	}
	for _, v := range df.Volumes {
		p.Volumes.Count++
		p.Volumes.Size += pos(v.UsageData.Size)
		if v.UsageData.RefCount == 0 {
			p.Volumes.Unused++
			p.Volumes.UnusedSize += pos(v.UsageData.Size)
		}
	}
	p.BuildCache = buildCache(df.BuildCache, now)
	sort.SliceStable(p.Containers, func(i, j int) bool {
		if p.Containers[i].Protected != p.Containers[j].Protected {
			return !p.Containers[i].Protected
		}
		return p.Containers[i].Name < p.Containers[j].Name
	})
	sort.SliceStable(p.Images, func(i, j int) bool { return p.Images[i].Unique > p.Images[j].Unique })
	return p
}

// Scan reads the current Docker state and builds a preview.
func Scan(ctx context.Context, eng Engine, now time.Time) (Preview, error) {
	df, err := eng.DiskDetail(ctx)
	if err != nil {
		return Preview{}, fmt.Errorf("disk usage: %w", err)
	}
	return Build(df, now), nil
}

func (r Request) Validate() error {
	if len(r.Containers) == 0 && len(r.Images) == 0 && r.BuildCache == "" {
		return fmt.Errorf("%w: nothing selected", ErrBadRequest)
	}
	if r.BuildCache != "" && !slices.Contains(CacheAges, r.BuildCache) {
		return fmt.Errorf("%w: build cache mode %q", ErrBadRequest, r.BuildCache)
	}
	return nil
}

// Execute re-scans Docker, removes the requested items that are still eligible and reports per item.
func Execute(ctx context.Context, eng Engine, req Request, now time.Time) (Result, error) {
	if err := req.Validate(); err != nil {
		return Result{}, err
	}
	p, err := Scan(ctx, eng, now)
	if err != nil {
		return Result{}, err
	}
	res := Result{Items: []ItemResult{}}
	add := func(it ItemResult) {
		if it.OK {
			res.Freed += it.Size
		}
		res.Items = append(res.Items, it)
	}

	containers := map[string]Container{}
	for _, c := range p.Containers {
		containers[c.ID] = c
	}
	for _, id := range dedupe(req.Containers) {
		c, ok := containers[id]
		it := ItemResult{Kind: KindContainer, ID: id, Name: shortID(id)}
		switch {
		case !ok:
			it.Error = errNotEligible.Error() + ": контейнер запущен или уже удалён"
		case c.Protected:
			it.Name, it.Error = c.Name, errNotEligible.Error()+": защищён — "+reasonText[c.Reason]
		default:
			it.Name, it.Size = c.Name, c.Size
			it.OK, it.Error = errText(eng.RemoveContainer(ctx, c.ID))
		}
		add(it)
	}

	images := map[string]Image{}
	for _, im := range p.Images {
		images[im.ID] = im
	}
	for _, id := range dedupe(req.Images) {
		im, ok := images[id]
		it := ItemResult{Kind: KindImage, ID: id, Name: shortID(id)}
		if !ok {
			it.Error = errNotEligible.Error() + ": образ используется контейнером или уже удалён"
			add(it)
			continue
		}
		if len(im.Tags) > 0 {
			it.Name = strings.Join(im.Tags, ", ")
		}
		it.Size = im.Unique
		it.OK, it.Error = errText(removeImage(ctx, eng, im))
		add(it)
	}

	if req.BuildCache != "" {
		until := req.BuildCache
		if until == CacheAll {
			until = ""
		}
		freed, n, err := eng.PruneBuildCache(ctx, until)
		it := ItemResult{Kind: KindBuildCache, ID: req.BuildCache, Name: fmt.Sprintf("%d", n), Size: freed}
		it.OK, it.Error = errText(err)
		add(it)
	}
	return res, nil
}

// removeImage deletes by ID when the image has at most one tag; with several tags Docker refuses an
// unforced delete by ID, so each tag is removed and the last untag deletes the image.
func removeImage(ctx context.Context, eng Engine, im Image) error {
	if len(im.Tags) <= 1 {
		return eng.RemoveImage(ctx, im.ID) //nolint:wrapcheck // engine errors are already descriptive
	}
	for _, t := range im.Tags {
		if err := eng.RemoveImage(ctx, t); err != nil {
			return err //nolint:wrapcheck // engine errors are already descriptive
		}
	}
	return nil
}

func errText(err error) (bool, string) {
	if err != nil {
		return false, err.Error()
	}
	return true, ""
}

func dedupe(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
