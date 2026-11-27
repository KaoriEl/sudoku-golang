package composeedit

import (
	"errors"
	"strings"
	"testing"
)

const localIP = "127.0.0.1"

const compose = `version: "3.9"

services:
  traefik:
    image: traefik:2.11.31
    container_name: work_traefik
    networks:
      - sudoku_network
    ports:
      - "127.0.0.1:8080:8080/tcp"
      - "80:80/tcp"
      - "127.0.0.1:25432:25432/tcp"  # pgsql_space
    restart: unless-stopped
    volumes:
      - ./sudoku-config/traefik/traefik.yaml:/etc/traefik/traefik.yaml:ro,cached

  redis:
    image: eqalpha/keydb:latest
    expose:
      - "6379/tcp"
`

const traefikCfg = `api:
  insecure: true

entryPoints:
  web_80:
    address: ":80/tcp"

  pgsql_space:
    address: ":25432/tcp"

#  mysql_pms:
#    address: ":13307/tcp"

providers:
  docker:
    exposedByDefault: false
`

func TestReadPorts(t *testing.T) {
	ports, err := ReadPorts([]byte(compose), "traefik")
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 3 {
		t.Fatalf("want 3 ports, got %d", len(ports))
	}
	p := ports[2]
	if p.HostIP != localIP || p.HostPort != 25432 || p.ContainerPort != 25432 || p.Protocol != protoTCP || p.Comment != "pgsql_space" {
		t.Fatalf("unexpected port: %+v", p)
	}
	if ports[1].HostIP != "" || ports[1].HostPort != 80 {
		t.Fatalf("unexpected port: %+v", ports[1])
	}
}

func TestAddPortPreservesFile(t *testing.T) {
	out, err := AddPort([]byte(compose), "traefik", NewPort{
		HostIP: localIP, HostPort: 15672, ContainerPort: 15672, Protocol: protoTCP, Comment: "rabbit",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(compose,
		"      - \"127.0.0.1:25432:25432/tcp\"  # pgsql_space\n",
		"      - \"127.0.0.1:25432:25432/tcp\"  # pgsql_space\n      - \"127.0.0.1:15672:15672/tcp\"  # rabbit\n", 1)
	if string(out) != want {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestAddPortCreatesList(t *testing.T) {
	out, err := AddPort([]byte(compose), "redis", NewPort{HostPort: 6379, ContainerPort: 6379})
	if err != nil {
		t.Fatal(err)
	}
	ports, err := ReadPorts(out, "redis")
	if err != nil || len(ports) != 1 || ports[0].HostPort != 6379 {
		t.Fatalf("ports=%+v err=%v\n%s", ports, err, out)
	}
	if !strings.Contains(string(out), "  redis:\n    ports:\n      - \"6379:6379/tcp\"\n    image:") {
		t.Fatalf("unexpected layout:\n%s", out)
	}
}

func TestAddPortFlowStyle(t *testing.T) {
	_, err := AddPort([]byte("services:\n  a:\n    ports: [\"1:1\"]\n"), "a", NewPort{HostPort: 2, ContainerPort: 2})
	if !errors.Is(err, ErrFlowStyle) {
		t.Fatalf("want ErrFlowStyle, got %v", err)
	}
}

func TestRemovePort(t *testing.T) {
	out, err := RemovePort([]byte(compose), "traefik", 1, "80:80/tcp")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), `"80:80/tcp"`) || !strings.Contains(string(out), "# pgsql_space") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	if _, err := RemovePort([]byte(compose), "traefik", 1, "81:81/tcp"); err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestRemoveLastPortDropsKey(t *testing.T) {
	src := "services:\n  a:\n    image: x\n    ports:\n      - \"1:1\"\n    restart: always\n"
	out, err := RemovePort([]byte(src), "a", 0, "1:1")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "services:\n  a:\n    image: x\n    restart: always\n" {
		t.Fatalf("unexpected output:\n%q", out)
	}
}

func TestEntryPoints(t *testing.T) {
	eps, err := ReadEntryPoints([]byte(traefikCfg))
	if err != nil || len(eps) != 2 || eps[1].Name != "pgsql_space" || eps[1].Port != 25432 {
		t.Fatalf("eps=%+v err=%v", eps, err)
	}

	out, err := AddEntryPoint([]byte(traefikCfg), "rabbitmq-admin", 15672, "tcp")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(traefikCfg,
		"  pgsql_space:\n    address: \":25432/tcp\"\n",
		"  pgsql_space:\n    address: \":25432/tcp\"\n\n  rabbitmq-admin:\n    address: \":15672/tcp\"\n", 1)
	if string(out) != want {
		t.Fatalf("unexpected output:\n%s", out)
	}

	if _, err := AddEntryPoint(out, "rabbitmq-admin", 1, "tcp"); !errors.Is(err, ErrEntryPointExists) {
		t.Fatalf("want ErrEntryPointExists, got %v", err)
	}
	if _, err := AddEntryPoint(out, "bad name", 1, "tcp"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("want ErrInvalidName, got %v", err)
	}

	back, err := RemoveEntryPoint(out, "rabbitmq-admin")
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != traefikCfg {
		t.Fatalf("remove did not restore original:\n%s", back)
	}
}

func TestAddEntryPointWithoutSection(t *testing.T) {
	out, err := AddEntryPoint([]byte("api:\n  insecure: true\n"), "web_80", 80, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "api:\n  insecure: true\n\nentryPoints:\n  web_80:\n    address: \":80/tcp\"\n" {
		t.Fatalf("unexpected output:\n%q", out)
	}
}

func TestServiceVolumeSource(t *testing.T) {
	src, ok := ServiceVolumeSource([]byte(compose), "traefik", "/etc/traefik/traefik.yaml")
	if !ok || src != "./sudoku-config/traefik/traefik.yaml" {
		t.Fatalf("got %q %v", src, ok)
	}
}

func TestParsePortSpec(t *testing.T) {
	cases := map[string]Port{
		"8080":                {ContainerPort: 8080, Protocol: protoTCP},
		"80:8080":             {HostPort: 80, ContainerPort: 8080, Protocol: protoTCP},
		"127.0.0.1:53:53/udp": {HostIP: "127.0.0.1", HostPort: 53, ContainerPort: 53, Protocol: "udp"},
		"[::1]:6379:6379/tcp": {HostIP: "::1", HostPort: 6379, ContainerPort: 6379, Protocol: protoTCP},
	}
	for in, want := range cases {
		got, ok := ParsePortSpec(in)
		if !ok || got.HostIP != want.HostIP || got.HostPort != want.HostPort || got.ContainerPort != want.ContainerPort || got.Protocol != want.Protocol {
			t.Errorf("%s: got %+v ok=%v", in, got, ok)
		}
	}
	if _, ok := ParsePortSpec("8000-8010:8000-8010"); ok {
		t.Error("ranges should not parse")
	}
}

func TestCleanCommentStripsLineBreaks(t *testing.T) {
	for _, in := range []string{"a\nb", "a\rb", "a\u0085b", "a b", "a b", "a\tb", "a\x00b"} {
		if got := cleanComment(in); got != "a b" {
			t.Errorf("cleanComment(%q) = %q, want %q", in, got, "a b")
		}
	}
}
