package latch

import (
	"testing"
	"time"

	"github.com/marchi/marchikeeper/internal/znodes"
)

func TestStandbyBecomesLeaderAfterSessionClose(t *testing.T) {
	store := znodes.New()
	a := store.CreateSession(5 * time.Second)
	b := store.CreateSession(5 * time.Second)

	pathA, err := Offer(store, a, "/election", "n")
	if err != nil {
		t.Fatal(err)
	}
	pathB, err := Offer(store, b, "/election", "n")
	if err != nil {
		t.Fatal(err)
	}

	leader, pred, err := IsLeader(store, pathA)
	if err != nil {
		t.Fatal(err)
	}
	if !leader || pred != "" {
		t.Fatalf("A should be leader, pred=%q", pred)
	}
	leader, pred, err = IsLeader(store, pathB)
	if err != nil {
		t.Fatal(err)
	}
	if leader || pred != pathA {
		t.Fatalf("B should watch A, leader=%v pred=%q", leader, pred)
	}

	done := make(chan error, 1)
	go func() {
		done <- WaitUntilLeader(store, b, pathB, 2*time.Second)
	}()
	time.Sleep(30 * time.Millisecond)
	if err := store.CloseSession(a); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("B did not become leader")
	}
	ok, _, err := IsLeader(store, pathB)
	if err != nil || !ok {
		t.Fatalf("B not leader after failover: ok=%v err=%v", ok, err)
	}
}
