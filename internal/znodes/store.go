package znodes

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

var (
	ErrNoNode       = errors.New("no node")
	ErrNodeExists   = errors.New("node exists")
	ErrNotEmpty     = errors.New("not empty")
	ErrBadPath      = errors.New("bad path")
	ErrRoot         = errors.New("cannot mutate root")
	ErrNoParent     = errors.New("no parent")
	ErrBadVersion   = errors.New("bad version")
	ErrNotEphemeral = errors.New("session required for ephemeral")
)

// Stat is the ZooKeeper-inspired metadata for a znode.
type Stat struct {
	Czxid       int64 `json:"czxid"`
	Mzxid       int64 `json:"mzxid"`
	Version     int32 `json:"version"`
	Cversion    int32 `json:"cversion"`
	DataLength  int   `json:"dataLength"`
	NumChildren int   `json:"numChildren"`
}

// Store is an in-memory znode tree. Root "/" always exists.
type Store struct {
	mu   sync.Mutex
	zxid int64
	root *node
}

type node struct {
	data     []byte
	czxid    int64
	mzxid    int64
	version  int32
	cversion int32
	children map[string]*node
}

func New() *Store {
	return &Store{root: &node{children: map[string]*node{}}}
}

func (s *Store) Zxid() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zxid
}

func (s *Store) Create(path string, data []byte) (string, Stat, error) {
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

	parent, err := s.lookupLocked(parentPath)
	if err != nil {
		if errors.Is(err, ErrNoNode) {
			return "", Stat{}, ErrNoParent
		}
		return "", Stat{}, err
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
	parent.children[name] = n
	parent.cversion++
	return path, n.stat(), nil
}

func (s *Store) Get(path string) ([]byte, Stat, error) {
	path, err := cleanPath(path)
	if err != nil {
		return nil, Stat{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
	return nil
}

func (s *Store) Children(path string) ([]string, Stat, error) {
	path, err := cleanPath(path)
	if err != nil {
		return nil, Stat{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
		Czxid:       n.czxid,
		Mzxid:       n.mzxid,
		Version:     n.version,
		Cversion:    n.cversion,
		DataLength:  len(n.data),
		NumChildren: len(n.children),
	}
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
