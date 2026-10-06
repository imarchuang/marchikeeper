package znodes

import (
	"testing"
	"time"
)

func TestOneShotWatchMustRearm(t *testing.T) {
	s := New()
	if _, _, err := s.Create("/n", []byte("a"), CreateFlags{}); err != nil {
		t.Fatal(err)
	}
	sid := s.CreateSession(time.Second)
	if err := s.ArmWatch("/n", WatchData, sid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set("/n", []byte("b"), nil); err != nil {
		t.Fatal(err)
	}
	ev, err := s.WaitEvents(sid, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 1 || ev[0].Type != "NodeDataChanged" || ev[0].Path != "/n" {
		t.Fatalf("events = %#v", ev)
	}

	if _, err := s.Set("/n", []byte("c"), nil); err != nil {
		t.Fatal(err)
	}
	ev, err = s.WaitEvents(sid, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 0 {
		t.Fatalf("second mutate re-fired: %#v", ev)
	}

	if err := s.ArmWatch("/n", WatchData, sid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set("/n", []byte("d"), nil); err != nil {
		t.Fatal(err)
	}
	ev, err = s.WaitEvents(sid, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 1 {
		t.Fatalf("re-arm events = %#v", ev)
	}
}

func TestWatchChildrenSequentialCreate(t *testing.T) {
	s := New()
	if _, _, err := s.Create("/election", nil, CreateFlags{}); err != nil {
		t.Fatal(err)
	}
	sid := s.CreateSession(time.Second)
	if err := s.ArmWatch("/election", WatchChildren, sid); err != nil {
		t.Fatal(err)
	}

	done := make(chan []Event, 1)
	go func() {
		ev, err := s.WaitEvents(sid, time.Second)
		if err != nil {
			t.Errorf("wait: %v", err)
		}
		done <- ev
	}()
	time.Sleep(20 * time.Millisecond)
	path, _, err := s.Create("/election/n", nil, CreateFlags{Sequential: true})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/election/n-0000000000" {
		t.Fatalf("path = %s", path)
	}
	select {
	case ev := <-done:
		if len(ev) != 1 || ev[0].Type != "NodeChildrenChanged" {
			t.Fatalf("events = %#v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not wake")
	}
}
