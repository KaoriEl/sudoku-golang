package admin

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sudoku-golang/internal/composeedit"
	"time"
)

const (
	traefikConfigTarget = "/etc/traefik/traefik.yaml"
	protoTCP            = "tcp"
)

var (
	ErrNoInfraCompose = errors.New("root compose.yaml not found in ROOT_PROJECTS_FOLDER")
	ErrNoTraefik      = errors.New("traefik service not found in root compose.yaml")
	ErrInvalidPort    = errors.New("invalid port")
	ErrPortConflict   = errors.New("port conflict")
	ErrStale          = errors.New("file changed since it was read, reload the page")
)

type Consumer struct {
	Container string `json:"container"`
	Router    string `json:"router"`
	Kind      string `json:"kind"`
}

type PortRow struct {
	composeedit.Port
	Live       bool       `json:"live"`
	EntryPoint string     `json:"entryPoint"`
	Consumers  []Consumer `json:"consumers"`
}

type EntryPointRow struct {
	composeedit.EntryPoint
	Published bool       `json:"published"`
	Consumers []Consumer `json:"consumers"`
}

type PortsView struct {
	ComposeFile        string          `json:"composeFile"`
	TraefikConfig      string          `json:"traefikConfig"`
	TraefikConfigFound bool            `json:"traefikConfigFound"`
	Service            string          `json:"service"`
	Container          *Container      `json:"container"`
	Ports              []PortRow       `json:"ports"`
	EntryPoints        []EntryPointRow `json:"entryPoints"`
	NeedsApply         bool            `json:"needsApply"`
	ApplyReasons       []string        `json:"applyReasons"`
	Warnings           []string        `json:"warnings"`
}

type AddPortRequest struct {
	HostIP        string `json:"hostIp"`
	HostPort      int    `json:"hostPort"`
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol"`
	Comment       string `json:"comment"`
	EntryPoint    string `json:"entryPoint"`
}

type RemovePortRequest struct {
	Index            int    `json:"index"`
	Raw              string `json:"raw"`
	RemoveEntryPoint bool   `json:"removeEntryPoint"`
}

type infraFiles struct {
	compose      ComposeFile
	traefikCfg   string
	traefikFound bool
	service      string
	composeData  []byte
	traefikData  []byte
}

func (s *Service) infra() (infraFiles, error) {
	files, _, err := s.composeFiles()
	if err != nil {
		return infraFiles{}, err
	}
	var out infraFiles
	found := false
	for _, f := range files {
		if f.Infra {
			out.compose, found = f, true
		}
	}
	if !found {
		return infraFiles{}, ErrNoInfraCompose
	}

	out.composeData, err = os.ReadFile(out.compose.Path)
	if err != nil {
		return infraFiles{}, fmt.Errorf("read %s: %w", out.compose.Path, err)
	}

	names, err := composeedit.ServiceNames(out.composeData)
	if err != nil {
		return infraFiles{}, fmt.Errorf("parse %s: %w", out.compose.Path, err)
	}
	for _, n := range names {
		if n == "traefik" || strings.HasPrefix(composeedit.ServiceImage(out.composeData, n), "traefik") {
			out.service = n
			break
		}
	}
	if out.service == "" {
		return infraFiles{}, ErrNoTraefik
	}

	dir := filepath.Dir(out.compose.Path)
	out.traefikCfg = filepath.Join(dir, "sudoku-config", "traefik", "traefik.yaml")
	if src, ok := composeedit.ServiceVolumeSource(out.composeData, out.service, traefikConfigTarget); ok {
		if !filepath.IsAbs(src) {
			src = filepath.Join(dir, src)
		}
		out.traefikCfg = filepath.Clean(src)
	}
	if data, readErr := os.ReadFile(out.traefikCfg); readErr == nil { //nolint:gosec // operator's own config file
		out.traefikData = data
		out.traefikFound = true
	}
	return out, nil
}

func consumersByEntryPoint(containers []Container) map[string][]Consumer {
	out := map[string][]Consumer{}
	for _, c := range containers {
		for _, r := range c.Routers {
			for _, ep := range r.EntryPoints {
				out[ep] = append(out[ep], Consumer{Container: c.Name, Router: r.Name, Kind: r.Kind})
			}
		}
	}
	return out
}

func portKey(port int, proto string) string {
	return fmt.Sprintf("%d/%s", port, proto)
}

func findService(containers []Container, file, service string) *Container {
	for i := range containers {
		if containers[i].ComposeFile == file && containers[i].Service == service {
			c := containers[i]
			return &c
		}
	}
	return nil
}

func livePorts(traefik *Container) map[string]bool {
	live := map[string]bool{}
	if traefik == nil || traefik.State != dockerRunning {
		return live
	}
	for _, p := range traefik.Ports {
		if p.HostPort > 0 {
			live[portKey(p.HostPort, p.Protocol)] = true
		}
	}
	return live
}

func (v *PortsView) addPortRows(ports []composeedit.Port, eps []composeedit.EntryPoint, consumers map[string][]Consumer, live map[string]bool) {
	epByPort := map[string]composeedit.EntryPoint{}
	for _, ep := range eps {
		epByPort[portKey(ep.Port, ep.Protocol)] = ep
	}
	configured := map[string]bool{}
	for _, p := range ports {
		row := PortRow{Port: p, Consumers: []Consumer{}}
		if p.Parsed && p.HostPort > 0 {
			key := portKey(p.HostPort, p.Protocol)
			configured[key] = true
			row.Live = live[key]
			if ep, ok := epByPort[portKey(p.ContainerPort, p.Protocol)]; ok {
				row.EntryPoint = ep.Name
				row.Consumers = append(row.Consumers, consumers[ep.Name]...)
			}
			if !row.Live && v.Container != nil && v.Container.State == dockerRunning {
				v.ApplyReasons = append(v.ApplyReasons, fmt.Sprintf("порт %d ещё не опубликован", p.HostPort))
			}
		}
		v.Ports = append(v.Ports, row)
	}
	for key := range live {
		if !configured[key] {
			v.ApplyReasons = append(v.ApplyReasons, fmt.Sprintf("порт %s удалён из конфига, но ещё опубликован", key))
		}
	}
}

func (v *PortsView) addEntryPointRows(ports []composeedit.Port, eps []composeedit.EntryPoint, consumers map[string][]Consumer) {
	published := map[string]bool{}
	for _, p := range ports {
		if p.Parsed {
			published[portKey(p.ContainerPort, p.Protocol)] = true
		}
	}
	for _, ep := range eps {
		cs := consumers[ep.Name]
		if cs == nil {
			cs = []Consumer{}
		}
		v.EntryPoints = append(v.EntryPoints, EntryPointRow{EntryPoint: ep, Published: published[portKey(ep.Port, ep.Protocol)], Consumers: cs})
	}
}

func (v *PortsView) checkConfigAge(inf infraFiles) {
	if v.Container == nil {
		v.ApplyReasons = append(v.ApplyReasons, "контейнер Traefik не создан")
		return
	}
	if v.Container.State != dockerRunning {
		v.ApplyReasons = append(v.ApplyReasons, "Traefik не запущен — порты не опубликованы")
		return
	}
	if !inf.traefikFound {
		return
	}
	st, err := os.Stat(inf.traefikCfg)
	if err != nil {
		return
	}
	started, err := time.Parse(time.RFC3339Nano, v.Container.StartedAt)
	if err == nil && st.ModTime().After(started) {
		v.ApplyReasons = append(v.ApplyReasons, "traefik.yaml изменён после запуска Traefik")
	}
}

func (s *Service) Ports(ctx context.Context) (PortsView, error) {
	inf, err := s.infra()
	if err != nil {
		return PortsView{}, err
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return PortsView{}, err
	}
	ports, err := composeedit.ReadPorts(inf.composeData, inf.service)
	if err != nil {
		return PortsView{}, fmt.Errorf("read ports: %w", err)
	}

	view := PortsView{
		ComposeFile:        inf.compose.Path,
		TraefikConfig:      inf.traefikCfg,
		TraefikConfigFound: inf.traefikFound,
		Service:            inf.service,
		Container:          findService(snap.Containers, inf.compose.Path, inf.service),
		Ports:              []PortRow{},
		EntryPoints:        []EntryPointRow{},
		ApplyReasons:       []string{},
		Warnings:           []string{},
	}

	var eps []composeedit.EntryPoint
	if inf.traefikFound {
		if eps, err = composeedit.ReadEntryPoints(inf.traefikData); err != nil {
			view.Warnings = append(view.Warnings, "traefik.yaml: "+err.Error())
		}
	} else {
		view.Warnings = append(view.Warnings, "traefik.yaml не найден: "+inf.traefikCfg)
	}

	consumers := consumersByEntryPoint(snap.Containers)
	view.addPortRows(ports, eps, consumers, livePorts(view.Container))
	view.addEntryPointRows(ports, eps, consumers)
	view.checkConfigAge(inf)
	view.NeedsApply = len(view.ApplyReasons) > 0
	return view, nil
}

func validatePortRequest(req *AddPortRequest) error {
	if req.HostPort < 1 || req.HostPort > 65535 {
		return fmt.Errorf("%w: host port %d", ErrInvalidPort, req.HostPort)
	}
	if req.ContainerPort == 0 {
		req.ContainerPort = req.HostPort
	}
	if req.ContainerPort < 1 || req.ContainerPort > 65535 {
		return fmt.Errorf("%w: container port %d", ErrInvalidPort, req.ContainerPort)
	}
	if req.Protocol == "" {
		req.Protocol = protoTCP
	}
	if req.Protocol != protoTCP && req.Protocol != "udp" {
		return fmt.Errorf("%w: protocol %q", ErrInvalidPort, req.Protocol)
	}
	if req.HostIP != "" && net.ParseIP(req.HostIP) == nil {
		return fmt.Errorf("%w: host ip %q", ErrInvalidPort, req.HostIP)
	}
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	// Edit the real file behind a symlink: renaming over the link would silently replace it with a
	// regular file and detach it from what it pointed to.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	info, err := os.Stat(path)
	mode := fs.FileMode(0o644)
	if err == nil {
		mode = info.Mode().Perm()
		if orig, readErr := os.ReadFile(path); readErr == nil {
			if err := os.WriteFile(path+".sudoku.bak", orig, mode); err != nil { //nolint:gosec // path is the operator's own config file
				return fmt.Errorf("backup %s: %w", path, err)
			}
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sudoku-*.tmp")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func (s *Service) hostPortUsedElsewhere(ctx context.Context, port int, proto, exceptFile, exceptService string) string {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return ""
	}
	for _, c := range snap.Containers {
		if c.ComposeFile == exceptFile && c.Service == exceptService {
			continue
		}
		for _, p := range c.Ports {
			if p.HostPort == port && p.Protocol == proto {
				return c.Name
			}
		}
	}
	return ""
}

func (s *Service) AddPort(ctx context.Context, req AddPortRequest) (PortsView, error) {
	if err := validatePortRequest(&req); err != nil {
		return PortsView{}, err
	}
	s.files.Lock()
	defer s.files.Unlock()

	inf, err := s.infra()
	if err != nil {
		return PortsView{}, err
	}
	existing, err := composeedit.ReadPorts(inf.composeData, inf.service)
	if err != nil {
		return PortsView{}, fmt.Errorf("read ports: %w", err)
	}
	for _, p := range existing {
		if p.Parsed && p.HostPort == req.HostPort && p.Protocol == req.Protocol {
			return PortsView{}, fmt.Errorf("%w: порт %d/%s уже есть в конфиге (%s)", ErrPortConflict, req.HostPort, req.Protocol, p.Raw)
		}
	}
	if owner := s.hostPortUsedElsewhere(ctx, req.HostPort, req.Protocol, inf.compose.Path, inf.service); owner != "" {
		return PortsView{}, fmt.Errorf("%w: порт %d уже опубликован контейнером %s", ErrPortConflict, req.HostPort, owner)
	}

	newCompose, err := composeedit.AddPort(inf.composeData, inf.service, composeedit.NewPort{
		HostIP: req.HostIP, HostPort: req.HostPort, ContainerPort: req.ContainerPort, Protocol: req.Protocol, Comment: req.Comment,
	})
	if err != nil {
		return PortsView{}, fmt.Errorf("edit compose: %w", err)
	}

	var newTraefik []byte
	if name := strings.TrimSpace(req.EntryPoint); name != "" {
		if !inf.traefikFound {
			return PortsView{}, fmt.Errorf("traefik.yaml не найден: %s", inf.traefikCfg)
		}
		eps, readErr := composeedit.ReadEntryPoints(inf.traefikData)
		if readErr != nil {
			return PortsView{}, fmt.Errorf("read entrypoints: %w", readErr)
		}
		for _, ep := range eps {
			if ep.Port == req.ContainerPort && ep.Protocol == req.Protocol {
				return PortsView{}, fmt.Errorf("%w: entryPoint %s уже слушает порт %d", ErrPortConflict, ep.Name, req.ContainerPort)
			}
		}
		newTraefik, err = composeedit.AddEntryPoint(inf.traefikData, name, req.ContainerPort, req.Protocol)
		if err != nil {
			return PortsView{}, fmt.Errorf("edit traefik.yaml: %w", err)
		}
	}

	if err := writeFileAtomic(inf.compose.Path, newCompose); err != nil {
		return PortsView{}, err
	}
	if newTraefik != nil {
		if err := writeFileAtomic(inf.traefikCfg, newTraefik); err != nil {
			_ = writeFileAtomic(inf.compose.Path, inf.composeData)
			return PortsView{}, err
		}
	}
	s.log.Info("port added", "port", req.HostPort, "entryPoint", req.EntryPoint)
	return s.Ports(ctx)
}

func (s *Service) RemovePort(ctx context.Context, req RemovePortRequest) (PortsView, error) {
	s.files.Lock()
	defer s.files.Unlock()

	inf, err := s.infra()
	if err != nil {
		return PortsView{}, err
	}
	ports, err := composeedit.ReadPorts(inf.composeData, inf.service)
	if err != nil {
		return PortsView{}, fmt.Errorf("read ports: %w", err)
	}
	if req.Index < 0 || req.Index >= len(ports) || ports[req.Index].Raw != req.Raw {
		return PortsView{}, ErrStale
	}
	target := ports[req.Index]

	newCompose, err := composeedit.RemovePort(inf.composeData, inf.service, req.Index, req.Raw)
	if err != nil {
		return PortsView{}, fmt.Errorf("edit compose: %w", err)
	}

	var newTraefik []byte
	if req.RemoveEntryPoint && inf.traefikFound && target.Parsed {
		newTraefik, err = s.dropEntryPoint(ctx, inf, target)
		if err != nil {
			return PortsView{}, err
		}
	}

	if err := writeFileAtomic(inf.compose.Path, newCompose); err != nil {
		return PortsView{}, err
	}
	if newTraefik != nil {
		if err := writeFileAtomic(inf.traefikCfg, newTraefik); err != nil {
			_ = writeFileAtomic(inf.compose.Path, inf.composeData)
			return PortsView{}, err
		}
	}
	s.log.Info("port removed", "raw", req.Raw)
	return s.Ports(ctx)
}

func (s *Service) dropEntryPoint(ctx context.Context, inf infraFiles, target composeedit.Port) ([]byte, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	consumers := consumersByEntryPoint(snap.Containers)
	eps, err := composeedit.ReadEntryPoints(inf.traefikData)
	if err != nil {
		return nil, fmt.Errorf("read entrypoints: %w", err)
	}
	for _, ep := range eps {
		if ep.Port != target.ContainerPort || ep.Protocol != target.Protocol {
			continue
		}
		if len(consumers[ep.Name]) > 0 {
			return nil, fmt.Errorf("%w: entryPoint %s используется (%s)", ErrPortConflict, ep.Name, consumers[ep.Name][0].Container)
		}
		out, err := composeedit.RemoveEntryPoint(inf.traefikData, ep.Name)
		if err != nil {
			return nil, fmt.Errorf("edit traefik.yaml: %w", err)
		}
		return out, nil
	}
	return nil, nil
}

func (s *Service) ApplyPorts() (*Run, error) {
	inf, err := s.infra()
	if err != nil {
		return nil, err
	}
	plan := runPlan{
		action: runApplyPorts,
		target: inf.service,
		steps: []stepPlan{
			{file: inf.compose, phase: "up", args: []string{"up", "-d", "--force-recreate", "--no-deps", inf.service}},
		},
	}
	return s.startRun(plan, true)
}
