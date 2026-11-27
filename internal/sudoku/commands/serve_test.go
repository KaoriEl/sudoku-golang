package commands

import (
	"slices"
	"strings"
	"testing"
)

func TestWithoutFlag(t *testing.T) {
	cases := map[string][]string{
		"sudoku serve --addr 127.0.0.1:1 --log": strings.Fields("sudoku serve --log"),
		"sudoku serve --addr=127.0.0.1:1":       strings.Fields("sudoku serve"),
		"sudoku serve -addr x --debug":          strings.Fields("sudoku serve --debug"),
		"sudoku serve":                          strings.Fields("sudoku serve"),
	}
	for in, want := range cases {
		if got := withoutFlag(strings.Fields(in), "--addr"); !slices.Equal(got, want) {
			t.Fatalf("%q: got %v want %v", in, got, want)
		}
	}
}
