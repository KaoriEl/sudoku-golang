package admin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sudoku-golang/internal/autostart"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/infra/configs"
	"time"

	"github.com/joho/godotenv"
)

const (
	keyRoot       = "ROOT_PROJECTS_FOLDER"
	keyDocker     = "DOCKER_PATH"
	keyCompose    = "DOCKER_COMPOSE_PATH"
	keyWorkers    = "MAX_WORKERS"
	keyAdminAddr  = "ADMIN_ADDR"
	maxWorkersCap = 64
	restartDelay  = 400 * time.Millisecond
	sourceFile    = "file"
)

// configKeys are the .env keys editable from the admin, in display order.
var configKeys = []string{keyRoot, keyDocker, keyCompose, keyWorkers, keyAdminAddr}

var (
	ErrConfigInvalid = errors.New("invalid config")
	errRestartBusy   = errors.New("идёт операция — дождитесь её окончания, затем сохраните конфиг")
)

type ConfigField struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	InFile    bool   `json:"inFile"`
	Effective string `json:"effective"`
	Default   string `json:"default"`
	Source    string `json:"source"`
}

type ConfigView struct {
	Path         string        `json:"path"`
	Exists       bool          `json:"exists"`
	Fields       []ConfigField `json:"fields"`
	OtherKeys    []string      `json:"otherKeys"`
	Addr         string        `json:"addr"`
	AddrFromFlag bool          `json:"addrFromFlag"`
	CanRestart   bool          `json:"canRestart"`
}

type ConfigUpdate struct {
	Values map[string]string `json:"values"`
}

type ConfigSaved struct {
	Config     ConfigView `json:"config"`
	Changed    []string   `json:"changed"`
	Restarting bool       `json:"restarting"`
	URL        string     `json:"url"`
}

// Restarter re-executes the server; dropAddrFlag removes a --addr flag so ADMIN_ADDR takes effect.
type Restarter func(dropAddrFlag bool) error

func (s *Service) SetRestarter(r Restarter, addrFromFlag bool) {
	s.restart = r
	s.addrFromFlag = addrFromFlag
}

func envPath() string {
	return configs.EnvPath()
}

func (s *Service) effective(key string) (string, string) {
	switch key {
	case keyRoot:
		return s.cfg.RootProjectsFolder, ""
	case keyDocker:
		return s.cfg.DockerPath, configs.DefaultDockerPath
	case keyCompose:
		return s.cfg.DockerComposePath, configs.DefaultDockerComposePath()
	case keyWorkers:
		return strconv.Itoa(s.cfg.MaxWorkers), "5"
	case keyAdminAddr:
		return s.addr, configs.DefaultAdminAddr
	}
	return "", ""
}

func (s *Service) Config() ConfigView {
	path := envPath()
	view := ConfigView{Path: path, Addr: s.addr, AddrFromFlag: s.addrFromFlag, CanRestart: s.restart != nil, OtherKeys: []string{}}
	values, err := godotenv.Read(path)
	if err == nil {
		view.Exists = true
	} else {
		values = map[string]string{}
	}
	for _, k := range configKeys {
		eff, def := s.effective(k)
		v, in := values[k]
		src := configs.Source(k)
		if k == keyAdminAddr && s.addrFromFlag {
			src = "flag"
		}
		view.Fields = append(view.Fields, ConfigField{Key: k, Value: v, InFile: in, Effective: eff, Default: def, Source: src})
	}
	for k := range values {
		if !slices.Contains(configKeys, k) {
			view.OtherKeys = append(view.OtherKeys, k)
		}
	}
	slices.Sort(view.OtherKeys)
	return view
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrConfigInvalid, fmt.Sprintf(format, a...))
}

func checkCommand(key, v string) error {
	argv := docker.SplitCommand(v)
	if len(argv) == 0 {
		return invalid("%s: пустая команда", key)
	}
	bin := argv[0]
	if filepath.Base(bin) == "env" && len(argv) > 1 {
		bin = argv[1]
	}
	if _, err := exec.LookPath(bin); err != nil {
		return invalid("%s: %q не найден", key, bin)
	}
	return nil
}

func (s *Service) validateConfig(key, v string) error {
	if strings.ContainsAny(v, "\r\n") {
		return invalid("%s: перевод строки недопустим", key)
	}
	if v == "" {
		return nil // removes the key: the default applies
	}
	switch key {
	case keyRoot:
		p := configs.ExpandPath(v, s.cfg.Home)
		st, err := os.Stat(p)
		if err != nil || !st.IsDir() {
			return invalid("%s: папка %q не найдена", key, p)
		}
		if s.cfg.Legacy() && p != s.cfg.RootProjectsFolder {
			return invalid("%s: инфраструктура (compose.yaml, sudoku-config) лежит в старой папке проектов %s — "+
				"сначала перенесите её командой `sudoku setup`, иначе после смены папки она пропадёт", key, s.cfg.RootProjectsFolder)
		}
	case keyDocker, keyCompose:
		return checkCommand(key, v)
	case keyWorkers:
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxWorkersCap {
			return invalid("%s: число от 1 до %d", key, maxWorkersCap)
		}
	case keyAdminAddr:
		return s.checkAddr(v)
	}
	return nil
}

func (s *Service) checkAddr(v string) error {
	if IsUnixAddr(v) {
		p := unixPath(v)
		if p == "" || !filepath.IsAbs(p) {
			return invalid("%s: ожидается unix:/абсолютный/путь.sock", keyAdminAddr)
		}
		if st, err := os.Stat(filepath.Dir(p)); err != nil || !st.IsDir() {
			return invalid("%s: папка для сокета %s не найдена", keyAdminAddr, filepath.Dir(p))
		}
		return nil
	}
	host, port, err := net.SplitHostPort(v)
	if err != nil {
		return invalid("%s: ожидается host:port, например 127.0.0.1:7777", keyAdminAddr)
	}
	if !isLoopbackHost(host) {
		return invalid("%s: админка слушает только loopback (127.0.0.1, ::1, localhost)", keyAdminAddr)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return invalid("%s: порт от 1 до 65535", keyAdminAddr)
	}
	if v == s.addr {
		return nil
	}
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", v)
	if err != nil {
		return invalid("%s: адрес %s занят или недоступен: %v", keyAdminAddr, v, err)
	}
	_ = l.Close()
	return nil
}

var safeEnvValue = regexp.MustCompile(`^[A-Za-z0-9_./:@%+,=~\- ]*$`)

func envLine(key, value string) string {
	if safeEnvValue.MatchString(value) && strings.TrimSpace(value) == value {
		return key + "=" + value
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`)
	return key + `="` + r.Replace(value) + `"`
}

// patchEnv replaces KEY=... lines in place (comments and other keys survive), drops keys set to ""
// and appends new ones.
func patchEnv(content string, values map[string]string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if content == "" {
		lines = nil
	}
	done := map[string]bool{}
	out := make([]string, 0, len(lines)+len(values))
	for _, line := range lines {
		trimmed := strings.TrimPrefix(strings.TrimSpace(line), "export ")
		key, _, ok := strings.Cut(trimmed, "=")
		key = strings.TrimSpace(key)
		v, edit := values[key]
		if !ok || !edit || strings.HasPrefix(trimmed, "#") {
			out = append(out, line)
			continue
		}
		if done[key] || v == "" {
			continue
		}
		done[key] = true
		out = append(out, envLine(key, v))
	}
	for _, k := range configKeys {
		if v, ok := values[k]; ok && v != "" && !done[k] {
			out = append(out, envLine(k, v))
		}
	}
	return strings.Join(out, "\n") + "\n"
}

// SaveConfig validates and writes .env, refreshes the autostart unit and schedules a restart.
func (s *Service) SaveConfig(u ConfigUpdate) (ConfigSaved, error) {
	// Held until the re-exec: a run started in the 400 ms before it would be killed halfway.
	if err := s.runs.tryExclusive(); err != nil {
		return ConfigSaved{}, errRestartBusy
	}
	restarting := false
	defer func() {
		if !restarting {
			s.runs.endExclusive()
		}
	}()

	s.files.Lock()
	defer s.files.Unlock()

	path := envPath()
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return ConfigSaved{}, fmt.Errorf("read %s: %w", path, err)
	}
	current, _ := godotenv.Read(path)

	changes := map[string]string{}
	changed := []string{}
	for k, v := range u.Values {
		if !slices.Contains(configKeys, k) {
			return ConfigSaved{}, invalid("неизвестный ключ %s", k)
		}
		v = strings.TrimSpace(v)
		if err := s.validateConfig(k, v); err != nil {
			return ConfigSaved{}, err
		}
		if old, in := current[k]; (in && old == v) || (!in && v == "") {
			continue
		}
		changes[k] = v
		changed = append(changed, k)
	}
	slices.Sort(changed)
	if len(changes) == 0 {
		return ConfigSaved{Config: s.Config(), Changed: changed}, nil
	}

	if err := writeFileAtomic(path, []byte(patchEnv(string(raw), changes))); err != nil {
		return ConfigSaved{}, err
	}
	s.log.Info("config saved", "path", path, "changed", strings.Join(changed, ","))

	addrChanged := slices.Contains(changed, keyAdminAddr)
	newAddr := s.addr
	if addrChanged {
		newAddr = changes[keyAdminAddr]
		if newAddr == "" {
			newAddr = configs.DefaultAdminAddr
		}
	}
	s.refreshAutostart(addrChanged)

	saved := ConfigSaved{Config: s.Config(), Changed: changed, URL: "http://" + newAddr}
	if IsUnixAddr(newAddr) {
		saved.URL = ""
	}
	if s.restart != nil {
		saved.Restarting = true
		restarting = true
		go func() {
			defer s.runs.endExclusive() // only reached when the exec failed
			time.Sleep(restartDelay)
			s.log.Info("restarting to apply config", "addr", newAddr)
			if err := s.restart(addrChanged); err != nil {
				s.log.Error("restart failed", "error", err)
			}
		}()
	}
	return saved, nil
}

// refreshAutostart rewrites an enabled LaunchAgent/systemd unit so it matches the new config.
func (s *Service) refreshAutostart(addrChanged bool) {
	spec, reason := s.autostartSpecWith(s.addrFromFlag && !addrChanged)
	if reason != "" || !autostart.Get(spec, reason).Enabled {
		return
	}
	if err := autostart.Enable(spec); err != nil {
		s.log.Error("refresh autostart", "error", err)
	}
}
