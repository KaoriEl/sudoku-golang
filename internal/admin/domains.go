package admin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sudoku-golang/internal/composeedit"
	"sudoku-golang/internal/domains"
	"time"
)

const (
	traefikCustomTarget = "/etc/traefik/custom"
	certTimeout         = 2 * time.Minute
)

type DomainsView struct {
	HostsFile string             `json:"hostsFile"`
	Hosts     []domains.Host     `json:"hosts"`
	Block     []string           `json:"block"`
	Pending   []string           `json:"pending"`
	TLS       domains.CertStatus `json:"tls"`
	Command   string             `json:"command"`
	Staged    string             `json:"staged"`
	Written   bool               `json:"written"`
}

// traefikCustomDir is the host folder mounted at /etc/traefik/custom (Traefik's file provider).
func (s *Service) traefikCustomDir() string {
	dir := filepath.Join(s.layout.ConfigDir(), "traefik", "custom")
	inf, err := s.infra()
	if err != nil {
		return dir
	}
	if src, ok := composeedit.ServiceVolumeSource(inf.composeData, inf.service, traefikCustomTarget); ok {
		if !filepath.IsAbs(src) {
			src = filepath.Join(filepath.Dir(inf.compose.Path), src)
		}
		return filepath.Clean(src)
	}
	return dir
}

func (s *Service) stagedHosts() string {
	return filepath.Join(s.layout.ConfigDir(), "hosts.sudoku")
}

// hostNames maps every Traefik Host() of a managed container to the containers that serve it.
func (s *Service) hostNames(ctx context.Context) (map[string][]string, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string][]string{}
	for _, c := range snap.Containers {
		if !c.Managed {
			continue
		}
		for _, h := range c.Hosts {
			names[h] = append(names[h], c.Name)
		}
	}
	return names, nil
}

func (s *Service) Domains(ctx context.Context) (DomainsView, error) {
	names, err := s.hostNames(ctx)
	if err != nil {
		return DomainsView{}, err
	}
	hosts := domains.Lookup(ctx, names)
	v := DomainsView{HostsFile: domains.HostsFile(), Hosts: hosts, Block: []string{}, Pending: []string{}}
	if raw, err := readFileLimited(v.HostsFile); err == nil {
		v.Block = domains.ReadBlock(raw)
	}
	inBlock := map[string]bool{}
	for _, b := range v.Block {
		inBlock[b] = true
	}
	all := make([]string, 0, len(hosts))
	for _, h := range hosts {
		all = append(all, h.Name)
	}
	dir := s.traefikCustomDir()
	covered := domains.Covered(dir, all)
	for i := range v.Hosts {
		h := &v.Hosts[i]
		h.InBlock = inBlock[h.Name]
		h.InCert = covered[h.Name]
		if !h.AutoLocal && !h.Local && !h.InBlock {
			v.Pending = append(v.Pending, h.Name)
		}
	}
	v.TLS = domains.Status(ctx, dir, all)
	return v, nil
}

// wantedHosts is the hosts block content: every non-.localhost domain served by a managed container.
func wantedHosts(hosts []domains.Host) []string {
	var out []string
	for _, h := range hosts {
		if !h.AutoLocal {
			out = append(out, h.Name)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Service) WriteHosts(ctx context.Context) (DomainsView, error) {
	names, err := s.hostNames(ctx)
	if err != nil {
		return DomainsView{}, err
	}
	hosts := make([]domains.Host, 0, len(names))
	for n := range names {
		hosts = append(hosts, domains.Host{Name: n, AutoLocal: strings.HasSuffix(n, ".localhost") || n == "localhost"})
	}
	cmd, werr := domains.Write(wantedHosts(hosts), s.stagedHosts())
	if werr != nil && !errors.Is(werr, domains.ErrNeedsElevation) {
		return DomainsView{}, werr //nolint:wrapcheck // descriptive
	}
	v, err := s.Domains(ctx)
	if err != nil {
		return DomainsView{}, err
	}
	if cmd != "" {
		v.Command, v.Staged = cmd, s.stagedHosts()
	} else {
		v.Written = true
	}
	s.log.Info("hosts block updated", "elevation", cmd != "")
	return v, nil
}

func (s *Service) IssueCert(ctx context.Context) (DomainsView, string, error) {
	names, err := s.hostNames(ctx)
	if err != nil {
		return DomainsView{}, "", err
	}
	hosts := make([]string, 0, len(names))
	for n := range names {
		hosts = append(hosts, n)
	}
	out, err := domains.Issue(ctx, s.traefikCustomDir(), traefikCustomTarget, hosts)
	if errors.Is(err, domains.ErrNoMkcert) {
		return DomainsView{}, "", &userError{msg: err.Error()}
	}
	if err != nil {
		return DomainsView{}, out, err //nolint:wrapcheck // descriptive
	}
	s.log.Info("tls certificate issued", "hosts", len(hosts))
	v, err := s.Domains(ctx)
	return v, out, err
}

func (s *Server) handleDomains(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	v, err := s.svc.Domains(ctx)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleWriteHosts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	v, err := s.svc.WriteHosts(ctx)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleIssueCert(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), certTimeout)
	defer cancel()
	v, out, err := s.svc.IssueCert(ctx)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": v, "output": out})
}

const maxHostsFile = 4 << 20

func readFileLimited(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err //nolint:wrapcheck // caller ignores
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxHostsFile))
	return string(raw), err
}
