package admin

import (
	"errors"
	"log/slog"
	"testing"
	"time"
)

func TestRestartAdmin(t *testing.T) {
	s := &Service{log: slog.New(slog.DiscardHandler)}
	s.runs = newRunManager(nil, func(string, bool) {})
	var ue *userError
	if err := s.RestartAdmin(); !errors.As(err, &ue) {
		t.Fatalf("no restarter: %v", err)
	}

	called := make(chan bool, 1)
	s.restart = func(drop bool) error {
		called <- drop
		return nil
	}
	if err := s.runs.tryExclusive(); err != nil {
		t.Fatal(err)
	}
	if err := s.RestartAdmin(); !errors.Is(err, errExclusiveBusy) {
		t.Fatalf("busy slot must refuse: %v", err)
	}
	s.runs.endExclusive()

	if err := s.RestartAdmin(); err != nil {
		t.Fatal(err)
	}
	select {
	case drop := <-called:
		if drop {
			t.Fatal("restart must keep --addr")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("restarter not called")
	}
}
