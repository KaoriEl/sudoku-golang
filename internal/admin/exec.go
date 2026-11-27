package admin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sudoku-golang/internal/engine"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	defaultCols uint16 = 120
	defaultRows uint16 = 32
	shellScript        = "if command -v bash >/dev/null 2>&1; then exec bash; else exec sh; fi"
	execSetup          = 15 * time.Second
	readLimit          = 1 << 20
	ptyBuffer          = 32 * 1024
)

type termControl struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func clampSize(v int, def uint16) uint16 {
	if v < 2 || v > 1000 {
		return def
	}
	return uint16(v)
}

// terminal is one interactive session, either an engine exec (any OS) or a host pty (Unix fallback).
type terminal interface {
	io.ReadWriteCloser
	Resize(cols, rows uint16)
	// Wait blocks until the shell exits and returns a short reason.
	Wait() string
}

// engineTerminal is `docker exec -it` done through the Engine API: the exec's TTY stream is the
// hijacked HTTP connection itself, so no pty is needed on the host (works on Windows too).
type engineTerminal struct {
	s      *Service
	ctx    context.Context
	execID string
	stream io.ReadWriteCloser
	done   chan struct{}
	once   sync.Once
}

func (t *engineTerminal) Read(p []byte) (int, error) {
	n, err := t.stream.Read(p)
	if err != nil {
		t.once.Do(func() { close(t.done) })
	}
	return n, err //nolint:wrapcheck // raw stream
}

func (t *engineTerminal) Write(p []byte) (int, error) {
	return t.stream.Write(p) //nolint:wrapcheck // raw stream
}

func (t *engineTerminal) Close() error {
	return t.stream.Close() //nolint:wrapcheck // raw stream
}

func (t *engineTerminal) Resize(cols, rows uint16) {
	ctx, cancel := context.WithTimeout(t.ctx, 5*time.Second)
	defer cancel()
	_ = t.s.engine.ExecResize(ctx, t.execID, uint(cols), uint(rows))
}

func (t *engineTerminal) Wait() string {
	select {
	case <-t.done:
	case <-t.ctx.Done():
		return "closed"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, code, err := t.s.engine.ExecInspect(ctx, t.execID); err == nil && code != 0 {
		return "exit " + strconv.Itoa(code)
	}
	return "exited"
}

func engineExecConfig(size [2]uint) engine.ExecConfig {
	return engine.ExecConfig{
		Cmd: []string{"sh", "-c", shellScript}, Env: []string{"TERM=xterm-256color"},
		Tty: true, AttachStdin: true, AttachStdout: true, AttachStderr: true, ConsoleSize: &size,
	}
}

func (s *Service) openEngineTerminal(ctx context.Context, containerID string, cols, rows uint16) (terminal, error) {
	setup, cancel := context.WithTimeout(ctx, execSetup)
	defer cancel()
	size := [2]uint{uint(rows), uint(cols)}
	id, err := s.engine.ExecCreate(setup, containerID, engineExecConfig(size))
	if err != nil {
		return nil, err //nolint:wrapcheck // descriptive
	}
	stream, err := s.engine.ExecStart(setup, id, true)
	if err != nil {
		return nil, err //nolint:wrapcheck // descriptive
	}
	t := &engineTerminal{s: s, ctx: ctx, execID: id, stream: stream, done: make(chan struct{})}
	t.Resize(cols, rows)
	return t, nil
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	c, err := s.svc.FindContainer(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	if !c.Managed {
		s.writeError(w, ErrNotManaged)
		return
	}
	if c.State != dockerRunning {
		s.writeError(w, errNotRunning)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.log.Warn("websocket accept failed", slog.Any("error", err))
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(readLimit)

	ctx, cancel := context.WithCancel(s.baseCtx)
	defer cancel()

	cols := clampSize(queryInt(r, "cols", int(defaultCols), 0, 2000), defaultCols)
	rows := clampSize(queryInt(r, "rows", int(defaultRows), 0, 2000), defaultRows)

	term, err := s.svc.openEngineTerminal(ctx, c.ID, cols, rows)
	if err != nil {
		s.log.Debug("engine exec unavailable, trying host pty", slog.Any("error", err))
		term, err = s.svc.openPtyTerminal(ctx, c.ID, cols, rows)
	}
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, truncateReason(err.Error()))
		return
	}
	defer func() { _ = term.Close() }()

	s.log.Info("exec session opened", slog.String("container", c.Name))
	var once sync.Once
	closeWith := func(code websocket.StatusCode, reason string) {
		once.Do(func() {
			_ = conn.Close(code, truncateReason(reason))
			cancel()
		})
	}

	go pumpTerminal(ctx, conn, term, closeWith)
	go pumpWS(ctx, conn, term, closeWith)

	closeWith(websocket.StatusNormalClosure, term.Wait())
	s.log.Info("exec session closed", slog.String("container", c.Name))
}

// truncateReason keeps a close reason within the 123 bytes a WebSocket close frame allows.
func truncateReason(s string) string {
	if len(s) > 120 {
		return s[:120]
	}
	return s
}

func pumpTerminal(ctx context.Context, conn *websocket.Conn, term terminal, closeWith func(websocket.StatusCode, string)) {
	buf := make([]byte, ptyBuffer)
	for {
		n, err := term.Read(buf)
		if n > 0 {
			if werr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
				closeWith(websocket.StatusGoingAway, "write failed")
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func pumpWS(ctx context.Context, conn *websocket.Conn, term terminal, closeWith func(websocket.StatusCode, string)) {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			closeWith(websocket.StatusNormalClosure, "client closed")
			return
		}
		if typ == websocket.MessageBinary {
			if _, err := term.Write(data); err != nil {
				closeWith(websocket.StatusInternalError, "terminal write failed")
				return
			}
			continue
		}
		var ctl termControl
		if json.Unmarshal(data, &ctl) == nil && ctl.Type == "resize" {
			term.Resize(clampSize(ctl.Cols, defaultCols), clampSize(ctl.Rows, defaultRows))
		}
	}
}
