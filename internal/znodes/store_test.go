package znodes

import (
	"errors"
	"testing"
)

func TestCreateGetSetDeleteChildren(t *testing.T) {
	s := New()

	if _, err := s.Create("/app", []byte(`{"k":1}`)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("/app")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"k":1}` {
		t.Fatalf("get = %q", got)
	}

	if err := s.Set("/app", []byte(`{"k":2}`)); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get("/app")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"k":2}` {
		t.Fatalf("get after set = %q", got)
	}

	if _, err := s.Create("/app/workers", nil); err != nil {
		t.Fatal(err)
	}
	kids, err := s.Children("/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 1 || kids[0] != "workers" {
		t.Fatalf("children = %v", kids)
	}

	if err := s.Delete("/app"); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("delete parent: %v", err)
	}
	if err := s.Delete("/app/workers"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("/app"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("/app"); !errors.Is(err, ErrNoNode) {
		t.Fatalf("get after delete: %v", err)
	}
}

func TestCreateRequiresParent(t *testing.T) {
	s := New()
	if _, err := s.Create("/a/b", nil); !errors.Is(err, ErrNoParent) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateDuplicate(t *testing.T) {
	s := New()
	if _, err := s.Create("/x", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("/x", nil); !errors.Is(err, ErrNodeExists) {
		t.Fatalf("err = %v", err)
	}
}

func TestRootAlwaysExists(t *testing.T) {
	s := New()
	data, err := s.Get("/")
	if err != nil {
		t.Fatal(err)
	}
	if data != nil {
		t.Fatalf("root data = %q", data)
	}
	if _, err := s.Create("/", nil); !errors.Is(err, ErrNodeExists) {
		t.Fatalf("create root: %v", err)
	}
	if err := s.Delete("/"); !errors.Is(err, ErrRoot) {
		t.Fatalf("delete root: %v", err)
	}
}
