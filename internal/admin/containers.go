package admin

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/paths"
)

const (
	labelProject     = "com.docker.compose.project"
	labelService     = "com.docker.compose.service"
	labelConfigFiles = "com.docker.compose.project.config_files"

	exitSIGKILL = 137
	exitSIGTERM = 143
)

var (
	hostRuleRe  = regexp.MustCompile(`Host\(([^)]*)\)`)
	backtickRe  = regexp.MustCompile("`([^`]+)`")
	traefikRule = regexp.MustCompile(`^traefik\.http\.routers\.[^.]+\.rule$`)
)

func matchComposeFile(labels map[string]string, known map[string]string) (string, bool) {
	raw := labels[labelConfigFiles]
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if original, ok := known[paths.Normalize(part)]; ok {
			return original, true
		}
	}
	first, _, _ := strings.Cut(raw, ",")
	return strings.TrimSpace(first), false
}

func deriveStatus(s docker.State) Status {
	health := ""
	if s.Health != nil {
		health = s.Health.Status
	}

	switch {
	case s.Restarting:
		return StatusRestarting
	case s.Paused:
		return StatusPaused
	case s.Running && health == "unhealthy":
		return StatusUnhealthy
	case s.Running && health == "starting":
		return StatusStarting
	case s.Running:
		return StatusRunning
	case s.Dead || s.OOMKilled:
		return StatusError
	case s.ExitCode != 0 && s.ExitCode != exitSIGKILL && s.ExitCode != exitSIGTERM:
		return StatusError
	default:
		return StatusStopped
	}
}

func mapPorts(c docker.Container) []Port {
	type key struct {
		cp    int
		proto string
		hp    int
	}
	seen := map[key]bool{}
	var ports []Port

	add := func(spec string, binding *docker.PortBinding) {
		portStr, proto, _ := strings.Cut(spec, "/")
		cp, err := strconv.Atoi(portStr)
		if err != nil {
			return
		}
		p := Port{ContainerPort: cp, Protocol: proto}
		if binding != nil {
			p.HostIP = binding.HostIP
			p.HostPort, _ = strconv.Atoi(binding.HostPort)
		}
		k := key{cp, proto, p.HostPort}
		if seen[k] {
			return
		}
		seen[k] = true
		ports = append(ports, p)
	}

	for spec, bindings := range c.NetworkSettings.Ports {
		if len(bindings) == 0 {
			add(spec, nil)
			continue
		}
		for i := range bindings {
			add(spec, &bindings[i])
		}
	}
	for spec := range c.Config.ExposedPorts {
		if _, ok := c.NetworkSettings.Ports[spec]; !ok {
			add(spec, nil)
		}
	}

	sort.Slice(ports, func(i, j int) bool {
		if ports[i].ContainerPort != ports[j].ContainerPort {
			return ports[i].ContainerPort < ports[j].ContainerPort
		}
		return ports[i].HostPort < ports[j].HostPort
	})
	return ports
}

func mapNetworks(c docker.Container) []Network {
	networks := make([]Network, 0, len(c.NetworkSettings.Networks))
	for name, ep := range c.NetworkSettings.Networks {
		names := ep.DNSNames
		if len(names) == 0 {
			names = ep.Aliases
		}
		networks = append(networks, Network{Name: name, IP: ep.IPAddress, IPv6: ep.GlobalIPv6Address, DNSNames: names})
	}
	sort.Slice(networks, func(i, j int) bool { return networks[i].Name < networks[j].Name })
	return networks
}

func traefikHosts(labels map[string]string) []string {
	set := map[string]bool{}
	for k, v := range labels {
		if !traefikRule.MatchString(k) {
			continue
		}
		for _, rule := range hostRuleRe.FindAllStringSubmatch(v, -1) {
			for _, h := range backtickRe.FindAllStringSubmatch(rule[1], -1) {
				set[h[1]] = true
			}
		}
	}
	hosts := make([]string, 0, len(set))
	for h := range set {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return hosts
}

var routerLabelRe = regexp.MustCompile(`^traefik\.(http|tcp|udp)\.routers\.([^.]+)\.(entrypoints|rule)$`)

func traefikRouters(labels map[string]string) []Router {
	byKey := map[string]*Router{}
	for k, v := range labels {
		m := routerLabelRe.FindStringSubmatch(k)
		if m == nil {
			continue
		}
		key := m[1] + "/" + m[2]
		r, ok := byKey[key]
		if !ok {
			r = &Router{Name: m[2], Kind: m[1]}
			byKey[key] = r
		}
		if m[3] == "rule" {
			r.Rule = v
			continue
		}
		for _, ep := range strings.Split(v, ",") {
			if ep = strings.TrimSpace(ep); ep != "" {
				r.EntryPoints = append(r.EntryPoints, ep)
			}
		}
	}
	routers := make([]Router, 0, len(byKey))
	for _, r := range byKey {
		routers = append(routers, *r)
	}
	sort.Slice(routers, func(i, j int) bool { return routers[i].Name < routers[j].Name })
	return routers
}

func mapContainer(c docker.Container, known map[string]string) Container {
	composeFile, managed := matchComposeFile(c.Config.Labels, known)
	health := ""
	if c.State.Health != nil {
		health = c.State.Health.Status
	}

	return Container{
		ID:          c.ID,
		Name:        strings.TrimPrefix(c.Name, "/"),
		Project:     c.Config.Labels[labelProject],
		Service:     c.Config.Labels[labelService],
		ComposeFile: composeFile,
		Managed:     managed,
		Image:       c.Config.Image,
		State:       c.State.Status,
		Health:      health,
		Status:      deriveStatus(c.State),
		ExitCode:    c.State.ExitCode,
		Error:       c.State.Error,
		StartedAt:   c.State.StartedAt,
		FinishedAt:  c.State.FinishedAt,
		Hostname:    c.Config.Hostname,
		Networks:    mapNetworks(c),
		Ports:       mapPorts(c),
		Hosts:       traefikHosts(c.Config.Labels),
		Routers:     traefikRouters(c.Config.Labels),
	}
}
