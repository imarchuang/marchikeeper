package cluster

import (
	"errors"
	"fmt"

	"github.com/marchi/marchikeeper/internal/znodes"
)

var ErrNoLeader = errors.New("no leader")

// Node is one replica. Public znode HTTP does not expose Raft vocabulary.
type Node struct {
	ID    int
	Store *znodes.Store
	Down  bool
}

// Cluster is an in-process majority broadcast: leader writes, followers apply by zxid.
type Cluster struct {
	Nodes  []*Node
	leader int
}

func New(n int) *Cluster {
	c := &Cluster{leader: 0}
	for i := 0; i < n; i++ {
		c.Nodes = append(c.Nodes, &Node{ID: i + 1, Store: znodes.New()})
	}
	return c
}

func (c *Cluster) Leader() *Node {
	if c.leader < 0 || c.leader >= len(c.Nodes) {
		return nil
	}
	n := c.Nodes[c.leader]
	if n.Down {
		return nil
	}
	return n
}

func (c *Cluster) majority() int {
	return len(c.Nodes)/2 + 1
}

func (c *Cluster) Create(path string, data []byte, flags znodes.CreateFlags) (string, znodes.Stat, error) {
	lead := c.Leader()
	if lead == nil {
		return "", znodes.Stat{}, ErrNoLeader
	}
	created, st, err := lead.Store.Create(path, data, flags)
	if err != nil {
		return "", znodes.Stat{}, err
	}
	err = c.broadcast(znodes.Txn{
		Zxid:      st.Czxid,
		Op:        "create",
		Path:      created,
		Data:      data,
		SessionID: flags.SessionID,
		Ephemeral: flags.Ephemeral,
	})
	return created, st, err
}

func (c *Cluster) Set(path string, data []byte, version *int32) (znodes.Stat, error) {
	lead := c.Leader()
	if lead == nil {
		return znodes.Stat{}, ErrNoLeader
	}
	st, err := lead.Store.Set(path, data, version)
	if err != nil {
		return znodes.Stat{}, err
	}
	err = c.broadcast(znodes.Txn{Zxid: st.Mzxid, Op: "set", Path: path, Data: data})
	return st, err
}

func (c *Cluster) Delete(path string, version *int32) error {
	lead := c.Leader()
	if lead == nil {
		return ErrNoLeader
	}
	if err := lead.Store.Delete(path, version); err != nil {
		return err
	}
	return c.broadcast(znodes.Txn{Zxid: lead.Store.Zxid(), Op: "delete", Path: path})
}

func (c *Cluster) GetFrom(nodeIdx int, path string) ([]byte, znodes.Stat, error) {
	n := c.Nodes[nodeIdx]
	if n.Down {
		return nil, znodes.Stat{}, fmt.Errorf("node %d down", n.ID)
	}
	return n.Store.Get(path)
}

func (c *Cluster) broadcast(t znodes.Txn) error {
	acks := 1
	for i, n := range c.Nodes {
		if i == c.leader || n.Down {
			continue
		}
		if err := n.Store.ApplyTxn(t); err != nil {
			continue
		}
		acks++
	}
	if acks < c.majority() {
		return fmt.Errorf("no majority ack (acks=%d)", acks)
	}
	return nil
}

// FailLeader marks the current leader down and elects the live replica with the highest zxid.
func (c *Cluster) FailLeader() error {
	if lead := c.Leader(); lead != nil {
		lead.Down = true
	}
	best := -1
	var bestZxid int64 = -1
	for i, n := range c.Nodes {
		if n.Down {
			continue
		}
		z := n.Store.Zxid()
		if z > bestZxid {
			bestZxid = z
			best = i
		}
	}
	if best < 0 {
		c.leader = -1
		return ErrNoLeader
	}
	c.leader = best
	return nil
}
