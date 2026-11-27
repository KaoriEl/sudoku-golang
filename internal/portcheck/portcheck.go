// Package portcheck finds host ports that a compose file wants to publish but something else already
// holds, and tells who holds them: another compose container or an OS process.
package portcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/health"
)

const (
	ProtoUDP      = "udp"
	KindContainer = "container"
	KindProcess   = "process"
	KindUnknown   = "unknown"

	labelService = "com.docker.compose.service"
)

// Binding is one published host port of a compose service.
type Binding struct {
	File    string `json:"file"`
	Service string `json:"service"`
	HostIP  string `json:"hostIp"`
	Port    int    `json:"port"`
	Proto   string `json:"proto"`
}

// Conflict is a binding whose port is taken by someone other than the service itself.
type Conflict struct {
	Binding
	Kind        string `json:"kind"`
	Owner       string `json:"owner"`
	ContainerID string `json:"containerId,omitempty"`
	PID         int    `json:"pid,omitempty"`
}

func (c Conflict) String() string {
	return fmt.Sprintf("%s: порт %d/%s занят — %s", c.Service, c.Port, c.Proto, c.Owner)
}

type composePort struct {
	Target    int    `json:"target"`
	Published string `json:"published"`
	Protocol  string `json:"protocol"`
	HostIP    string `json:"host_ip"` //nolint:tagliatelle // docker compose config schema
}

// Published extracts the published host ports from `compose config --format json` output, optionally
// only for some services.
func Published(configJSON []byte, file string, services []string) ([]Binding, error) {
	var cfg struct {
		Services map[string]struct {
			Ports []composePort `json:"ports"`
		} `json:"services"`
	}
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, fmt.Errorf("decode compose config: %w", err)
	}
	only := map[string]bool{}
	for _, s := range services {
		only[s] = true
	}
	var out []Binding
	for name, svc := range cfg.Services {
		if len(only) > 0 && !only[name] {
			continue
		}
		for _, p := range svc.Ports {
			lo, hi, ok := portRange(p.Published)
			if !ok {
				continue
			}
			proto := p.Protocol
			if proto == "" {
				proto = "tcp"
			}
			for port := lo; port <= hi; port++ {
				out = append(out, Binding{File: file, Service: name, HostIP: p.HostIP, Port: port, Proto: proto})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Service < out[j].Service
	})
	return out, nil
}

func portRange(s string) (int, int, bool) {
	lo, hi, isRange := strings.Cut(strings.TrimSpace(s), "-")
	a, err := strconv.Atoi(lo)
	if err != nil || a <= 0 || a > 65535 {
		return 0, 0, false
	}
	if !isRange {
		return a, a, true
	}
	b, err := strconv.Atoi(hi)
	if err != nil || b < a || b > 65535 || b-a > 1000 {
		return 0, 0, false
	}
	return a, b, true
}

// Free reports whether the port can be bound on the host. Both the wildcard and loopback are tried:
// with SO_REUSEADDR (macOS/BSD) binding 0.0.0.0 can succeed while 127.0.0.1:port is taken.
func Free(hostIP string, port int, proto string) bool {
	addrs := []string{"0.0.0.0", "127.0.0.1"}
	if hostIP != "" && hostIP != "0.0.0.0" && hostIP != "::" {
		addrs = []string{hostIP}
	}
	var lc net.ListenConfig
	for _, a := range addrs {
		addr := net.JoinHostPort(a, strconv.Itoa(port))
		if proto == ProtoUDP {
			c, err := lc.ListenPacket(context.Background(), "udp", addr)
			if err != nil {
				return false
			}
			_ = c.Close()
			continue
		}
		l, err := lc.Listen(context.Background(), "tcp", addr)
		if err != nil {
			return false
		}
		_ = l.Close()
	}
	return true
}

func publishes(c docker.Container, port int, proto string) bool {
	for spec, bindings := range c.NetworkSettings.Ports {
		if !strings.HasSuffix(spec, "/"+proto) {
			continue
		}
		for _, b := range bindings {
			if b.HostPort == strconv.Itoa(port) {
				return true
			}
		}
	}
	return false
}

// Check returns the bindings whose port is busy. A port held by the very service being started is
// not a conflict (it is already up, or compose recreates it). running lists the current containers.
func Check(ctx context.Context, bindings []Binding, running []docker.Container) []Conflict {
	var out []Conflict
	for _, b := range bindings {
		var holder *docker.Container
		for i := range running {
			if running[i].State.Running && publishes(running[i], b.Port, b.Proto) {
				holder = &running[i]
				break
			}
		}
		if holder != nil {
			if health.BelongsTo(*holder, b.File) && holder.Config.Labels[labelService] == b.Service {
				continue
			}
			out = append(out, Conflict{
				Binding: b, Kind: KindContainer, Owner: "контейнер " + strings.TrimPrefix(holder.Name, "/"), ContainerID: holder.ID,
			})
			continue
		}
		if Free(b.HostIP, b.Port, b.Proto) {
			continue
		}
		c := Conflict{Binding: b, Kind: KindUnknown, Owner: "неизвестный процесс"}
		if name, pid, ok := Owner(ctx, b.Port, b.Proto); ok {
			c.Kind, c.Owner, c.PID = KindProcess, fmt.Sprintf("%s (pid %d)", name, pid), pid
		}
		out = append(out, c)
	}
	return out
}
