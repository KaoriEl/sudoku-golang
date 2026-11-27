package portcheck

import (
	"context"
	"net"
	"os"
	"runtime"
	"strconv"
	"sudoku-golang/internal/docker"
	"testing"
)

const cfgJSON = `{"services":{
 "db":{"ports":[{"target":5432,"published":"15432","protocol":"tcp","host_ip":"127.0.0.1"}]},
 "web":{"ports":[{"target":80,"published":"8080-8081"},{"target":9000}]},
 "other":{"ports":[{"target":1,"published":"1"}]}}}`

func TestPublished(t *testing.T) {
	got, err := Published([]byte(cfgJSON), "/r/c.yaml", []string{"db", "web"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Port != 8080 || got[1].Port != 8081 || got[2].Port != 15432 || got[2].HostIP != "127.0.0.1" {
		t.Fatalf("%+v", got)
	}
}

func TestCheckFindsBusyPort(t *testing.T) {
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	port := l.Addr().(*net.TCPAddr).Port

	conflicts := Check(context.Background(), []Binding{{File: "/r/c.yaml", Service: "db", HostIP: "127.0.0.1", Port: port, Proto: "tcp"}}, nil)
	if len(conflicts) != 1 || conflicts[0].Kind == KindContainer {
		t.Fatalf("%+v", conflicts)
	}

	self := docker.Container{ID: "x", Name: "/db"}
	self.State.Running = true
	self.Config.Labels = map[string]string{"com.docker.compose.project.config_files": "/r/c.yaml", labelService: "db"}
	self.NetworkSettings.Ports = map[string][]docker.PortBinding{"5432/tcp": {{HostPort: strconv.Itoa(port)}}}
	if c := Check(context.Background(), []Binding{{File: "/r/c.yaml", Service: "db", Port: port, Proto: "tcp"}}, []docker.Container{self}); len(c) != 0 {
		t.Fatalf("own port reported as conflict: %+v", c)
	}
	self.Config.Labels[labelService] = "other"
	c := Check(context.Background(), []Binding{{File: "/r/c.yaml", Service: "db", Port: port, Proto: "tcp"}}, []docker.Container{self})
	if len(c) != 1 || c[0].Kind != KindContainer {
		t.Fatalf("container conflict missed: %+v", c)
	}
}

func TestFreeIgnoresPermissionDenied(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Geteuid() == 0 {
		t.Skip("privileged-port EACCES on loopback is macOS-specific")
	}
	if !Free("", 1, "tcp") {
		t.Fatal("port 1 is unused but reported busy (EACCES on 127.0.0.1 treated as busy)")
	}
	var lc net.ListenConfig
	busy, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	if Free("", busy.Addr().(*net.TCPAddr).Port, "tcp") {
		t.Fatal("port held on 127.0.0.1 reported free")
	}
}
