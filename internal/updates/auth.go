package updates

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sudoku-golang/internal/sysproc"
	"time"
)

const (
	dockerHubIndex = "https://index.docker.io/v1/"
	helperTimeout  = 5 * time.Second
)

type dockerConfig struct {
	Auths map[string]struct {
		Auth          string `json:"auth"`
		IdentityToken string `json:"identitytoken"`
	} `json:"auths"`
	CredsStore  string            `json:"credsStore"`
	CredHelpers map[string]string `json:"credHelpers"`
}

type authConfig struct {
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	IdentityToken string `json:"identitytoken,omitempty"`
	ServerAddress string `json:"serveraddress,omitempty"`
}

func configDir() string {
	if d := os.Getenv("DOCKER_CONFIG"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".docker")
}

// registryOf returns the registry host of an image reference ("docker.io" for Hub images).
func registryOf(ref string) string {
	first, _, ok := strings.Cut(ref, "/")
	if !ok || (!strings.ContainsAny(first, ".:") && first != "localhost") {
		return "docker.io"
	}
	return first
}

func loadConfig() (dockerConfig, bool) {
	var cfg dockerConfig
	raw, err := os.ReadFile(filepath.Join(configDir(), "config.json"))
	if err != nil || json.Unmarshal(raw, &cfg) != nil {
		return cfg, false
	}
	return cfg, true
}

func fromHelper(ctx context.Context, helper, server string) (authConfig, bool) {
	ctx, cancel := context.WithTimeout(ctx, helperTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker-credential-"+helper, "get") //nolint:gosec // helper name comes from the user's docker config
	cmd.Stdin = strings.NewReader(server)
	sysproc.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return authConfig{}, false
	}
	var c struct{ Username, Secret string }
	if json.Unmarshal(out, &c) != nil || c.Secret == "" {
		return authConfig{}, false
	}
	if c.Username == "<token>" {
		return authConfig{IdentityToken: c.Secret, ServerAddress: server}, true
	}
	return authConfig{Username: c.Username, Password: c.Secret, ServerAddress: server}, true
}

// registryAuth builds the X-Registry-Auth header for ref the way the docker CLI would, from
// ~/.docker/config.json (inline auths, credHelpers, credsStore). Empty means anonymous.
func registryAuth(ctx context.Context, ref string) string {
	cfg, ok := loadConfig()
	if !ok {
		return ""
	}
	reg := registryOf(ref)
	keys := []string{reg, "https://" + reg, "http://" + reg}
	if reg == "docker.io" {
		keys = append([]string{dockerHubIndex, "index.docker.io", "registry-1.docker.io"}, keys...)
	}
	var ac authConfig
	found := false
	if helper, ok := cfg.CredHelpers[reg]; ok {
		ac, found = fromHelper(ctx, helper, keys[0])
	}
	for _, k := range keys {
		if found {
			break
		}
		if a, ok := cfg.Auths[k]; ok {
			if a.IdentityToken != "" {
				ac, found = authConfig{IdentityToken: a.IdentityToken, ServerAddress: k}, true
				continue
			}
			if dec, err := base64.StdEncoding.DecodeString(a.Auth); err == nil {
				if user, pass, ok := strings.Cut(string(dec), ":"); ok {
					ac, found = authConfig{Username: user, Password: pass, ServerAddress: k}, true
				}
			}
		}
	}
	if !found && cfg.CredsStore != "" {
		ac, found = fromHelper(ctx, cfg.CredsStore, keys[0])
	}
	if !found {
		return ""
	}
	raw, _ := json.Marshal(ac) //nolint:gosec // X-Registry-Auth carries the credentials by design
	return base64.URLEncoding.EncodeToString(bytes.TrimSpace(raw))
}
