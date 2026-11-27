package admin

import "sudoku-golang/internal/shellquote"

func shellJoin(args []string) string {
	return shellquote.Join(args)
}

func displayArgv(argv []string) []string {
	if len(argv) > 1 && argv[0] == "/usr/bin/env" {
		return argv[1:]
	}
	return argv
}
