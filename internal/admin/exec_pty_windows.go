//go:build windows

package admin

import (
	"context"
	"errors"
)

var errNoPty = errors.New("терминал недоступен: нет доступа к Docker Engine API, а host pty на этой ОС не поддерживается")

// On Windows the terminal needs the engine API (named pipe); there is no host pty fallback.
func (s *Service) openPtyTerminal(context.Context, string, uint16, uint16) (terminal, error) {
	return nil, errNoPty
}
