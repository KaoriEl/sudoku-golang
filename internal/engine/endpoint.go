package engine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sudoku-golang/internal/sysproc"
	"time"
)

const (
	SchemeUnix  = "unix"
	SchemeNpipe = "npipe"
	SchemeTCP   = "tcp"

	defaultUnixSocket = "/var/run/docker.sock"
	defaultNamedPipe  = `\\.\pipe\docker_engine`
	resolveTimeout    = 5 * time.Second
)

var ErrUnsupportedEndpoint = errors.New("unsupported docker endpoint")

// Endpoint is where the Docker Engine API listens: a unix socket (Linux, macOS, Colima, OrbStack,
// Docker Desktop), a Windows named pipe (Docker Desktop on Windows) or TCP, optionally with TLS.
type Endpoint struct {
	Scheme  string
	Address string
	TLS     *tls.Config
}

func UnixEndpoint(path string) Endpoint {
	return Endpoint{Scheme: SchemeUnix, Address: path}
}

func (e Endpoint) String() string {
	switch e.Scheme {
	case SchemeUnix:
		return "unix://" + e.Address
	case SchemeNpipe:
		return "npipe://" + strings.ReplaceAll(e.Address, `\`, "/")
	default:
		if e.TLS != nil {
			return "tcp://" + e.Address + " (tls)"
		}
		return "tcp://" + e.Address
	}
}

// DefaultEndpoint is the platform's standard engine address.
func DefaultEndpoint() Endpoint {
	if runtime.GOOS == "windows" {
		return Endpoint{Scheme: SchemeNpipe, Address: defaultNamedPipe}
	}
	return UnixEndpoint(defaultUnixSocket)
}

// ParseHost parses a DOCKER_HOST style address: unix:///path, npipe:////./pipe/name, tcp://host:port.
func ParseHost(host string) (Endpoint, error) {
	host = strings.TrimSpace(host)
	switch {
	case strings.HasPrefix(host, "unix://"):
		return UnixEndpoint(strings.TrimPrefix(host, "unix://")), nil
	case strings.HasPrefix(host, "npipe://"):
		p := strings.TrimPrefix(host, "npipe://")
		return Endpoint{Scheme: SchemeNpipe, Address: strings.ReplaceAll(p, "/", `\`)}, nil
	case strings.HasPrefix(host, "tcp://"), strings.HasPrefix(host, "http://"), strings.HasPrefix(host, "https://"):
		u, err := url.Parse(host)
		if err != nil || u.Host == "" {
			return Endpoint{}, fmt.Errorf("%w: %q", ErrUnsupportedEndpoint, host)
		}
		ep := Endpoint{Scheme: SchemeTCP, Address: u.Host}
		if u.Scheme == "https" {
			ep.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		return ep, nil
	}
	return Endpoint{}, fmt.Errorf("%w: %q", ErrUnsupportedEndpoint, host)
}

func lookupEnv(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(env[i], "="); ok && k == key {
			return v
		}
	}
	return ""
}

// tlsFromEnv builds the client TLS config the docker CLI would use for a TCP host
// (DOCKER_TLS_VERIFY / DOCKER_CERT_PATH with ca.pem, cert.pem, key.pem).
func tlsFromEnv(env []string) (*tls.Config, error) {
	verify := lookupEnv(env, "DOCKER_TLS_VERIFY")
	certPath := lookupEnv(env, "DOCKER_CERT_PATH")
	if verify == "" && certPath == "" && lookupEnv(env, "DOCKER_TLS") == "" {
		return nil, nil
	}
	if certPath == "" {
		home, _ := os.UserHomeDir()
		certPath = filepath.Join(home, ".docker")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if cert, err := tls.LoadX509KeyPair(filepath.Join(certPath, "cert.pem"), filepath.Join(certPath, "key.pem")); err == nil {
		cfg.Certificates = []tls.Certificate{cert}
	}
	if verify != "" && verify != "0" {
		ca, err := os.ReadFile(filepath.Join(certPath, "ca.pem"))
		if err != nil {
			return nil, fmt.Errorf("docker tls ca: %w", err)
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(ca)
		cfg.RootCAs = pool
	} else {
		// DOCKER_TLS without DOCKER_TLS_VERIFY means exactly this for the docker CLI.
		cfg.InsecureSkipVerify = true
	}
	return cfg, nil
}

// ResolveEndpoint finds the engine the docker CLI talks to: DOCKER_HOST, then the current docker
// context (Colima, OrbStack, Docker Desktop and rootless setups all live there), then the platform default.
func ResolveEndpoint(ctx context.Context, dockerArgv []string, env []string) Endpoint {
	withTLS := func(ep Endpoint) Endpoint {
		if ep.Scheme == SchemeTCP && ep.TLS == nil {
			if cfg, err := tlsFromEnv(env); err == nil && cfg != nil {
				ep.TLS = cfg
			}
		}
		return ep
	}
	if h := lookupEnv(env, "DOCKER_HOST"); h != "" {
		if ep, err := ParseHost(h); err == nil {
			return withTLS(ep)
		}
	}
	if len(dockerArgv) > 0 {
		ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
		defer cancel()
		args := append(append([]string{}, dockerArgv[1:]...), "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
		cmd := exec.CommandContext(ctx, dockerArgv[0], args...) //nolint:gosec // operator-configured docker binary
		cmd.Env = env
		sysproc.HideWindow(cmd)
		if out, err := cmd.Output(); err == nil {
			if ep, err := ParseHost(strings.TrimSpace(string(out))); err == nil {
				return withTLS(ep)
			}
		}
	}
	return DefaultEndpoint()
}

func (e Endpoint) dial(ctx context.Context) (net.Conn, error) {
	conn, err := e.dialRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", e, err)
	}
	return conn, nil
}

func (e Endpoint) dialRaw(ctx context.Context) (net.Conn, error) {
	switch e.Scheme {
	case SchemeUnix:
		var d net.Dialer
		return d.DialContext(ctx, "unix", e.Address) //nolint:wrapcheck // wrapped by dial
	case SchemeNpipe:
		return dialPipe(ctx, e.Address)
	case SchemeTCP:
		if e.TLS != nil {
			d := tls.Dialer{Config: e.TLS}
			return d.DialContext(ctx, "tcp", e.Address) //nolint:wrapcheck // wrapped by dial
		}
		var d net.Dialer
		return d.DialContext(ctx, "tcp", e.Address) //nolint:wrapcheck // wrapped by dial
	}
	return nil, fmt.Errorf("%w: %s", ErrUnsupportedEndpoint, e.Scheme)
}
