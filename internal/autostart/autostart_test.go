package autostart

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testSpec() Spec {
	return Spec{
		Executable: "/opt/sudoku & co/sudoku",
		Args:       []string{"serve", "--addr", "127.0.0.1:7777"},
		WorkDir:    "/Users/me/Projects",
		Env:        map[string]string{"PATH": "/usr/bin:/bin", "ROOT_PROJECTS_FOLDER": "/Users/me/Projects"},
	}
}

func TestPlistIsValid(t *testing.T) {
	out := plist(testSpec(), "/tmp/sudoku.log")
	if !strings.Contains(out, "<string>/opt/sudoku &amp; co/sudoku</string>") {
		t.Fatalf("executable not escaped:\n%s", out)
	}
	if runtime.GOOS != osDarwin {
		return
	}
	path := filepath.Join(t.TempDir(), "test.plist")
	if err := os.WriteFile(path, []byte(out), fileMode); err != nil {
		t.Fatal(err)
	}
	if res, err := exec.CommandContext(t.Context(), "plutil", "-lint", path).CombinedOutput(); err != nil {
		t.Fatalf("plutil: %v %s", err, res)
	}
}

func TestUnit(t *testing.T) {
	out := unit(testSpec())
	if !strings.Contains(out, `ExecStart="/opt/sudoku & co/sudoku" "serve" "--addr" "127.0.0.1:7777"`) {
		t.Fatalf("unexpected unit:\n%s", out)
	}
	if !strings.Contains(out, "WantedBy=default.target") {
		t.Fatalf("missing install section:\n%s", out)
	}
}

func TestVBS(t *testing.T) {
	out := vbs(Spec{
		Executable: `C:\Program Files\sudoku\sudoku.exe`, Args: []string{"serve"},
		WorkDir: `C:\Users\me\pro"j`, Env: map[string]string{"PATH": `C:\x`},
	})
	for _, want := range []string{
		`sh.CurrentDirectory = "C:\Users\me\pro""j"`,
		`env("PATH") = "C:\x"`,
		`sh.Run """C:\Program Files\sudoku\sudoku.exe"" ""serve""", 0, False`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
