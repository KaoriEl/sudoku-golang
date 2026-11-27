package admin

import "time"

// RestartAdmin re-executes the server (the same binary path, so a fresh `sudoku update` takes effect).
// It holds the exclusive slot until the exec, so no run or cleanup starts in between, and is refused
// while one is active (ErrRunInProgress / errExclusiveBusy).
func (s *Service) RestartAdmin() error {
	if s.restart == nil {
		return &userError{msg: "перезапуск из админки недоступен в этом режиме — перезапустите sudoku serve вручную"}
	}
	if err := s.runs.tryExclusive(); err != nil {
		return err
	}
	go func() {
		defer s.runs.endExclusive()
		time.Sleep(restartDelay)
		s.log.Info("restarting admin by request")
		if err := s.restart(false); err != nil {
			s.log.Error("restart failed", "error", err)
		}
	}()
	return nil
}
