package latch

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/marchi/marchikeeper/internal/znodes"
)

// Offer creates an ephemeral sequential candidate under parent.
func Offer(store *znodes.Store, sessionID int64, parent, prefix string) (string, error) {
	if _, _, err := store.Get(parent); err != nil {
		if _, _, err := store.Create(parent, nil, znodes.CreateFlags{}); err != nil {
			return "", err
		}
	}
	path := parent
	if parent == "/" {
		path = "/" + prefix
	} else {
		path = parent + "/" + prefix
	}
	created, _, err := store.Create(path, []byte(fmt.Sprintf(`{"sessionId":%d}`, sessionID)), znodes.CreateFlags{
		Ephemeral:  true,
		Sequential: true,
		SessionID:  sessionID,
	})
	return created, err
}

// IsLeader reports whether myPath is the lowest sequential child under its parent.
func IsLeader(store *znodes.Store, myPath string) (bool, string, error) {
	parent, name := splitPath(myPath)
	kids, _, err := store.Children(parent)
	if err != nil {
		return false, "", err
	}
	if len(kids) == 0 {
		return false, "", fmt.Errorf("no children under %s", parent)
	}
	if kids[0] == name {
		return true, "", nil
	}
	pred := ""
	for i, k := range kids {
		if k == name {
			if i == 0 {
				return true, "", nil
			}
			pred = parent + "/" + kids[i-1]
			break
		}
	}
	if pred == "" {
		return false, "", fmt.Errorf("%s not in children %v", myPath, kids)
	}
	return false, pred, nil
}

// WaitUntilLeader watches the predecessor (herd avoidance) until this node is first.
func WaitUntilLeader(store *znodes.Store, sessionID int64, myPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		ok, pred, err := IsLeader(store, myPath)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		remain := time.Until(deadline)
		if remain <= 0 {
			return fmt.Errorf("timeout waiting for leadership")
		}
		if err := store.ArmWatch(pred, znodes.WatchData, sessionID); err != nil {
			if errors.Is(err, znodes.ErrNoNode) {
				continue
			}
			return err
		}
		ok, _, err = IsLeader(store, myPath)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if _, err := store.WaitEvents(sessionID, remain); err != nil {
			return err
		}
	}
}

func splitPath(path string) (parent, name string) {
	i := strings.LastIndex(path, "/")
	if i <= 0 {
		return "/", path[1:]
	}
	return path[:i], path[i+1:]
}
