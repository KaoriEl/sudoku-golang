package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		cur, latest string
		want        bool
	}{
		{"v1.2.3", "v1.2.4", true},
		{"v1.2.3", "v1.10.0", true},
		{"v2.0.0", "v1.9.9", false},
		{"v1.2.3", "v1.2.3", false},
		{"dev", "v9.9.9", false},
		{"1.0.0", "v1.0.1-rc1", true},
	}
	for _, c := range cases {
		if got := Newer(c.cur, c.latest); got != c.want {
			t.Errorf("Newer(%s, %s) = %v", c.cur, c.latest, got)
		}
	}
}

func tarGz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	p := filepath.Join(t.TempDir(), "a.tar.gz")
	f, _ := os.Create(p)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()
	raw, _ := os.ReadFile(p)
	return raw
}

func TestApply(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("zip archives on windows")
	}
	archive := tarGz(t, "sudoku", []byte("NEW BINARY"))
	sum := sha256.Sum256(archive)
	name := fmt.Sprintf("sudoku_1.0.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			_, _ = w.Write(archive)
		case "/sums":
			_, _ = fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), name)
		case "/bad":
			_, _ = fmt.Fprintf(w, "%s  %s\n", "00", name)
		}
	}))
	defer srv.Close()

	exe := filepath.Join(t.TempDir(), "sudoku")
	_ = os.WriteFile(exe, []byte("OLD"), 0o755) //nolint:gosec // an executable
	u := New("x/y")
	r := Release{Tag: "v1.0.0", Assets: []Asset{{Name: name, URL: srv.URL + "/a"}, {Name: checksumName, URL: srv.URL + "/sums"}}}
	if err := u.Apply(context.Background(), r, exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "NEW BINARY" {
		t.Fatalf("binary not replaced: %q", got)
	}

	r.Assets[1].URL = srv.URL + "/bad"
	if err := u.Apply(context.Background(), r, exe); !errors.Is(err, ErrChecksum) {
		t.Fatalf("bad checksum accepted: %v", err)
	}
	r.Assets = r.Assets[:1]
	if err := u.Apply(context.Background(), r, exe); !errors.Is(err, ErrNoChecksum) {
		t.Fatalf("missing checksums accepted: %v", err)
	}
}

func TestAssetForNames(t *testing.T) {
	r := Release{Assets: []Asset{
		{Name: "checksums.txt"},
		{Name: "sudoku_linux_arm64.tar.gz"},
		{Name: "sudoku_windows_amd64.zip"},
		{Name: "sudoku_1.0.3_darwin_arm64.tar.gz"},
	}}
	cases := [][3]string{
		{"linux", "arm64", "sudoku_linux_arm64.tar.gz"},
		{"windows", "amd64", "sudoku_windows_amd64.zip"},
		{"darwin", "arm64", "sudoku_1.0.3_darwin_arm64.tar.gz"},
	}
	for _, c := range cases {
		if a, ok := r.AssetFor(c[0], c[1]); !ok || a.Name != c[2] {
			t.Fatalf("%s/%s: %q %v", c[0], c[1], a.Name, ok)
		}
	}
	if _, ok := r.AssetFor("linux", "amd64"); ok {
		t.Fatal("linux/amd64 must not match linux_arm64")
	}
}
