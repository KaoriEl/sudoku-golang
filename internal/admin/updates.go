package admin

import (
	"context"
	"errors"
	"net/http"
	"sudoku-golang/internal/updates"
	"time"
)

const updatesTimeout = 5 * time.Minute

type UpdatesView struct {
	Running   bool             `json:"running"`
	CheckedAt *time.Time       `json:"checkedAt"`
	Images    []updates.Status `json:"images"`
	Available int              `json:"available"`
}

func (s *Service) updatesView() UpdatesView {
	running, last := s.updates.State()
	v := UpdatesView{Running: running, Images: []updates.Status{}}
	if !last.IsZero() {
		v.CheckedAt = &last
	}
	for _, st := range s.updates.Results() {
		v.Images = append(v.Images, st)
		if st.Available {
			v.Available++
		}
	}
	return v
}

// CheckUpdates asks the registries about every image used by a managed container.
func (s *Service) CheckUpdates(ctx context.Context) (UpdatesView, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return UpdatesView{}, err
	}
	images := make([]string, 0, len(snap.Containers))
	for _, c := range snap.Containers {
		if c.Managed {
			images = append(images, c.Image)
		}
	}
	if _, err := s.updates.Check(ctx, images); err != nil {
		return UpdatesView{}, err //nolint:wrapcheck // ErrBusy
	}
	s.hub.markState()
	return s.updatesView(), nil
}

func (s *Server) handleUpdates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.updatesView())
}

func (s *Server) handleCheckUpdates(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), updatesTimeout)
	defer cancel()
	v, err := s.svc.CheckUpdates(ctx)
	if errors.Is(err, updates.ErrBusy) {
		writeJSON(w, http.StatusConflict, apiError{Error: err.Error(), Code: "busy"})
		return
	}
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
