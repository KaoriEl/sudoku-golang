package service

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	svcMySQL = "mysql"
	svcPgSQL = "pgsql"
)

func TestDisabledStore(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "alpha", "compose-sudoku.yaml")
	d := NewDisabledStore(Layout{Data: root, Root: root})

	got, err := d.For(file)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty store: %v %v", got, err)
	}
	if err := d.Set(file, svcPgSQL, true); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(file, svcMySQL, true); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(DisabledPath(Layout{Data: root, Root: root}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "alpha/compose-sudoku.yaml:\n    - mysql\n    - pgsql\n") {
		t.Fatalf("unexpected file:\n%s", raw)
	}

	fresh := NewDisabledStore(Layout{Data: root, Root: root})
	got, _ = fresh.For(file)
	if !reflect.DeepEqual(got, []string{svcMySQL, svcPgSQL}) {
		t.Fatalf("got %v", got)
	}

	if err := fresh.Set(file, svcMySQL, false); err != nil {
		t.Fatal(err)
	}
	if err := fresh.Set(file, svcPgSQL, false); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(DisabledPath(Layout{Data: root, Root: root}))
	if strings.Contains(string(raw), "alpha") {
		t.Fatalf("entry not removed:\n%s", raw)
	}
}

func TestEnabledServices(t *testing.T) {
	got := EnabledServices([]string{"app", "pgsql", "mysql"}, []string{"mysql"})
	if !reflect.DeepEqual(got, []string{"app", "pgsql"}) {
		t.Fatalf("got %v", got)
	}
}
