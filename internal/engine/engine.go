// Package engine is a small Docker Engine API client over a unix socket, a Windows named pipe or TCP.
// It covers exactly what sudoku needs and keeps the hot paths (container list, stats, events) free of
// process spawns.
package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sudoku-golang/internal/docker"
	"time"
)

const (
	engineTimeout  = 15 * time.Second
	containersPath = "/containers/"
	qFalse         = "false"
	qTrue          = "true"
	listPath       = "/containers/json"
	labelProject   = "com.docker.compose.project"
	contentJSON    = "application/json"
)

type Engine struct {
	endpoint Endpoint
	client   *http.Client
	// long has no client timeout: streams, deletes and prunes are bounded by the caller's context.
	long *http.Client
}

func New(ep Endpoint) *Engine {
	transport := &http.Transport{
		DialContext:         func(ctx context.Context, _, _ string) (net.Conn, error) { return ep.dial(ctx) },
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     30 * time.Second,
	}
	return &Engine{
		endpoint: ep,
		client:   &http.Client{Transport: transport, Timeout: engineTimeout},
		long:     &http.Client{Transport: transport},
	}
}

// Socket is the human-readable endpoint (kept for the API field name).
func (e *Engine) Socket() string {
	return e.endpoint.String()
}

func (e *Engine) Endpoint() Endpoint {
	return e.endpoint
}

// EngineError is a non-2xx answer from the Docker Engine API.
type EngineError struct {
	Path    string
	Status  int
	Message string
}

func (e *EngineError) Error() string {
	return fmt.Sprintf("engine %s: %d: %s", e.Path, e.Status, e.Message)
}

// IsNotFound reports a 404 from the engine (unknown container/image/exec).
func IsNotFound(err error) bool {
	var ee *EngineError
	return errors.As(err, &ee) && ee.Status == http.StatusNotFound
}

// IsEngineError reports whether err is an HTTP answer from the engine (as opposed to the endpoint
// being unreachable), i.e. whether falling back to the docker CLI would not help.
func IsEngineError(err error) bool {
	var ee *EngineError
	return errors.As(err, &ee)
}

type request struct {
	method  string
	path    string
	query   url.Values
	body    io.Reader
	headers map[string]string
}

// do sends a request and returns the response on 2xx; any other status becomes an *EngineError.
// The caller closes the body.
func (e *Engine) do(ctx context.Context, client *http.Client, r request) (*http.Response, error) {
	u := "http://docker" + r.path
	if len(r.query) > 0 {
		u += "?" + r.query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, r.method, u, r.body)
	if err != nil {
		return nil, fmt.Errorf("engine request: %w", err)
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("engine %s: %w", r.path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer func() { _ = resp.Body.Close() }()
		return nil, errorFrom(r.path, resp)
	}
	return resp, nil
}

func errorFrom(path string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	msg := strings.TrimSpace(string(body))
	var m struct{ Message string }
	if json.Unmarshal(body, &m) == nil && m.Message != "" {
		msg = m.Message
	}
	return &EngineError{Path: path, Status: resp.StatusCode, Message: msg}
}

func (e *Engine) send(ctx context.Context, client *http.Client, method, path string, query url.Values, out any) error {
	return e.sendJSON(ctx, client, method, path, query, nil, out)
}

func (e *Engine) sendJSON(ctx context.Context, client *http.Client, method, path string, query url.Values, in, out any) error {
	r := request{method: method, path: path, query: query}
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("engine %s encode: %w", path, err)
		}
		r.body = bytes.NewReader(raw)
		r.headers = map[string]string{"Content-Type": contentJSON}
	}
	resp, err := e.do(ctx, client, r)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("engine %s decode: %w", path, err)
	}
	return nil
}

func (e *Engine) get(ctx context.Context, path string, query url.Values, out any) error {
	return e.send(ctx, e.client, http.MethodGet, path, query, out)
}

// Ping checks that the engine answers (`GET /_ping`).
func (e *Engine) Ping(ctx context.Context) error {
	resp, err := e.do(ctx, e.client, request{method: http.MethodGet, path: "/_ping"})
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

type Version struct {
	Version       string `json:"version"`
	APIVersion    string `json:"apiVersion"`
	Os            string `json:"os"`
	Arch          string `json:"arch"`
	KernelVersion string `json:"kernelVersion"`
}

func (e *Engine) Version(ctx context.Context) (Version, error) {
	var raw struct {
		Version       string
		APIVersion    string `json:"ApiVersion"`
		Os            string
		Arch          string
		KernelVersion string
	}
	if err := e.get(ctx, "/version", nil, &raw); err != nil {
		return Version{}, err
	}
	return Version{Version: raw.Version, APIVersion: raw.APIVersion, Os: raw.Os, Arch: raw.Arch, KernelVersion: raw.KernelVersion}, nil
}

type Info struct {
	NCPU            int    `json:"ncpu"`
	MemTotal        uint64 `json:"memTotal"`
	ServerVersion   string `json:"serverVersion"`
	OperatingSystem string `json:"operatingSystem"`
	KernelVersion   string `json:"kernelVersion"`
	Driver          string `json:"driver"`
	CgroupVersion   string `json:"cgroupVersion"`
	Containers      int    `json:"containers"`
	Running         int    `json:"running"`
	Images          int    `json:"images"`
	Name            string `json:"name"`
}

func (e *Engine) Info(ctx context.Context) (Info, error) {
	var raw rawInfo
	if err := e.get(ctx, "/info", nil, &raw); err != nil {
		return Info{}, err
	}
	return Info{
		NCPU: raw.NCPU, MemTotal: raw.MemTotal, ServerVersion: raw.ServerVersion, OperatingSystem: raw.OperatingSystem,
		KernelVersion: raw.KernelVersion, Driver: raw.Driver, CgroupVersion: raw.CgroupVersion, Containers: raw.Containers,
		Running: raw.ContainersRunning, Images: raw.Images, Name: raw.Name,
	}, nil
}

func filterJSON(f map[string][]string) string {
	raw, _ := json.Marshal(f)
	return string(raw)
}

func (e *Engine) RunningCompose(ctx context.Context) ([]ListedContainer, error) {
	var out []ListedContainer
	err := e.get(ctx, listPath, url.Values{"filters": {filterJSON(map[string][]string{"label": {labelProject}, "status": {"running"}})}}, &out)
	return out, err
}

// ComposeContainers lists every compose container (any state). It is one cheap API call; the
// expensive per-container detail comes from InspectContainer.
func (e *Engine) ComposeContainers(ctx context.Context) ([]ContainerSummary, error) {
	var out []ContainerSummary
	err := e.get(ctx, listPath, url.Values{"all": {qTrue}, "filters": {filterJSON(map[string][]string{"label": {labelProject}})}}, &out)
	return out, err
}

// InspectContainer is `docker inspect` over the API; the JSON has the same shape as the CLI's.
func (e *Engine) InspectContainer(ctx context.Context, id string) (docker.Container, error) {
	var c docker.Container
	err := e.get(ctx, containersPath+url.PathEscape(id)+"/json", nil, &c)
	return c, err
}

func (e *Engine) ContainerStats(ctx context.Context, id string) (Stats, error) {
	var s Stats
	err := e.get(ctx, containersPath+url.PathEscape(id)+"/stats", url.Values{"stream": {qFalse}, "one-shot": {qTrue}}, &s)
	return s, err
}

// ContainerAction is start / stop / restart / kill / pause / unpause; stop and restart give the
// container `timeout` seconds before SIGKILL (like `docker stop -t`).
func (e *Engine) ContainerAction(ctx context.Context, id, action string, timeout int) error {
	q := url.Values{}
	if action == "stop" || action == "restart" {
		q.Set("t", strconv.Itoa(timeout))
	}
	err := e.send(ctx, e.long, http.MethodPost, containersPath+url.PathEscape(id)+"/"+action, q, nil)
	var ee *EngineError
	if errors.As(err, &ee) && ee.Status == http.StatusNotModified {
		return nil // already in the requested state
	}
	return err
}

type DiskUsage struct {
	Images          int    `json:"images"`
	ImagesSize      uint64 `json:"imagesSize"`
	ImagesUnused    uint64 `json:"imagesUnused"`
	Containers      int    `json:"containers"`
	ContainersSize  uint64 `json:"containersSize"`
	Volumes         int    `json:"volumes"`
	VolumesSize     uint64 `json:"volumesSize"`
	BuildCache      int    `json:"buildCache"`
	BuildCacheSize  uint64 `json:"buildCacheSize"`
	BuildCacheInUse uint64 `json:"buildCacheInUse"`
}

func pos(v int64) uint64 {
	if v < 0 {
		return 0
	}
	return uint64(v)
}

func (e *Engine) DiskUsage(ctx context.Context) (DiskUsage, error) {
	var raw rawDF
	if err := e.send(ctx, e.long, http.MethodGet, "/system/df", nil, &raw); err != nil {
		return DiskUsage{}, err
	}
	du := DiskUsage{
		Images:     len(raw.Images),
		ImagesSize: pos(raw.LayersSize),
		Containers: len(raw.Containers),
		Volumes:    len(raw.Volumes),
		BuildCache: len(raw.BuildCache),
	}
	for _, im := range raw.Images {
		if im.Containers == 0 {
			du.ImagesUnused += pos(im.Size - im.SharedSize)
		}
	}
	for _, c := range raw.Containers {
		du.ContainersSize += pos(c.SizeRw)
	}
	for _, v := range raw.Volumes {
		du.VolumesSize += pos(v.UsageData.Size)
	}
	for _, b := range raw.BuildCache {
		du.BuildCacheSize += pos(b.Size)
		if b.InUse {
			du.BuildCacheInUse += pos(b.Size)
		}
	}
	return du, nil
}

// DiskDetail returns the full /system/df listing (slow: Docker computes sizes).
func (e *Engine) DiskDetail(ctx context.Context) (DF, error) {
	var df DF
	err := e.send(ctx, e.long, http.MethodGet, "/system/df", nil, &df)
	return df, err
}

// RemoveContainer deletes a stopped container. Volumes are never removed (v=false) and running
// containers are refused by Docker (force=false).
func (e *Engine) RemoveContainer(ctx context.Context, id string) error {
	return e.send(ctx, e.long, http.MethodDelete, containersPath+url.PathEscape(id), url.Values{"v": {qFalse}, "force": {qFalse}}, nil)
}

// RemoveImage deletes an image by ID or reference without force, so images used by any container are refused.
func (e *Engine) RemoveImage(ctx context.Context, ref string) error {
	return e.send(ctx, e.long, http.MethodDelete, "/images/"+escapeRef(ref), url.Values{"force": {qFalse}}, nil)
}

// PruneBuildCache removes unused build cache; until (e.g. "168h") keeps records used more recently.
func (e *Engine) PruneBuildCache(ctx context.Context, until string) (uint64, int, error) {
	q := url.Values{"all": {qTrue}}
	if until != "" {
		q.Set("filters", filterJSON(map[string][]string{"until": {until}}))
	}
	var rep buildPruneReport
	if err := e.send(ctx, e.long, http.MethodPost, "/build/prune", q, &rep); err != nil {
		return 0, 0, err
	}
	return rep.SpaceReclaimed, len(rep.CachesDeleted), nil
}

// escapeRef escapes an image reference but keeps "/" separators the way the docker CLI sends them.
func escapeRef(ref string) string {
	parts := strings.Split(ref, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// ImageInspect returns the local image's id, tags and registry digests.
func (e *Engine) ImageInspect(ctx context.Context, ref string) (Image, error) {
	var im Image
	err := e.get(ctx, "/images/"+escapeRef(ref)+"/json", nil, &im)
	return im, err
}

// DistributionInspect asks the registry for the manifest digest of ref without pulling anything.
// Public images need no auth; for private ones registryAuth is the base64 X-Registry-Auth payload.
func (e *Engine) DistributionInspect(
	ctx context.Context, ref, registryAuth string,
) (string, error) {
	r := request{method: http.MethodGet, path: "/distribution/" + escapeRef(ref) + "/json"}
	if registryAuth != "" {
		r.headers = map[string]string{"X-Registry-Auth": registryAuth}
	}
	resp, err := e.do(ctx, e.long, r)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Descriptor struct {
			Digest string
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("distribution %s decode: %w", ref, err)
	}
	return out.Descriptor.Digest, nil
}

// GetArchive streams a tar of path from the container (works on stopped containers too).
func (e *Engine) GetArchive(ctx context.Context, id, path string) (io.ReadCloser, error) {
	resp, err := e.do(ctx, e.long, request{
		method: http.MethodGet, path: containersPath + url.PathEscape(id) + "/archive", query: url.Values{"path": {path}},
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// PutArchive extracts a tar stream into dir inside the container.
func (e *Engine) PutArchive(ctx context.Context, id, dir string, tar io.Reader) error {
	resp, err := e.do(ctx, e.long, request{
		method: http.MethodPut, path: containersPath + url.PathEscape(id) + "/archive",
		query: url.Values{"path": {dir}}, body: tar, headers: map[string]string{"Content-Type": "application/x-tar"},
	})
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("put archive: %w", err)
	}
	return nil
}

// Event is one entry of the /events stream.
type Event struct {
	Type   string `json:"Type"`
	Action string `json:"Action"`
	Actor  struct {
		ID         string            `json:"ID"`
		Attributes map[string]string `json:"Attributes"`
	} `json:"Actor"`
	Time     int64 `json:"time"`
	TimeNano int64 `json:"timeNano"`
}

// Events blocks, calling fn for every event that matches filters, until ctx ends or the stream breaks.
func (e *Engine) Events(ctx context.Context, filters map[string][]string, fn func(Event)) error {
	q := url.Values{}
	if len(filters) > 0 {
		q.Set("filters", filterJSON(filters))
	}
	resp, err := e.do(ctx, e.long, request{method: http.MethodGet, path: "/events", query: q})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	dec := json.NewDecoder(resp.Body)
	for {
		var ev Event
		if err := dec.Decode(&ev); err != nil {
			if ctx.Err() != nil {
				return ctx.Err() //nolint:wrapcheck // cancellation is the normal end
			}
			return fmt.Errorf("events: %w", err)
		}
		fn(ev)
	}
}

// ExecConfig is the body of POST /containers/{id}/exec.
type ExecConfig struct {
	Cmd          []string `json:"Cmd"`
	Env          []string `json:"Env,omitempty"`
	Tty          bool     `json:"Tty"`
	AttachStdin  bool     `json:"AttachStdin"`
	AttachStdout bool     `json:"AttachStdout"`
	AttachStderr bool     `json:"AttachStderr"`
	ConsoleSize  *[2]uint `json:"ConsoleSize,omitempty"`
}

func (e *Engine) ExecCreate(ctx context.Context, id string, cfg ExecConfig) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	if err := e.sendJSON(ctx, e.client, http.MethodPost, containersPath+url.PathEscape(id)+"/exec", nil, cfg, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// ExecResize sets the tty size of a running exec.
func (e *Engine) ExecResize(ctx context.Context, execID string, cols, rows uint) error {
	q := url.Values{"w": {strconv.FormatUint(uint64(cols), 10)}, "h": {strconv.FormatUint(uint64(rows), 10)}}
	return e.send(ctx, e.client, http.MethodPost, "/exec/"+url.PathEscape(execID)+"/resize", q, nil)
}

// ExecInspect reports whether the exec is still running and its exit code.
func (e *Engine) ExecInspect(ctx context.Context, execID string) (bool, int, error) {
	var out struct {
		Running  bool
		ExitCode int
	}
	err := e.get(ctx, "/exec/"+url.PathEscape(execID)+"/json", nil, &out)
	return out.Running, out.ExitCode, err
}

// Hijacked is the raw bidirectional stream of an attached exec: with Tty it is the terminal byte
// stream itself (no multiplexing headers).
type Hijacked struct {
	net.Conn
	r *bufio.Reader
}

func (h *Hijacked) Read(p []byte) (int, error) {
	return h.r.Read(p) //nolint:wrapcheck // raw stream
}

// CloseWrite half-closes the stream so the process sees EOF on stdin, when the transport supports it.
func (h *Hijacked) CloseWrite() error {
	if cw, ok := h.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite() //nolint:wrapcheck // raw stream
	}
	return nil
}

// ExecStart starts an exec and hijacks the connection (HTTP Upgrade: tcp), like `docker exec -it`
// does. This needs no pty on the host, so the terminal works the same on Windows, Linux and macOS.
func (e *Engine) ExecStart(ctx context.Context, execID string, tty bool) (*Hijacked, error) {
	conn, err := e.endpoint.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("engine dial: %w", err)
	}
	body, _ := json.Marshal(map[string]bool{"Detach": false, "Tty": tty})
	path := "/exec/" + url.PathEscape(execID) + "/start"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker"+path, bytes.NewReader(body))
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("engine request: %w", err)
	}
	req.Header.Set("Content-Type", contentJSON)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "tcp")
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("engine %s: %w", path, err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("engine %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		defer func() { _ = conn.Close() }()
		defer func() { _ = resp.Body.Close() }()
		return nil, errorFrom(path, resp)
	}
	if resp.StatusCode == http.StatusSwitchingProtocols {
		_ = resp.Body.Close() // a 101 has no body; the stream is the connection itself
	}
	_ = conn.SetDeadline(time.Time{})
	return &Hijacked{Conn: conn, r: br}, nil
}
