package oauthlogin

import (
	"context"
	"net/http"
	"time"
)

func (s *Service) resetWorker() error {
	s.workerResetMu.Lock()
	defer s.workerResetMu.Unlock()
	if s.workerReady {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.worker(ctx, http.MethodDelete, "/tasks", nil, nil); err != nil {
		return err
	}
	s.workerReady = true
	return nil
}
