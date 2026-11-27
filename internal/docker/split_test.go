package docker

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSplitCommand(t *testing.T) {
	cases := map[string][]string{
		"docker":                           {"docker"},
		"/usr/bin/env docker":              {"/usr/bin/env", "docker"},
		"docker compose":                   {"docker", "compose"},
		`"C:\Program Files\d.exe" compose`: {`C:\Program Files\d.exe`, "compose"},
		"  ":                               nil,
	}
	for in, want := range cases {
		if got := SplitCommand(in); !reflect.DeepEqual(got, want) {
			t.Errorf("SplitCommand(%q) = %q, want %q", in, got, want)
		}
	}
	exe := filepath.Join(t.TempDir(), "my docker")
	if err := os.WriteFile(exe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := SplitCommand(exe); len(got) != 1 || got[0] != exe {
		t.Fatalf("existing path with a space must stay one argument: %q", got)
	}
}
