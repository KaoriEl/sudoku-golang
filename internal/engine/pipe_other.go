//go:build !windows

package engine

import (
	"context"
	"fmt"
	"net"
)

func dialPipe(_ context.Context, name string) (net.Conn, error) {
	return nil, fmt.Errorf("%w: named pipe %s is Windows-only", ErrUnsupportedEndpoint, name)
}
