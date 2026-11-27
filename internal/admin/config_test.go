package admin

import "testing"

func TestPatchEnv(t *testing.T) {
	in := "# comment\nDOCKER_PATH=/usr/bin/env docker\nMAX_WORKERS=10\nPREFIX_CONTAINER_NAME=work\nexport ROOT_PROJECTS_FOLDER=/a\n"
	got := patchEnv(in, map[string]string{
		keyWorkers:   "4",
		keyRoot:      "/b c",
		keyDocker:    "",
		keyAdminAddr: "127.0.0.1:7790",
		keyCompose:   `has "quote" #x`,
	})
	want := "# comment\nMAX_WORKERS=4\nPREFIX_CONTAINER_NAME=work\nROOT_PROJECTS_FOLDER=/b c\n" +
		"DOCKER_COMPOSE_PATH=\"has \\\"quote\\\" #x\"\nADMIN_ADDR=127.0.0.1:7790\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := patchEnv("", map[string]string{keyWorkers: "3"}); got != "MAX_WORKERS=3\n" {
		t.Fatalf("empty file: %q", got)
	}
}

func TestCheckAddr(t *testing.T) {
	s := &Service{addr: "127.0.0.1:7777"}
	for _, bad := range []string{"0.0.0.0:7777", "example.com:80", "127.0.0.1", "127.0.0.1:0", "127.0.0.1:99999"} {
		if err := s.checkAddr(bad); err == nil {
			t.Fatalf("%s must be rejected", bad)
		}
	}
	if err := s.checkAddr("127.0.0.1:7777"); err != nil {
		t.Fatalf("current addr must pass: %v", err)
	}
}
