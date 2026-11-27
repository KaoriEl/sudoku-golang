package commands

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

var errBroken = errors.New("broken")

func fakeChoice(found string) commandChoice {
	return commandChoice{
		title: "docker", key: "DOCKER_PATH", example: "docker",
		detect: func() (string, string, bool) { return found, "1.0", found != "" },
		check: func(c string) (string, error) {
			if strings.Contains(c, "bad") {
				return "", errBroken
			}
			return "1.0", nil
		},
	}
}

func prompt(input string) *prompter {
	return &prompter{on: true, in: bufio.NewReader(strings.NewReader(input))}
}

func TestChooseCommand(t *testing.T) {
	cases := []struct {
		name, found, existing, input, want string
	}{
		{"accept detected", "/usr/local/bin/docker", "", "\n", "/usr/local/bin/docker"},
		{"reject and type own", "/usr/local/bin/docker", "", "n\n/opt/docker\n", "/opt/docker"},
		{"broken input asked again", "/usr/local/bin/docker", "", "n\n/bad\nn\n/opt/docker\n", "/opt/docker"},
		{"broken input kept on demand", "", "", "/bad\ny\n", "/bad"},
		{"empty keeps detected", "/usr/local/bin/docker", "", "n\n\n", "/usr/local/bin/docker"},
		{"working .env value wins", "/usr/local/bin/docker", "/env/docker", "\n", "/env/docker"},
		{"broken .env value replaced", "/usr/local/bin/docker", "/bad/docker", "\n", "/usr/local/bin/docker"},
	}
	for _, c := range cases {
		ch := fakeChoice(c.found)
		ch.existing = c.existing
		if got := chooseCommand(prompt(c.input), ch); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	if got := chooseCommand(&prompter{}, fakeChoice("")); got != "" {
		t.Errorf("non-interactive without a docker: %q", got)
	}
	ch := fakeChoice("/usr/local/bin/docker")
	ch.flag = "/flag/docker"
	if got := chooseCommand(prompt(""), ch); got != "/flag/docker" {
		t.Errorf("flag: %q", got)
	}
}
