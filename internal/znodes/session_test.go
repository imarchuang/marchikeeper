package znodes

import (
	"errors"
	"testing"
	"time"
)

func TestEphemeralExpiresWithFakeClock(t *testing.T) {
	clk := NewFakeClock(time.Unix(1000, 0).UTC())
	s := NewWithClock(clk)
	if _, _, err := s.Create("/workers", nil, CreateFlags{}); err != nil {
		t.Fatal(err)
	}
	sid := s.CreateSession(100 * time.Millisecond)
	path, st, err := s.Create("/workers/w", []byte(`{"who":"a"}`), CreateFlags{Ephemeral: true, SessionID: sid})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/workers/w" || st.EphemeralOwner != sid {
		t.Fatalf("path=%s stat=%+v", path, st)
	}
	kids, _, err := s.Children("/workers")
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 1 {
		t.Fatalf("children before expire = %v", kids)
	}

	clk.Advance(100 * time.Millisecond)
	if _, _, err := s.Get("/workers/w"); !errors.Is(err, ErrNoNode) {
		t.Fatalf("get after expire: %v", err)
	}
	kids, _, err = s.Children("/workers")
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 0 {
		t.Fatalf("children after expire = %v", kids)
	}
}

func TestPingKeepsEphemeral(t *testing.T) {
	clk := NewFakeClock(time.Unix(1000, 0).UTC())
	s := NewWithClock(clk)
	if _, _, err := s.Create("/w", nil, CreateFlags{}); err != nil {
		t.Fatal(err)
	}
	sid := s.CreateSession(100 * time.Millisecond)
	if _, _, err := s.Create("/w/a", nil, CreateFlags{Ephemeral: true, SessionID: sid}); err != nil {
		t.Fatal(err)
	}
	clk.Advance(50 * time.Millisecond)
	if err := s.Ping(sid); err != nil {
		t.Fatal(err)
	}
	clk.Advance(50 * time.Millisecond)
	if _, _, err := s.Get("/w/a"); err != nil {
		t.Fatal(err)
	}
	clk.Advance(100 * time.Millisecond)
	if _, _, err := s.Get("/w/a"); !errors.Is(err, ErrNoNode) {
		t.Fatalf("expected expired, got %v", err)
	}
}

func TestCloseSessionDeletesEphemeral(t *testing.T) {
	s := New()
	if _, _, err := s.Create("/e", nil, CreateFlags{}); err != nil {
		t.Fatal(err)
	}
	sid := s.CreateSession(time.Second)
	if _, _, err := s.Create("/e/x", nil, CreateFlags{Ephemeral: true, SessionID: sid}); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseSession(sid); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("/e/x"); !errors.Is(err, ErrNoNode) {
		t.Fatalf("got %v", err)
	}
}

func TestEphemeralRequiresSession(t *testing.T) {
	s := New()
	if _, _, err := s.Create("/x", nil, CreateFlags{Ephemeral: true}); !errors.Is(err, ErrNotEphemeral) {
		t.Fatalf("err = %v", err)
	}
}
