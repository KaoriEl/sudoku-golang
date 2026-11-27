// Package selfupdate replaces the running sudoku binary with the latest GitHub release built by
// goreleaser (see .goreleaser.yaml): it picks the archive for this OS/arch, verifies its SHA-256
// against checksums.txt, extracts the binary and swaps it in atomically.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	osWindows    = "windows"
	apiBase      = "https://api.github.com/repos/"
	checksumName = "checksums.txt"
	maxArchive   = 200 << 20
	httpTimeout  = 5 * time.Minute
	userAgent    = "sudoku-selfupdate"
)

var (
	ErrNoAsset    = errors.New("в релизе нет архива для этой платформы")
	ErrChecksum   = errors.New("контрольная сумма не совпала")
	ErrNoBinary   = errors.New("в архиве нет бинарника sudoku")
	ErrNoChecksum = errors.New("в релизе нет checksums.txt — обновление без проверки целостности запрещено")
)

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type Release struct {
	Tag       string    `json:"tag_name"`
	Name      string    `json:"name"`
	Published time.Time `json:"published_at"`
	URL       string    `json:"html_url"`
	Body      string    `json:"body"`
	Assets    []Asset   `json:"assets"`
}

type Updater struct {
	Repo   string
	Client *http.Client
}

func New(repo string) *Updater {
	return &Updater{Repo: repo, Client: &http.Client{Timeout: httpTimeout}}
}

func (u *Updater) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	if strings.HasPrefix(url, apiBase) {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" && strings.HasPrefix(url, "https://api.github.com/") {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := u.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

// Latest returns the newest published release.
func (u *Updater) Latest(ctx context.Context) (Release, error) {
	resp, err := u.get(ctx, apiBase+u.Repo+"/releases/latest")
	if err != nil {
		return Release{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var r Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return Release{}, fmt.Errorf("decode release: %w", err)
	}
	return r, nil
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "-")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Newer reports whether latest is a higher semantic version than current. A non-release current
// version (dev build) is never "older" — the caller decides with --force.
func Newer(current, latest string) bool {
	c, ok1 := parse(current)
	l, ok2 := parse(latest)
	if !ok1 || !ok2 {
		return false
	}
	for i := range 3 {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// AssetFor picks the archive built for goos/goarch ("sudoku_linux_amd64.tar.gz"; older releases had the
// version in the name, "sudoku_1.2.3_linux_amd64.tar.gz" — both match).
func (r Release) AssetFor(goos, goarch string) (Asset, bool) {
	want := "_" + goos + "_" + goarch + "."
	for _, a := range r.Assets {
		n := strings.ToLower(a.Name)
		if strings.Contains(n, want) && (strings.HasSuffix(n, ".tar.gz") || strings.HasSuffix(n, ".zip")) {
			return a, true
		}
	}
	return Asset{}, false
}

func (r Release) checksums() (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == checksumName || strings.HasSuffix(a.Name, "_"+checksumName) {
			return a, true
		}
	}
	return Asset{}, false
}

func (u *Updater) expectedSum(ctx context.Context, r Release, name string) (string, error) {
	a, ok := r.checksums()
	if !ok {
		return "", ErrNoChecksum
	}
	resp, err := u.get(ctx, a.URL)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("%w: нет строки для %s", ErrNoChecksum, name)
}

// download saves the asset into dir and checks its SHA-256.
func (u *Updater) download(ctx context.Context, a Asset, sum, dir string) (string, error) {
	resp, err := u.get(ctx, a.URL)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	f, err := os.CreateTemp(dir, "sudoku-download-*")
	if err != nil {
		return "", fmt.Errorf("temp: %w", err)
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxArchive))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("download %s: %w", a.Name, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("%w: %s (ожидалось %s, получено %s)", ErrChecksum, a.Name, sum, got)
	}
	return f.Name(), nil
}

func binaryName(goos string) string {
	if goos == osWindows {
		return "sudoku.exe"
	}
	return "sudoku"
}

// extract writes the sudoku binary from the archive to dst.
func extract(archive, name, goos, dst string) error {
	bin := binaryName(goos)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer func() { _ = out.Close() }()
	if strings.HasSuffix(strings.ToLower(name), ".zip") {
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return fmt.Errorf("zip: %w", err)
		}
		defer func() { _ = zr.Close() }()
		for _, f := range zr.File {
			if filepath.Base(f.Name) != bin || f.FileInfo().IsDir() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("zip entry: %w", err)
			}
			defer func() { _ = rc.Close() }()
			_, err = io.Copy(out, io.LimitReader(rc, maxArchive))
			return err //nolint:wrapcheck // io error
		}
		return ErrNoBinary
	}
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gunzip: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return ErrNoBinary
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == bin {
			_, err = io.Copy(out, io.LimitReader(tr, maxArchive))
			return err //nolint:wrapcheck // io error
		}
	}
}

// Apply downloads release r for this platform and replaces exe. On Unix the running process keeps
// its (now unlinked) inode; on Windows the running .exe is renamed to .old (allowed while running)
// and removed by CleanupOld on the next start.
func (u *Updater) Apply(ctx context.Context, r Release, exe string) error {
	a, ok := r.AssetFor(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return fmt.Errorf("%w: %s/%s", ErrNoAsset, runtime.GOOS, runtime.GOARCH)
	}
	sum, err := u.expectedSum(ctx, r, a.Name)
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	archive, err := u.download(ctx, a, sum, dir)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(archive) }()
	next := exe + ".new"
	if err := extract(archive, a.Name, runtime.GOOS, next); err != nil {
		_ = os.Remove(next)
		return err
	}
	if runtime.GOOS == osWindows {
		_ = os.Remove(exe + ".old")
		if err := os.Rename(exe, exe+".old"); err != nil {
			_ = os.Remove(next)
			return fmt.Errorf("move running binary: %w", err)
		}
	}
	if err := os.Rename(next, exe); err != nil {
		if runtime.GOOS == osWindows {
			_ = os.Rename(exe+".old", exe)
		}
		return fmt.Errorf("install %s: %w", exe, err)
	}
	return nil
}

// CleanupOld removes the binary left over by a Windows self-update.
func CleanupOld() {
	if runtime.GOOS != "windows" {
		return
	}
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}
