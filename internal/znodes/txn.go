package znodes

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Txn is a committed mutation. Followers apply these in zxid order.
// The public HTTP API never exposes Raft terms; zxid is the ZK order.
type Txn struct {
	Zxid      int64  `json:"zxid"`
	Op        string `json:"op"`
	Path      string `json:"path"`
	Data      []byte `json:"data,omitempty"`
	SessionID int64  `json:"sessionId,omitempty"`
	Ephemeral bool   `json:"ephemeral,omitempty"`
}

func (s *Store) ApplyTxn(t Txn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.Zxid <= s.zxid {
		return nil
	}
	if t.Zxid != s.zxid+1 {
		return fmt.Errorf("%w: have %d got %d", ErrZxidGap, s.zxid, t.Zxid)
	}
	switch t.Op {
	case "create":
		return s.applyCreateLocked(t)
	case "set":
		return s.applySetLocked(t)
	case "delete":
		return s.applyDeleteLocked(t)
	default:
		return fmt.Errorf("unknown op %q", t.Op)
	}
}

func (s *Store) applyCreateLocked(t Txn) error {
	path, err := cleanPath(t.Path)
	if err != nil {
		return err
	}
	parentPath, name := split(path)
	parent, err := s.lookupLocked(parentPath)
	if err != nil {
		if errors.Is(err, ErrNoNode) {
			return ErrNoParent
		}
		return err
	}
	if _, exists := parent.children[name]; exists {
		return ErrNodeExists
	}
	n := &node{
		data:     clone(t.Data),
		czxid:    t.Zxid,
		mzxid:    t.Zxid,
		children: map[string]*node{},
	}
	if t.Ephemeral {
		n.ephemeralOwner = t.SessionID
		if s.ephByOwner[t.SessionID] == nil {
			s.ephByOwner[t.SessionID] = map[string]struct{}{}
		}
		s.ephByOwner[t.SessionID][path] = struct{}{}
	}
	parent.children[name] = n
	parent.cversion++
	bumpSeq(parent, name)
	s.zxid = t.Zxid
	s.fireLocked(parentPath, WatchChildren, Event{Type: "NodeChildrenChanged", Path: parentPath, Zxid: t.Zxid})
	return nil
}

func (s *Store) applySetLocked(t Txn) error {
	path, err := cleanPath(t.Path)
	if err != nil {
		return err
	}
	n, err := s.lookupLocked(path)
	if err != nil {
		return err
	}
	n.data = clone(t.Data)
	n.mzxid = t.Zxid
	n.version++
	s.zxid = t.Zxid
	s.fireLocked(path, WatchData, Event{Type: "NodeDataChanged", Path: path, Zxid: t.Zxid})
	return nil
}

func (s *Store) applyDeleteLocked(t Txn) error {
	path, err := cleanPath(t.Path)
	if err != nil {
		return err
	}
	parentPath, name := split(path)
	parent, err := s.lookupLocked(parentPath)
	if err != nil {
		return err
	}
	child, ok := parent.children[name]
	if !ok {
		return ErrNoNode
	}
	delete(parent.children, name)
	parent.cversion++
	if child.ephemeralOwner != 0 {
		delete(s.ephByOwner[child.ephemeralOwner], path)
	}
	s.zxid = t.Zxid
	s.fireLocked(path, WatchData, Event{Type: "NodeDeleted", Path: path, Zxid: t.Zxid})
	s.fireLocked(parentPath, WatchChildren, Event{Type: "NodeChildrenChanged", Path: parentPath, Zxid: t.Zxid})
	return nil
}

func bumpSeq(parent *node, name string) {
	i := strings.LastIndex(name, "-")
	if i < 0 {
		return
	}
	n, err := strconv.Atoi(name[i+1:])
	if err != nil {
		return
	}
	if int32(n)+1 > parent.seq {
		parent.seq = int32(n) + 1
	}
}
