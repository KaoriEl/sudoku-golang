package backup

import "testing"

func TestDetect(t *testing.T) {
	cases := map[string]Kind{
		"postgres:16":               Postgres,
		"postgis/postgis:16-3.4":    Postgres,
		"timescale/timescaledb:2":   Postgres,
		"mysql:8":                   MySQL,
		"mariadb:11":                MySQL,
		"mongo:7":                   Mongo,
		"redis:7-alpine":            Redis,
		"eqalpha/keydb:latest":      Redis,
		"valkey/valkey":             Redis,
		"registry.local:5000/redis": Redis,
		"ghcr.io/acme/api:postgres": "",
		"nginx:alpine":              "",
		"elasticsearch:9.1.8":       "",
	}
	for image, want := range cases {
		got, ok := Detect(image)
		if got != want || ok != (want != "") {
			t.Errorf("Detect(%q) = %q, %v; want %q", image, got, ok, want)
		}
	}
}

func TestIDValidation(t *testing.T) {
	m := &Manager{dir: func() string { return t.TempDir() }}
	for _, id := range []string{"../etc/20240101-000000", "a/b/20240101-000000", "x/2024", "x/../20240101-000000"} {
		if _, err := m.Get(id); err == nil {
			t.Errorf("Get(%q) accepted a bad id", id)
		}
	}
}
