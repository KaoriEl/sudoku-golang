package service

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// InfraName addresses the root compose.yaml in targets ("infra" or "core").
const InfraName = "infra"

var ErrUnknownTarget = errors.New("unknown target")

// Project is one discovered compose file as targets see it.
type Project struct {
	File     string
	Rel      string
	Name     string
	Infra    bool
	Services []string
}

// Entry is one compose file of a selection. Services == nil means "the whole file".
type Entry struct {
	Project  Project
	Services []string
}

// ProjectsFrom turns discovered compose files into projects: the folder name is the project name, the
// root compose.yaml is "infra".
func ProjectsFrom(root string, files []string) []Project {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	out := make([]Project, 0, len(files))
	for _, f := range files {
		rel, err := filepath.Rel(abs, f)
		if err != nil {
			rel = f
		}
		p := Project{File: f, Rel: filepath.ToSlash(rel), Name: filepath.Base(filepath.Dir(f))}
		if filepath.Dir(rel) == "." {
			p.Infra, p.Name = true, InfraName
		}
		out = append(out, p)
	}
	return out
}

func isPattern(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

func match(pattern, name string) bool {
	if !isPattern(pattern) {
		return strings.EqualFold(pattern, name)
	}
	ok, _ := path.Match(strings.ToLower(pattern), strings.ToLower(name))
	return ok
}

// NeedsServices reports whether resolving targets requires each file's service list (a target that
// is not a project name has to be looked up as a service).
func NeedsServices(projects []Project, targets []string) bool {
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if strings.ContainsAny(t, "/:") {
			return true
		}
		if t == "core" {
			continue
		}
		found := false
		for _, p := range projects {
			if match(t, p.Name) {
				found = true
			}
		}
		if !found {
			return true
		}
	}
	return false
}

// Resolve maps CLI-style targets onto compose files and services:
//
//	api            project "api" (its folder), all services
//	infra | core   the root compose.yaml
//	redis          the service "redis" in whatever project defines it
//	api/worker     service "worker" of project "api" (also "api:worker")
//	api-*, */db    shell-style patterns for project and/or service
//
// A project target wins over a service with the same name. The result keeps discovery order (infra
// first). Projects must carry Services when NeedsServices says so.
func Resolve(projects []Project, targets []string) ([]Entry, error) {
	sel := selection{whole: map[string]bool{}, svc: map[string][]string{}}
	var unknown []string
	for _, raw := range targets {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if t == "core" {
			t = InfraName
		}
		if !sel.add(projects, t) {
			unknown = append(unknown, raw)
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("%w: %s (проекты: %s)", ErrUnknownTarget, strings.Join(unknown, ", "), strings.Join(projectNames(projects), ", "))
	}
	return sel.entries(projects), nil
}

type selection struct {
	whole map[string]bool
	svc   map[string][]string
}

// add resolves one target; false means it matched nothing.
func (sel selection) add(projects []Project, t string) bool {
	proj, service, explicit := strings.Cut(strings.ReplaceAll(t, ":", "/"), "/")
	if explicit {
		matched := false
		for _, p := range projects {
			if match(proj, p.Name) && sel.services(p, service) {
				matched = true
			}
		}
		return matched
	}
	matched := false
	for _, p := range projects {
		if match(t, p.Name) {
			sel.whole[p.File], matched = true, true
		}
	}
	if matched {
		return true // a project wins over a service with the same name
	}
	for _, p := range projects {
		if sel.services(p, t) {
			matched = true
		}
	}
	return matched
}

func (sel selection) services(p Project, pattern string) bool {
	matched := false
	for _, s := range p.Services {
		if match(pattern, s) {
			sel.svc[p.File] = append(sel.svc[p.File], s)
			matched = true
		}
	}
	return matched
}

func (sel selection) entries(projects []Project) []Entry {
	var out []Entry
	for _, p := range projects {
		switch {
		case sel.whole[p.File]:
			out = append(out, Entry{Project: p})
		case len(sel.svc[p.File]) > 0:
			list := sel.svc[p.File]
			sort.Strings(list)
			out = append(out, Entry{Project: p, Services: slices.Compact(list)})
		}
	}
	return out
}

func projectNames(projects []Project) []string {
	out := make([]string, 0, len(projects))
	for _, p := range projects {
		out = append(out, p.Name)
	}
	return out
}

// Count is the number of explicitly selected services, with whole files counted as their size.
func Count(entries []Entry) int {
	n := 0
	for _, e := range entries {
		if e.Services == nil {
			n += max(1, len(e.Project.Services))
			continue
		}
		n += len(e.Services)
	}
	return n
}
