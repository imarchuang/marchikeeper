package znodes

import (
	"time"
)

const (
	WatchData     = "data"
	WatchChildren = "children"
)

// Event is a one-shot watch delivery.
type Event struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Zxid int64  `json:"zxid"`
}

type watch struct {
	session int64
	kind    string
}

func (s *Store) ArmWatch(path, kind string, sessionID int64) error {
	path, err := cleanPath(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	if _, ok := s.sessions[sessionID]; !ok {
		return ErrNoSession
	}
	if _, err := s.lookupLocked(path); err != nil {
		return err
	}
	s.watches[path] = append(s.watches[path], watch{session: sessionID, kind: kind})
	return nil
}

func (s *Store) WaitEvents(sessionID int64, timeout time.Duration) ([]Event, error) {
	deadline := time.Now().Add(timeout)
	for {
		s.mu.Lock()
		s.expireLocked()
		if _, ok := s.sessions[sessionID]; !ok {
			s.mu.Unlock()
			return nil, ErrNoSession
		}
		if ev := s.pending[sessionID]; len(ev) > 0 {
			s.pending[sessionID] = nil
			s.mu.Unlock()
			return ev, nil
		}
		ch := s.waiters[sessionID]
		if ch == nil {
			ch = make(chan struct{}, 1)
			s.waiters[sessionID] = ch
		}
		s.mu.Unlock()

		remain := time.Until(deadline)
		if remain <= 0 {
			return nil, nil
		}
		timer := time.NewTimer(remain)
		select {
		case <-ch:
			timer.Stop()
		case <-timer.C:
			return nil, nil
		}
	}
}

func (s *Store) fireLocked(path, kind string, ev Event) {
	kept := s.watches[path][:0]
	for _, w := range s.watches[path] {
		if w.kind != kind {
			kept = append(kept, w)
			continue
		}
		s.pending[w.session] = append(s.pending[w.session], ev)
		if ch := s.waiters[w.session]; ch != nil {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
	if len(kept) == 0 {
		delete(s.watches, path)
	} else {
		s.watches[path] = kept
	}
}

func (s *Store) dropSessionWatchesLocked(id int64) {
	for path, list := range s.watches {
		kept := list[:0]
		for _, w := range list {
			if w.session != id {
				kept = append(kept, w)
			}
		}
		if len(kept) == 0 {
			delete(s.watches, path)
		} else {
			s.watches[path] = kept
		}
	}
	delete(s.pending, id)
	delete(s.waiters, id)
}
