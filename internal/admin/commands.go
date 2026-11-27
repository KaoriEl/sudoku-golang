package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const (
	flagFile = "--file"
	cmdBuild = "build"
	cmdLogs  = "logs"
)

type CommandItem struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Command string `json:"command"`
}

type BuildInfo struct {
	HasBuild   bool              `json:"hasBuild"`
	Image      string            `json:"image"`
	Context    string            `json:"context"`
	Dockerfile string            `json:"dockerfile"`
	Target     string            `json:"target"`
	Args       map[string]string `json:"args"`
	Inline     bool              `json:"inline"`
	Command    string            `json:"command"`
}

type ContainerCommands struct {
	Container   string        `json:"container"`
	Service     string        `json:"service"`
	ComposeFile string        `json:"composeFile"`
	Commands    []CommandItem `json:"commands"`
	Build       *BuildInfo    `json:"build"`
	BuildError  string        `json:"buildError"`
}

type composeConfig struct {
	Name     string                          `json:"name"`
	Services map[string]composeConfigService `json:"services"`
}

type composeConfigService struct {
	Image string              `json:"image"`
	Build *composeConfigBuild `json:"build"`
}

type composeConfigBuild struct {
	Context          string             `json:"context"`
	Dockerfile       string             `json:"dockerfile"`
	DockerfileInline string             `json:"dockerfile_inline"` //nolint:tagliatelle // docker compose config schema
	Target           string             `json:"target"`
	Args             map[string]*string `json:"args"`
}

func (s *Service) buildInfo(ctx context.Context, file, service string) (*BuildInfo, error) {
	raw, err := s.configJSON(ctx, file)
	if err != nil {
		return nil, fmt.Errorf("compose config: %w", err)
	}
	var cfg composeConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("decode compose config: %w", err)
	}
	svc, ok := cfg.Services[service]
	if !ok {
		return nil, fmt.Errorf("service %q not found in compose config", service)
	}

	info := &BuildInfo{Image: svc.Image, Args: map[string]string{}}
	if svc.Build == nil {
		info.Command = shellJoin(append(displayArgv(s.docker.DockerArgv()), "pull", svc.Image))
		return info, nil
	}

	b := svc.Build
	info.HasBuild = true
	info.Context = b.Context
	info.Target = b.Target
	info.Inline = b.DockerfileInline != ""
	if info.Image == "" {
		info.Image = cfg.Name + "-" + service
	}
	dockerfile := b.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	if !filepath.IsAbs(dockerfile) {
		dockerfile = filepath.Join(b.Context, dockerfile)
	}
	info.Dockerfile = dockerfile

	args := append(displayArgv(s.docker.DockerArgv()), cmdBuild, "-t", info.Image)
	if info.Inline {
		args = append(args, "-f", "-")
	} else {
		args = append(args, "-f", dockerfile)
	}
	if b.Target != "" {
		args = append(args, "--target", b.Target)
	}
	keys := make([]string, 0, len(b.Args))
	for k := range b.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v := b.Args[k]; v != nil {
			info.Args[k] = *v
			args = append(args, "--build-arg", k+"="+*v)
		} else {
			args = append(args, "--build-arg", k)
		}
	}
	args = append(args, b.Context)
	info.Command = shellJoin(args)
	return info, nil
}

func (s *Service) ContainerCommands(ctx context.Context, id string) (ContainerCommands, error) {
	c, err := s.FindContainer(ctx, id)
	if err != nil {
		return ContainerCommands{}, err
	}
	compose := func(args ...string) string {
		return s.displayCompose(append([]string{flagFile, c.ComposeFile}, args...))
	}
	docker := func(args ...string) string {
		return shellJoin(append(displayArgv(s.docker.DockerArgv()), args...))
	}

	out := ContainerCommands{
		Container:   c.Name,
		Service:     c.Service,
		ComposeFile: c.ComposeFile,
		Commands: []CommandItem{
			{ID: cmdBuild, Label: cmdBuild, Command: compose(cmdBuild, c.Service)},
			{ID: "build-no-cache", Label: "build --no-cache", Command: compose(cmdBuild, "--no-cache", c.Service)},
			{ID: "rebuild", Label: "up --build", Command: compose("up", "-d", "--build", "--force-recreate", "--no-deps", c.Service)},
			{ID: "up", Label: "up", Command: compose("up", "-d", c.Service)},
			{ID: cmdLogs, Label: cmdLogs, Command: docker(cmdLogs, "-f", "--tail", "200", c.Name)},
			{ID: "exec", Label: "exec", Command: docker("exec", "-it", c.Name, "sh")},
		},
	}

	info, err := s.buildInfo(ctx, c.ComposeFile, c.Service)
	if err != nil {
		out.BuildError = err.Error()
	} else {
		out.Build = info
	}
	return out, nil
}

func (s *Service) RebuildService(ctx context.Context, id string, noCache bool) (*Run, error) {
	c, err := s.FindContainer(ctx, id)
	if err != nil {
		return nil, err
	}
	if !c.Managed {
		return nil, ErrNotManaged
	}
	files, _, err := s.composeFiles()
	if err != nil {
		return nil, err
	}
	var file ComposeFile
	for _, f := range files {
		if f.Path == c.ComposeFile {
			file = f
		}
	}

	first := stepPlan{file: file, phase: cmdBuild, args: []string{cmdBuild, c.Service}}
	if noCache {
		first.args = []string{cmdBuild, "--no-cache", c.Service}
	}
	if info, infoErr := s.buildInfo(ctx, c.ComposeFile, c.Service); infoErr == nil && !info.HasBuild {
		first = stepPlan{file: file, phase: "pull", args: []string{"pull", c.Service}}
	}

	plan := runPlan{
		action: runServiceRebuild,
		target: strings.TrimSpace(c.Service + " · " + c.Name),
		steps: []stepPlan{
			first,
			{file: file, phase: "up", args: []string{"up", "-d", "--force-recreate", "--no-deps", c.Service}},
		},
	}
	s.updates.Forget(c.Image) // the pull below makes the cached answer stale
	return s.startRun(plan, true)
}
