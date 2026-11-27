//go:build windows

package engine

import (
	"context"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

func dialPipe(ctx context.Context, name string) (net.Conn, error) {
	conn, err := winio.DialPipeContext(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("dial pipe %s: %w", name, err)
	}
	return conn, nil
}
