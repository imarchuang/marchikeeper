package znodes

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNoNode         = errors.New("no node")
	ErrNodeExists     = errors.New("node exists")
	ErrNotEmpty       = errors.New("not empty")
	ErrBadPath        = errors.New("bad path")
	ErrRoot           = errors.New("cannot mutate root")
	ErrNoParent       = errors.New("no parent")
	ErrBadVersion     = errors.New("bad version")
	ErrNotEphemeral   = errors.New("session required for ephemeral")
	ErrNoSession      = errors.New("no session")
	ErrEphemeralChild = errors.New("ephemeral cannot have children")
)

// Stat is the ZooKeeper-inspired metadata for a znode.
type Stat struct {
	Czxid          int64 `json:"czxid"`
	Mzxid          int64 `json:"mzxid"`
	Version        int32 `json:"version"`
	Cversion       int32 `json:"cversion"`
	DataLength     int   `json:"dataLength"`
	NumChildren    int   `json:"numChildren"`
	EphemeralOwner int64 `json:"ephemeralOwner"`
}

// CreateFlags controls sequential (and later ephemeral) create behavior.
type CreateFlags struct {
	Sequential bool
	Ephemeral  bool
	SessionID  int64
}

// Store is an in-memory znode tree. Root "/" always exists.
type Store struct {
	mu         sync.Mutex
	zxid       int64
	root       *node
	clock      Clock
	nextSid    int64
	sessions   map[int64]*session
	ephByOwner map[int64]map[string]struct{}
	watches    map[string][]watch
	pending    map[int64][]Event
	waiters    map[int64]chan struct{}
}

type session struct {
	id       int64
	timeout  time.Duration
	lastPing time.Time
}

type node struct {
	data           []byte
	czxid          int64
	mzxid          int64
	version        int32
	cversion       int32
	seq            int32
	ephemeralOwner int64
	children       map[string]*node
}

func New() *Store {
	return NewWithClock(realClock{})
}

func NewWithClock(c Clock) *Store {
	if c == nil {
		c = realClock{}
	}
	return &Store{
		root:       &node{children: map[string]*node{}},
		clock:      c,
		sessions:   map[int64]*session{},
		ephByOwner: map[int64]map[string]struct{}{},
		watches:    map[string][]watch{},
		pending:    map[int64][]Event{},
		waiters:    map[int64]chan struct{}{},
	}
}

func (s *Store) Zxid() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zxid
}

func (s *Store) Create(path string, data []byte, flags CreateFlags) (string, Stat, error) {
	path, err := cleanPath(path)
	if err != nil {
		return "", Stat{}, err
	}
	if path == "/" {
		return "", Stat{}, ErrNodeExists
	}
	parentPath, name := split(path)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()

	parent, err := s.lookupLocked(parentPath)
	if err != nil {
		if errors.Is(err, ErrNoNode) {
			return "", Stat{}, ErrNoParent
		}
		return "", Stat{}, err
	}
	if parent.ephemeralOwner != 0 {
		return "", Stat{}, ErrEphemeralChild
	}
	if flags.Ephemeral {
		if flags.SessionID == 0 {
			return "", Stat{}, ErrNotEphemeral
		}
		if _, ok := s.sessions[flags.SessionID]; !ok {
			return "", Stat{}, ErrNoSession
		}
	}
	if flags.Sequential {
		name = fmt.Sprintf("%s-%010d", name, parent.seq)
		parent.seq++
		if parentPath == "/" {
			path = "/" + name
		} else {
			path = parentPath + "/" + name
		}
	}
	if _, exists := parent.children[name]; exists {
		return "", Stat{}, ErrNodeExists
	}
	zxid := s.nextZxidLocked()
	n := &node{
		data:     clone(data),
		czxid:    zxid,
		mzxid:    zxid,
		children: map[string]*node{},
	}
	if flags.Ephemeral {
		n.ephemeralOwner = flags.SessionID
		if s.ephByOwner[flags.SessionID] == nil {
			s.ephByOwner[flags.SessionID] = map[string]struct{}{}
		}
		s.ephByOwner[flags.SessionID][path] = struct{}{}
	}
	parent.children[name] = n
	parent.cversion++
	s.fireLocked(parentPath, WatchChildren, Event{Type: "NodeChildrenChanged", Path: parentPath, Zxid: zxid})
	return path, n.stat(), nil
}

func (s *Store) Get(path string) ([]byte, Stat, error) {
	path, err := cleanPath(path)
	if err != nil {
		return nil, Stat{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	n, err := s.lookupLocked(path)
	if err != nil {
		return nil, Stat{}, err
	}
	return clone(n.data), n.stat(), nil
}

func (s *Store) Set(path string, data []byte, version *int32) (Stat, error) {
	path, err := cleanPath(path)
	if err != nil {
		return Stat{}, err
	}
	if path == "/" {
		return Stat{}, ErrRoot
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	n, err := s.lookupLocked(path)
	if err != nil {
		return Stat{}, err
	}
	if version != nil && n.version != *version {
		return Stat{}, ErrBadVersion
	}
	zxid := s.nextZxidLocked()
	n.data = clone(data)
	n.mzxid = zxid
	n.version++
	s.fireLocked(path, WatchData, Event{Type: "NodeDataChanged", Path: path, Zxid: zxid})
	return n.stat(), nil
}

func (s *Store) Delete(path string, version *int32) error {
	path, err := cleanPath(path)
	if err != nil {
		return err
	}
	if path == "/" {
		return ErrRoot
	}
	parentPath, name := split(path)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()

	parent, err := s.lookupLocked(parentPath)
	if err != nil {
		return err
	}
	child, ok := parent.children[name]
	if !ok {
		return ErrNoNode
	}
	if version != nil && child.version != *version {
		return ErrBadVersion
	}
	if len(child.children) > 0 {
		return ErrNotEmpty
	}
	s.nextZxidLocked()
	delete(parent.children, name)
	parent.cversion++
	if child.ephemeralOwner != 0 {
		delete(s.ephByOwner[child.ephemeralOwner], path)
	}
	s.fireLocked(path, WatchData, Event{Type: "NodeDeleted", Path: path, Zxid: s.zxid})
	s.fireLocked(parentPath, WatchChildren, Event{Type: "NodeChildrenChanged", Path: parentPath, Zxid: s.zxid})
	return nil
}

func (s *Store) Children(path string) ([]string, Stat, error) {
	path, err := cleanPath(path)
	if err != nil {
		return nil, Stat{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	n, err := s.lookupLocked(path)
	if err != nil {
		return nil, Stat{}, err
	}
	out := make([]string, 0, len(n.children))
	for name := range n.children {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, n.stat(), nil
}

func (s *Store) nextZxidLocked() int64 {
	s.zxid++
	return s.zxid
}

func (n *node) stat() Stat {
	return Stat{
		Czxid:          n.czxid,
		Mzxid:          n.mzxid,
		Version:        n.version,
		Cversion:       n.cversion,
		DataLength:     len(n.data),
		NumChildren:    len(n.children),
		EphemeralOwner: n.ephemeralOwner,
	}
}

func (s *Store) CreateSession(timeout time.Duration) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	s.nextSid++
	id := s.nextSid
	s.sessions[id] = &session{id: id, timeout: timeout, lastPing: s.clock.Now()}
	return id
}

func (s *Store) Ping(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	sess, ok := s.sessions[id]
	if !ok {
		return ErrNoSession
	}
	sess.lastPing = s.clock.Now()
	return nil
}

func (s *Store) CloseSession(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	if _, ok := s.sessions[id]; !ok {
		return ErrNoSession
	}
	s.closeSessionLocked(id)
	return nil
}

func (s *Store) Expire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
}

func (s *Store) expireLocked() {
	now := s.clock.Now()
	var dead []int64
	for id, sess := range s.sessions {
		if now.Sub(sess.lastPing) >= sess.timeout {
			dead = append(dead, id)
		}
	}
	for _, id := range dead {
		s.closeSessionLocked(id)
	}
}

func (s *Store) closeSessionLocked(id int64) {
	for path := range s.ephByOwner[id] {
		s.deleteEphemeralLocked(path)
	}
	delete(s.ephByOwner, id)
	delete(s.sessions, id)
	s.dropSessionWatchesLocked(id)
}

func (s *Store) deleteEphemeralLocked(path string) {
	parentPath, name := split(path)
	parent, err := s.lookupLocked(parentPath)
	if err != nil {
		return
	}
	if _, ok := parent.children[name]; !ok {
		return
	}
	s.nextZxidLocked()
	delete(parent.children, name)
	parent.cversion++
	s.fireLocked(path, WatchData, Event{Type: "NodeDeleted", Path: path, Zxid: s.zxid})
	s.fireLocked(parentPath, WatchChildren, Event{Type: "NodeChildrenChanged", Path: parentPath, Zxid: s.zxid})
}

func (s *Store) lookupLocked(path string) (*node, error) {
	if path == "/" {
		return s.root, nil
	}
	cur := s.root
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, ok := cur.children[part]
		if !ok {
			return nil, ErrNoNode
		}
		cur = next
	}
	return cur, nil
}

func cleanPath(path string) (string, error) {
	if path == "" || path[0] != '/' {
		return "", ErrBadPath
	}
	if path == "/" {
		return "/", nil
	}
	if strings.HasSuffix(path, "/") {
		return "", ErrBadPath
	}
	parts := strings.Split(path, "/")
	if parts[0] != "" {
		return "", ErrBadPath
	}
	for _, p := range parts[1:] {
		if p == "" || p == "." || p == ".." {
			return "", ErrBadPath
		}
	}
	return path, nil
}

func split(path string) (parent, name string) {
	i := strings.LastIndex(path, "/")
	if i == 0 {
		return "/", path[1:]
	}
	return path[:i], path[i+1:]
}

func clone(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
