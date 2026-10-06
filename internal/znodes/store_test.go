package znodes

import (
	"errors"
	"testing"
)

func TestCreateGetSetDeleteChildren(t *testing.T) {
	s := New()

	if _, _, err := s.Create("/app", []byte(`{"k":1}`), CreateFlags{}); err != nil {
		t.Fatal(err)
	}
	got, st, err := s.Get("/app")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"k":1}` {
		t.Fatalf("get = %q", got)
	}
	if st.Czxid != 1 || st.Mzxid != 1 || st.Version != 0 {
		t.Fatalf("create stat = %+v", st)
	}

	st, err = s.Set("/app", []byte(`{"k":2}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != 1 || st.Mzxid != 2 || st.Czxid != 1 {
		t.Fatalf("set stat = %+v", st)
	}
	got, _, err = s.Get("/app")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"k":2}` {
		t.Fatalf("get after set = %q", got)
	}

	if _, _, err := s.Create("/app/workers", nil, CreateFlags{}); err != nil {
		t.Fatal(err)
	}
	kids, pst, err := s.Children("/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 1 || kids[0] != "workers" {
		t.Fatalf("children = %v", kids)
	}
	if pst.Cversion != 1 || pst.NumChildren != 1 {
		t.Fatalf("parent stat = %+v", pst)
	}

	if err := s.Delete("/app", nil); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("delete parent: %v", err)
	}
	if err := s.Delete("/app/workers", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("/app", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("/app"); !errors.Is(err, ErrNoNode) {
		t.Fatalf("get after delete: %v", err)
	}
}

func TestVersionCAS(t *testing.T) {
	s := New()
	if _, _, err := s.Create("/n", []byte("a"), CreateFlags{}); err != nil {
		t.Fatal(err)
	}
	wrong := int32(5)
	if _, err := s.Set("/n", []byte("b"), &wrong); !errors.Is(err, ErrBadVersion) {
		t.Fatalf("set mismatch: %v", err)
	}
	zero := int32(0)
	st, err := s.Set("/n", []byte("b"), &zero)
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != 1 {
		t.Fatalf("version = %d", st.Version)
	}
	if err := s.Delete("/n", &zero); !errors.Is(err, ErrBadVersion) {
		t.Fatalf("delete mismatch: %v", err)
	}
	one := int32(1)
	if err := s.Delete("/n", &one); err != nil {
		t.Fatal(err)
	}
}

func TestZxidMonotonic(t *testing.T) {
	s := New()
	_, a, err := s.Create("/a", nil, CreateFlags{})
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := s.Create("/b", nil, CreateFlags{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Set("/a", []byte("x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !(a.Czxid < b.Czxid && b.Czxid < c.Mzxid) {
		t.Fatalf("zxids a=%d b=%d set=%d", a.Czxid, b.Czxid, c.Mzxid)
	}
	if s.Zxid() != 3 {
		t.Fatalf("zxid = %d", s.Zxid())
	}
}

func TestCreateRequiresParent(t *testing.T) {
	s := New()
	if _, _, err := s.Create("/a/b", nil, CreateFlags{}); !errors.Is(err, ErrNoParent) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateDuplicate(t *testing.T) {
	s := New()
	if _, _, err := s.Create("/x", nil, CreateFlags{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Create("/x", nil, CreateFlags{}); !errors.Is(err, ErrNodeExists) {
		t.Fatalf("err = %v", err)
	}
}

func TestRootAlwaysExists(t *testing.T) {
	s := New()
	data, _, err := s.Get("/")
	if err != nil {
		t.Fatal(err)
	}
	if data != nil {
		t.Fatalf("root data = %q", data)
	}
	if _, _, err := s.Create("/", nil, CreateFlags{}); !errors.Is(err, ErrNodeExists) {
		t.Fatalf("create root: %v", err)
	}
	if err := s.Delete("/", nil); !errors.Is(err, ErrRoot) {
		t.Fatalf("delete root: %v", err)
	}
}

func TestSequentialSuffix(t *testing.T) {
	s := New()
	if _, _, err := s.Create("/lock", nil, CreateFlags{}); err != nil {
		t.Fatal(err)
	}
	var names []string
	for i := 0; i < 3; i++ {
		p, _, err := s.Create("/lock/guid", []byte("x"), CreateFlags{Sequential: true})
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, p)
	}
	want := []string{"/lock/guid-0000000000", "/lock/guid-0000000001", "/lock/guid-0000000002"}
	if len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Fatalf("names = %v want %v", names, want)
	}
	kids, _, err := s.Children("/lock")
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 3 || kids[0] != "guid-0000000000" {
		t.Fatalf("children = %v", kids)
	}
}
