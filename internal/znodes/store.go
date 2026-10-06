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

// Store is an in-memory znode tree. Root "/" always exists.
type Store struct {
	mu   sync.Mutex
	root *node
}

type node struct {
	data     []byte
	children map[string]*node
}

func New() *Store {
	return &Store{root: &node{children: map[string]*node{}}}
}

func (s *Store) Create(path string, data []byte) (string, error) {
	path, err := cleanPath(path)
	if err != nil {
		return "", err
	}
	if path == "/" {
		return "", ErrNodeExists
	}
	parentPath, name := split(path)

	s.mu.Lock()
	defer s.mu.Unlock()

	parent, err := s.lookupLocked(parentPath)
	if err != nil {
		if errors.Is(err, ErrNoNode) {
			return "", ErrNoParent
		}
		return "", err
	}
	if _, exists := parent.children[name]; exists {
		return "", ErrNodeExists
	}
	parent.children[name] = &node{
		data:     clone(data),
		children: map[string]*node{},
	}
	return path, nil
}

func (s *Store) Get(path string) ([]byte, error) {
	path, err := cleanPath(path)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.lookupLocked(path)
	if err != nil {
		return nil, err
	}
	return clone(n.data), nil
}

func (s *Store) Set(path string, data []byte) error {
	path, err := cleanPath(path)
	if err != nil {
		return err
	}
	if path == "/" {
		return ErrRoot
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.lookupLocked(path)
	if err != nil {
		return err
	}
	n.data = clone(data)
	return nil
}

func (s *Store) Delete(path string) error {
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
	if len(child.children) > 0 {
		return ErrNotEmpty
	}
	delete(parent.children, name)
	return nil
}

func (s *Store) Children(path string) ([]string, error) {
	path, err := cleanPath(path)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.lookupLocked(path)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(n.children))
	for name := range n.children {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
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
