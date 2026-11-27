package admin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultLogTail = 500
	maxLogTail     = 10000
	maxLogLine     = 16 * 1024
	logFlushEvery  = 100 * time.Millisecond
	logBatchMax    = 500
	logHeartbeat   = 15 * time.Second
)

type logLine struct {
	Time   string `json:"t"`
	Stream string `json:"s"`
	Msg    string `json:"m"`
	Src    string `json:"c,omitempty"`
}

type lineParser func(stream, raw string) logLine

// parseComposeLine splits `compose logs` output ("name  | 2024-…Z message") into source and line.
func parseComposeLine(stream, raw string) logLine {
	prefix, rest, ok := strings.Cut(raw, " | ")
	if !ok {
		return parseLogLine(stream, raw)
	}
	l := parseLogLine(stream, rest)
	l.Src = strings.TrimSpace(prefix)
	return l
}

func parseLogLine(stream, raw string) logLine {
	raw = strings.TrimRight(raw, "\r\n")
	if len(raw) >= maxLogLine {
		raw = strings.ToValidUTF8(raw, "") + " …"
	}
	ts, msg, ok := strings.Cut(raw, " ")
	if ok && len(ts) >= 20 && ts[4] == '-' && ts[10] == 'T' {
		return logLine{Time: ts, Stream: stream, Msg: msg}
	}
	return logLine{Stream: stream, Msg: raw}
}

// pumpLines reads lines with bounded memory: a line longer than the buffer (a container printing
// megabytes without a newline) is cut at maxLogLine and the rest is discarded, instead of
// bufio.ReadString growing without limit. It stops when ctx is done so it never blocks on a channel
// nobody reads any more.
func pumpLines(ctx context.Context, r io.Reader, stream string, out chan<- logLine, wg *sync.WaitGroup) {
	pumpParsed(ctx, r, stream, out, wg, parseLogLine)
}

func pumpParsed(ctx context.Context, r io.Reader, stream string, out chan<- logLine, wg *sync.WaitGroup, parse lineParser) {
	defer wg.Done()
	br := bufio.NewReaderSize(r, maxLogLine)
	for {
		chunk, err := br.ReadSlice('\n')
		line := string(chunk) // copy: ReadSlice's buffer is reused
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = br.ReadSlice('\n') // drop the rest of an over-long line
		}
		if line != "" {
			select {
			case out <- parse(stream, line):
			case <-ctx.Done():
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func queryInt(r *http.Request, key string, def, lo, hi int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return def
	}
	return max(lo, min(hi, v))
}

func writeSSE(w http.ResponseWriter, event string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode sse: %w", err)
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return fmt.Errorf("write sse: %w", err)
	}
	return nil
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	c, err := s.svc.FindContainer(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeNoStream(w)
		return
	}

	tailN := queryInt(r, "tail", defaultLogTail, 0, maxLogTail)
	args := []string{cmdLogs, "--timestamps", "--tail", strconv.Itoa(tailN)}
	if r.URL.Query().Get("follow") != "0" {
		args = append(args, "--follow")
	}
	args = append(args, c.ID)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	cmd, err := s.svc.docker.Command(ctx, s.svc.docker.DockerArgv(), args...)
	if err != nil {
		s.writeError(w, err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.writeError(w, fmt.Errorf("stdout pipe: %w", err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.writeError(w, fmt.Errorf("stderr pipe: %w", err))
		return
	}
	if err := cmd.Start(); err != nil {
		s.writeError(w, fmt.Errorf("docker logs: %w", err))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = writeSSE(w, "meta", map[string]any{"container": c.Name, "tail": tailN})
	flusher.Flush()

	lines := make(chan logLine, 1024)
	var wg sync.WaitGroup
	wg.Add(2)
	go pumpLines(ctx, stdout, "out", lines, &wg)
	go pumpLines(ctx, stderr, "err", lines, &wg)
	go func() {
		wg.Wait()
		close(lines)
	}()

	s.streamLogLines(ctx, w, flusher, lines)
	// Kill `docker logs --follow` first: when streaming stopped because a write failed, the request
	// context may still be alive and Wait would block forever on a process that never exits.
	cancel()
	wg.Wait()
	_ = cmd.Wait()
}

func (s *Server) streamLogLines(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, lines <-chan logLine) {
	ticker := time.NewTicker(logFlushEvery)
	defer ticker.Stop()
	heartbeat := time.NewTicker(logHeartbeat)
	defer heartbeat.Stop()

	batch := make([]logLine, 0, logBatchMax)
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		if err := writeSSE(w, "lines", batch); err != nil {
			return false
		}
		flusher.Flush()
		batch = batch[:0]
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return
		case l, ok := <-lines:
			if !ok {
				if flush() {
					_ = writeSSE(w, "end", map[string]string{"reason": "eof"})
					flusher.Flush()
				}
				return
			}
			batch = append(batch, l)
			if len(batch) >= logBatchMax && !flush() {
				return
			}
		case <-ticker.C:
			if !flush() {
				return
			}
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

var errNotRunning = errors.New("container is not running")

const (
	downloadMaxTail = 1_000_000
	projectLogTail  = 200
)

// syncWriter serialises the writes of stdout and stderr into one ResponseWriter.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p) //nolint:wrapcheck // passthrough
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// handleLogsDownload streams `docker logs` straight into the response (no buffering), so even a
// multi-gigabyte log downloads with constant memory.
func (s *Server) handleLogsDownload(w http.ResponseWriter, r *http.Request) {
	c, err := s.svc.FindContainer(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	tail := "all"
	if v := r.URL.Query().Get("tail"); v != "" && v != "all" {
		tail = strconv.Itoa(queryInt(r, "tail", defaultLogTail, 1, downloadMaxTail))
	}
	cmd, err := s.svc.docker.Command(r.Context(), s.svc.docker.DockerArgv(), cmdLogs, "--timestamps", "--tail", tail, c.ID)
	if err != nil {
		s.writeError(w, err)
		return
	}
	name := unsafeFileChars.ReplaceAllString(c.Name, "_") + "-" + time.Now().Format("20060102-150405") + ".log"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	sw := &syncWriter{w: w}
	cmd.Stdout, cmd.Stderr = sw, sw
	if err := cmd.Run(); err != nil && r.Context().Err() == nil {
		_, _ = io.WriteString(sw, "\n# sudoku: docker logs завершился с ошибкой: "+err.Error()+"\n")
	}
}

// handleProjectLogs is one SSE stream for several services of a compose file (`compose logs`), each
// line tagged with its container.
func (s *Server) handleProjectLogs(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	files, _, err := s.svc.composeFiles()
	if err != nil {
		s.writeError(w, err)
		return
	}
	known := false
	for _, f := range files {
		if f.Path == file {
			known = true
		}
	}
	if !known {
		s.writeError(w, fmt.Errorf("%w: %s", ErrUnknownService, file))
		return
	}
	var services []string
	if raw := strings.TrimSpace(r.URL.Query().Get("services")); raw != "" {
		all, err := s.svc.composeServices(r.Context(), file)
		if err != nil {
			s.writeError(w, err)
			return
		}
		for _, svc := range strings.Split(raw, ",") {
			if !slices.Contains(all, svc) {
				s.writeError(w, fmt.Errorf("%w: %s", ErrUnknownService, svc))
				return
			}
			services = append(services, svc)
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeNoStream(w)
		return
	}
	tailN := queryInt(r, "tail", projectLogTail, 0, maxLogTail)
	args := []string{flagFile, file, cmdLogs, "--no-color", "--timestamps", "--tail", strconv.Itoa(tailN)}
	if r.URL.Query().Get("follow") != "0" {
		args = append(args, "--follow")
	}
	args = append(args, services...)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	cmd, err := s.svc.docker.Command(ctx, s.svc.docker.ComposeArgv(), args...)
	if err != nil {
		s.writeError(w, err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.writeError(w, fmt.Errorf("stdout pipe: %w", err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.writeError(w, fmt.Errorf("stderr pipe: %w", err))
		return
	}
	if err := cmd.Start(); err != nil {
		s.writeError(w, fmt.Errorf("compose logs: %w", err))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = writeSSE(w, "meta", map[string]any{"file": file, "tail": tailN, "services": services})
	flusher.Flush()

	lines := make(chan logLine, 1024)
	var wg sync.WaitGroup
	wg.Add(2)
	go pumpParsed(ctx, stdout, "out", lines, &wg, parseComposeLine)
	go pumpParsed(ctx, stderr, "err", lines, &wg, parseComposeLine)
	go func() {
		wg.Wait()
		close(lines)
	}()
	s.streamLogLines(ctx, w, flusher, lines)
	cancel()
	wg.Wait()
	_ = cmd.Wait()
}
