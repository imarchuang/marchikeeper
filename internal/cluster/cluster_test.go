package cluster

import (
	"errors"
	"testing"

	"github.com/marchi/marchikeeper/internal/znodes"
)

func TestFollowersApplyByZxidAndSurviveFailover(t *testing.T) {
	c := New(3)
	if _, _, err := c.Create("/app", []byte(`{"ok":true}`), znodes.CreateFlags{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Create("/app/w", []byte(`{"n":1}`), znodes.CreateFlags{}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		data, st, err := c.GetFrom(i, "/app/w")
		if err != nil {
			t.Fatalf("node %d: %v", i, err)
		}
		if string(data) != `{"n":1}` {
			t.Fatalf("node %d data %s", i, data)
		}
		if st.Czxid != 2 {
			t.Fatalf("node %d czxid=%d", i, st.Czxid)
		}
		if c.Nodes[i].Store.Zxid() != 2 {
			t.Fatalf("node %d zxid=%d", i, c.Nodes[i].Store.Zxid())
		}
	}

	if err := c.FailLeader(); err != nil {
		t.Fatal(err)
	}
	if c.Leader().ID == 1 {
		t.Fatal("leader should have changed")
	}
	data, _, err := c.Leader().Store.Get("/app/w")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"n":1}` {
		t.Fatalf("committed znode lost after failover: %s", data)
	}

	if _, err := c.Set("/app/w", []byte(`{"n":2}`), nil); err != nil {
		t.Fatal(err)
	}
	for i, n := range c.Nodes {
		if n.Down {
			continue
		}
		got, _, err := n.Store.Get("/app/w")
		if err != nil {
			t.Fatalf("node %d after set: %v", i, err)
		}
		if string(got) != `{"n":2}` {
			t.Fatalf("node %d data %s", i, got)
		}
	}
}

func TestApplyRejectsZxidGap(t *testing.T) {
	s := znodes.New()
	err := s.ApplyTxn(znodes.Txn{Zxid: 2, Op: "create", Path: "/x"})
	if !errors.Is(err, znodes.ErrZxidGap) {
		t.Fatalf("err = %v", err)
	}
}
