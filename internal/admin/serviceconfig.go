package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigVar is one ${VAR} reference of a service.
type ConfigVar struct {
	Name     string `json:"name"`
	Set      bool   `json:"set"`
	Default  string `json:"default"`
	Required bool   `json:"required"`
	Uses     int    `json:"uses"`
}

type ServiceConfig struct {
	Service     string      `json:"service"`
	ComposeFile string      `json:"composeFile"`
	YAML        string      `json:"yaml"`
	RawYAML     string      `json:"rawYaml"`
	Variables   []ConfigVar `json:"variables"`
	Warnings    []string    `json:"warnings"`
	Error       string      `json:"error"`
}

var (
	varRe      = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?[-?+])([^}]*))?}|\$([A-Za-z_][A-Za-z0-9_]*)`)
	unsetVarRe = regexp.MustCompile(`"([A-Za-z_][A-Za-z0-9_]*)" variable is not set`)
)

func serviceSection(raw string, service string) (map[string]any, error) {
	var cfg struct {
		Services map[string]map[string]any `json:"services"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("decode compose config: %w", err)
	}
	svc, ok := cfg.Services[service]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownService, service)
	}
	return svc, nil
}

func toYAML(v any) string {
	out, err := yaml.Marshal(v)
	if err != nil {
		return ""
	}
	return string(out)
}

func extractVars(raw string, unset map[string]bool) []ConfigVar {
	byName := map[string]*ConfigVar{}
	for _, m := range varRe.FindAllStringSubmatchIndex(raw, -1) {
		if m[0] > 0 && raw[m[0]-1] == '$' {
			continue // $$ is a literal dollar
		}
		var name, op, def string
		if m[2] >= 0 {
			name = raw[m[2]:m[3]]
			if m[4] >= 0 {
				op, def = raw[m[4]:m[5]], raw[m[6]:m[7]]
			}
		} else {
			name = raw[m[8]:m[9]]
		}
		v, ok := byName[name]
		if !ok {
			v = &ConfigVar{Name: name, Set: !unset[name]}
			byName[name] = v
		}
		v.Uses++
		switch strings.TrimPrefix(op, ":") {
		case "-":
			v.Default = def
		case "?":
			v.Required = true
		}
	}
	out := make([]ConfigVar, 0, len(byName))
	for _, v := range byName {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Set != out[j].Set {
			return !out[i].Set
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ServiceConfig shows the effective compose config of one service and which variables it uses,
// flagging the ones compose could not resolve.
func (s *Service) ServiceConfig(ctx context.Context, id string) (ServiceConfig, error) {
	c, err := s.FindContainer(ctx, id)
	if err != nil {
		return ServiceConfig{}, err
	}
	if !c.Managed {
		return ServiceConfig{}, ErrNotManaged
	}
	out := ServiceConfig{Service: c.Service, ComposeFile: c.ComposeFile, Variables: []ConfigVar{}, Warnings: []string{}}
	resolved, stderr, cerr := s.docker.ComposeOutput(ctx, c.ComposeFile, "config", "--format", "json")
	if cerr != nil {
		out.Error = cerr.Error() // an invalid compose file is shown to the operator, not a request failure
		return out, nil          //nolint:nilerr // reported inside the result
	}
	unset := map[string]bool{}
	for _, line := range strings.Split(stderr, "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		out.Warnings = append(out.Warnings, line)
		if m := unsetVarRe.FindStringSubmatch(line); m != nil {
			unset[m[1]] = true
		}
	}
	if svc, err := serviceSection(resolved, c.Service); err == nil {
		out.YAML = toYAML(map[string]any{c.Service: svc})
	} else {
		out.Error = err.Error()
	}
	s.addRawConfig(ctx, c, unset, &out)
	return out, nil
}

// addRawConfig adds the uninterpolated config (where ${VAR} references are still visible). Older
// compose versions without --no-interpolate simply leave it empty.
func (s *Service) addRawConfig(ctx context.Context, c Container, unset map[string]bool, out *ServiceConfig) {
	raw, _, err := s.docker.ComposeOutput(ctx, c.ComposeFile, "config", "--no-interpolate", "--format", "json")
	if err != nil {
		return
	}
	svc, err := serviceSection(raw, c.Service)
	if err != nil {
		return
	}
	out.RawYAML = toYAML(map[string]any{c.Service: svc})
	rawJSON, _ := json.Marshal(svc)
	out.Variables = extractVars(string(rawJSON), unset)
}

func (s *Server) handleServiceConfig(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), snapshotTimeout)
	defer cancel()
	out, err := s.svc.ServiceConfig(ctx, r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
