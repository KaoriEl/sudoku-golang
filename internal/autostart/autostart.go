package autostart

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	launchdLabel = "com.sudoku.serve"
	systemdUnit  = "sudoku-serve.service"
	cmdTimeout   = 10 * time.Second
	osDarwin     = "darwin"
	osLinux      = "linux"
	osWindows    = "windows"
	vbsName      = "sudoku-serve.vbs"
	fileMode     = 0o600
)

var ErrUnsupported = errors.New("autostart is not supported here")

type Spec struct {
	Executable string
	Args       []string
	WorkDir    string
	Env        map[string]string
}

type Status struct {
	Supported  bool   `json:"supported"`
	Enabled    bool   `json:"enabled"`
	Platform   string `json:"platform"`
	Path       string `json:"path"`
	Executable string `json:"executable"`
	WorkDir    string `json:"workDir"`
	LogPath    string `json:"logPath"`
	Reason     string `json:"reason"`
}

func CurrentSpec(addr string, extraEnv map[string]string) (Spec, string) {
	exe, err := os.Executable()
	if err != nil {
		return Spec{}, "не удалось определить путь к бинарнику: " + err.Error()
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if strings.Contains(exe, string(filepath.Separator)+"go-build") {
		return Spec{}, "админка запущена через `go run` — запустите собранный бинарник sudoku"
	}
	wd, err := os.Getwd()
	if err != nil {
		return Spec{}, "не удалось определить рабочую папку: " + err.Error()
	}
	env := map[string]string{"PATH": os.Getenv("PATH")}
	for k, v := range extraEnv {
		if v != "" {
			env[k] = v
		}
	}
	args := []string{"serve"}
	if addr != "" {
		args = append(args, "--addr", addr)
	}
	return Spec{Executable: exe, Args: args, WorkDir: wd, Env: env}, ""
}

func paths() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("home dir: %w", err)
	}
	switch runtime.GOOS {
	case osDarwin:
		return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), filepath.Join(home, "Library", "Logs", "sudoku-serve.log"), nil
	case osLinux:
		return filepath.Join(home, ".config", "systemd", "user", systemdUnit), "journalctl --user -u " + systemdUnit, nil
	case osWindows:
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", vbsName), "", nil
	default:
		return "", "", ErrUnsupported
	}
}

func Get(spec Spec, reason string) Status {
	st := Status{Platform: runtime.GOOS, Executable: spec.Executable, WorkDir: spec.WorkDir, Reason: reason}
	path, logPath, err := paths()
	if err != nil {
		st.Reason = "автозапуск поддерживается на macOS (launchd), Linux (systemd --user) и Windows (папка «Автозагрузка»)"
		return st
	}
	st.Path, st.LogPath = path, logPath
	st.Supported = reason == ""
	if _, err := os.Stat(path); err == nil {
		st.Enabled = true
	}
	return st
}

func xmlText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func plist(spec Spec, logPath string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("  <key>Label</key>\n  <string>" + launchdLabel + "</string>\n")
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	b.WriteString("    <string>" + xmlText(spec.Executable) + "</string>\n")
	for _, a := range spec.Args {
		b.WriteString("    <string>" + xmlText(a) + "</string>\n")
	}
	b.WriteString("  </array>\n")
	b.WriteString("  <key>WorkingDirectory</key>\n  <string>" + xmlText(spec.WorkDir) + "</string>\n")
	b.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
	for k, v := range spec.Env {
		b.WriteString("    <key>" + xmlText(k) + "</key>\n    <string>" + xmlText(v) + "</string>\n")
	}
	b.WriteString("  </dict>\n")
	b.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	b.WriteString("  <key>StandardOutPath</key>\n  <string>" + xmlText(logPath) + "</string>\n")
	b.WriteString("  <key>StandardErrorPath</key>\n  <string>" + xmlText(logPath) + "</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func systemdQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(s) + `"`
}

func unit(spec Spec) string {
	exec := make([]string, 0, len(spec.Args)+1)
	exec = append(exec, systemdQuote(spec.Executable))
	for _, a := range spec.Args {
		exec = append(exec, systemdQuote(a))
	}
	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, systemdQuote(k+"="+v))
	}
	return "[Unit]\nDescription=sudoku admin panel\n\n[Service]\n" +
		"ExecStart=" + strings.Join(exec, " ") + "\n" +
		"WorkingDirectory=" + spec.WorkDir + "\n" +
		"Environment=" + strings.Join(env, " ") + "\n" +
		"Restart=on-failure\n\n[Install]\nWantedBy=default.target\n"
}

func vbsString(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// vbs is a Startup-folder script that starts the server hidden (window style 0), so no console
// window pops up at login; WScript sets the working directory and environment first.
func vbs(spec Spec) string {
	var b strings.Builder
	b.WriteString("' sudoku: автозапуск админки (sudoku serve). Удалить — выключить в Настройках.\r\n")
	b.WriteString("Set sh = CreateObject(\"WScript.Shell\")\r\n")
	b.WriteString("sh.CurrentDirectory = " + vbsString(spec.WorkDir) + "\r\n")
	b.WriteString("Set env = sh.Environment(\"PROCESS\")\r\n")
	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString("env(" + vbsString(k) + ") = " + vbsString(spec.Env[k]) + "\r\n")
	}
	cmd := `"` + spec.Executable + `"`
	for _, a := range spec.Args {
		cmd += ` "` + a + `"`
	}
	b.WriteString("sh.Run " + vbsString(cmd) + ", 0, False\r\n")
	return b.String()
}

func run(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func Enable(spec Spec) error {
	path, logPath, err := paths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	switch runtime.GOOS {
	case osDarwin:
		if err := os.WriteFile(path, []byte(plist(spec, logPath)), fileMode); err != nil {
			return fmt.Errorf("write plist: %w", err)
		}
		return nil
	case osLinux:
		if err := os.WriteFile(path, []byte(unit(spec)), fileMode); err != nil {
			return fmt.Errorf("write unit: %w", err)
		}
		if err := run("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		return run("systemctl", "--user", "enable", systemdUnit)
	case osWindows:
		if err := os.WriteFile(path, []byte(vbs(spec)), fileMode); err != nil {
			return fmt.Errorf("write startup script: %w", err)
		}
		return nil
	default:
		return ErrUnsupported
	}
}

func Disable() error {
	path, _, err := paths()
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case osDarwin:
		_ = run("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+launchdLabel)
	case osLinux:
		_ = run("systemctl", "--user", "disable", systemdUnit)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	if runtime.GOOS == osLinux {
		_ = run("systemctl", "--user", "daemon-reload")
	}
	return nil
}
