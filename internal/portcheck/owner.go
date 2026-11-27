package portcheck

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sudoku-golang/internal/sysproc"
	"time"
)

const ownerTimeout = 3 * time.Second

func output(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, ownerTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	sysproc.HideWindow(cmd)
	return cmd.Output() //nolint:wrapcheck // best effort
}

// Owner names the process listening on port: lsof on macOS/BSD, ss (then lsof) on Linux, netstat +
// tasklist on Windows. Returns false when it cannot tell (tool missing, no permission).
func Owner(ctx context.Context, port int, proto string) (string, int, bool) {
	switch runtime.GOOS {
	case "windows":
		return ownerWindows(ctx, port, proto)
	case "linux":
		if name, pid, ok := ownerSS(ctx, port, proto); ok {
			return name, pid, true
		}
	}
	return ownerLsof(ctx, port, proto)
}

func ownerLsof(ctx context.Context, port int, proto string) (string, int, bool) {
	spec := "-iTCP:" + strconv.Itoa(port)
	args := []string{"-nP", spec, "-sTCP:LISTEN", "-Fpc"}
	if proto == ProtoUDP {
		args = []string{"-nP", "-iUDP:" + strconv.Itoa(port), "-Fpc"}
	}
	out, err := output(ctx, "lsof", args...)
	if err != nil && len(out) == 0 {
		return "", 0, false
	}
	pid, name := 0, ""
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "p") && pid == 0:
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "c") && name == "":
			name = line[1:]
		}
	}
	return name, pid, pid > 0
}

var ssUsers = regexp.MustCompile(`users:\(\("([^"]+)",pid=(\d+)`)

func ownerSS(ctx context.Context, port int, proto string) (string, int, bool) {
	flag := "-ltnpH"
	if proto == ProtoUDP {
		flag = "-lunpH"
	}
	out, err := output(ctx, "ss", flag, "sport", "= :"+strconv.Itoa(port))
	if err != nil {
		return "", 0, false
	}
	m := ssUsers.FindSubmatch(out)
	if m == nil {
		return "", 0, false
	}
	pid, _ := strconv.Atoi(string(m[2]))
	return string(m[1]), pid, true
}

func ownerWindows(ctx context.Context, port int, proto string) (string, int, bool) {
	p := "TCP"
	if proto == ProtoUDP {
		p = "UDP"
	}
	out, err := output(ctx, "netstat", "-ano", "-p", p)
	if err != nil {
		return "", 0, false
	}
	suffix := ":" + strconv.Itoa(port)
	pid := 0
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || !strings.HasSuffix(f[1], suffix) {
			continue
		}
		if p == "TCP" && (len(f) < 5 || f[3] != "LISTENING") {
			continue
		}
		pid, _ = strconv.Atoi(f[len(f)-1])
		break
	}
	if pid <= 0 {
		return "", 0, false
	}
	name := "pid " + strconv.Itoa(pid)
	if out, err := output(ctx, "tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH"); err == nil {
		if fields := strings.Split(strings.TrimSpace(string(out)), ","); len(fields) > 0 && strings.HasPrefix(fields[0], `"`) {
			name = strings.Trim(fields[0], `"`)
		}
	}
	return name, pid, true
}
