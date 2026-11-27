package service

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func testProjects() []Project {
	return []Project{
		{File: "/r/compose.yaml", Name: InfraName, Infra: true, Services: []string{"elasticsearch", "redis", "traefik"}},
		{File: "/r/api/compose-sudoku.yaml", Name: "api", Services: []string{"app", "redis", "worker"}},
		{File: "/r/billing/compose-sudoku.yaml", Name: "billing", Services: []string{"app", "db"}},
	}
}

func files(es []Entry) map[string][]string {
	out := map[string][]string{}
	for _, e := range es {
		out[e.Project.Name] = e.Services
	}
	return out
}

func TestResolve(t *testing.T) {
	cases := []struct {
		targets []string
		want    map[string][]string
	}{
		{[]string{"api"}, map[string][]string{"api": nil}},
		{[]string{"core"}, map[string][]string{InfraName: nil}},
		{[]string{"redis"}, map[string][]string{InfraName: {"redis"}, "api": {"redis"}}},
		{[]string{"api/worker", "billing:db"}, map[string][]string{"api": {"worker"}, "billing": {"db"}}},
		{[]string{"*/app"}, map[string][]string{"api": {"app"}, "billing": {"app"}}},
		{[]string{"bill*"}, map[string][]string{"billing": nil}},
		{[]string{"api", "api/worker"}, map[string][]string{"api": nil}},
	}
	for _, tc := range cases {
		got, err := Resolve(testProjects(), tc.targets)
		if err != nil {
			t.Fatalf("%v: %v", tc.targets, err)
		}
		if !reflect.DeepEqual(files(got), tc.want) {
			t.Errorf("%v: got %v, want %v", tc.targets, files(got), tc.want)
		}
	}
}

func TestResolveKeepsDiscoveryOrderAndReportsUnknown(t *testing.T) {
	got, _ := Resolve(testProjects(), []string{"billing", "infra"})
	if got[0].Project.Name != InfraName || got[1].Project.Name != "billing" {
		t.Fatalf("order: %v", files(got))
	}
	if _, err := Resolve(testProjects(), []string{"nope"}); !errors.Is(err, ErrUnknownTarget) {
		t.Fatalf("unknown target: %v", err)
	}
}

func TestNeedsServices(t *testing.T) {
	p := testProjects()
	if NeedsServices(p, []string{"api", "infra"}) {
		t.Fatal("project names need no service lookup")
	}
	if !NeedsServices(p, []string{"redis"}) || !NeedsServices(p, []string{"api/app"}) {
		t.Fatal("service targets need the service lists")
	}
}

func TestProjectsFrom(t *testing.T) {
	root := t.TempDir()
	l := Layout{Data: root, Root: root}
	got := l.Projects([]string{filepath.Join(root, "compose.yaml"), filepath.Join(root, "shop", "compose-sudoku.yaml")})
	if !got[0].Infra || got[0].Name != InfraName || got[1].Name != "shop" || got[1].Rel != "shop/compose-sudoku.yaml" {
		t.Fatalf("%+v", got)
	}

	home := t.TempDir()
	l = Layout{Data: home, Root: root}
	got = l.Projects([]string{filepath.Join(home, "compose.yaml"), filepath.Join(root, "shop", "compose-sudoku.yaml")})
	if !got[0].Infra || got[0].Rel != "compose.yaml" || got[1].Infra || got[1].Rel != "shop/compose-sudoku.yaml" {
		t.Fatalf("separate home: %+v", got)
	}
}

func TestSetsStore(t *testing.T) {
	root := t.TempDir()
	s := NewSetsStore(Layout{Data: root, Root: root})
	if _, err := s.Upsert(Set{Name: "bad name"}); !errors.Is(err, ErrBadSetName) {
		t.Fatalf("bad name accepted: %v", err)
	}
	if _, err := s.Upsert(Set{Name: "backend", Targets: []string{" api ", "api", "infra"}, Description: "a\nb"}); err != nil {
		t.Fatal(err)
	}
	got, err := NewSetsStore(Layout{Data: root, Root: root}).Get("backend")
	if err != nil || !reflect.DeepEqual(got.Targets, []string{"api", "infra"}) || got.Description != "a b" {
		t.Fatalf("reload: %+v %v", got, err)
	}
	exp, err := s.Expand([]string{"@backend"})
	if err != nil || len(exp) != 2 {
		t.Fatalf("expand: %v %v", exp, err)
	}
	if err := s.Delete("backend"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("backend"); !errors.Is(err, ErrSetNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestDiscoverSeparateHome(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	for _, p := range []string{
		filepath.Join(home, "compose.yaml"),
		filepath.Join(root, "compose.yaml"),
		filepath.Join(root, "shop", "compose-sudoku.yaml"),
		filepath.Join(root, "old_noscan", "compose-sudoku.yaml"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("services: {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Layout{Data: home, Root: root}.Discover()
	want := []string{filepath.Join(home, "compose.yaml"), filepath.Join(root, "shop", "compose-sudoku.yaml")}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v %v, want %v", got, err, want)
	}
	if _, err := (Layout{Data: t.TempDir()}).Discover(); !errors.Is(err, ErrRootNotSet) {
		t.Fatalf("no infra and no root: %v", err)
	}
}
