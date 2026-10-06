package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marchi/marchikeeper/internal/znodes"
)

func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(New(nil))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestHTTPZnodeCRUD(t *testing.T) {
	srv := httptest.NewServer(New(nil))
	defer srv.Close()

	put := do(t, http.MethodPut, srv.URL+"/znodes/app", `{"v":1}`)
	if put.StatusCode != http.StatusCreated {
		t.Fatalf("create status %d body %s", put.StatusCode, readAll(t, put))
	}

	get := do(t, http.MethodGet, srv.URL+"/znodes/app", "")
	if get.StatusCode != 200 {
		t.Fatalf("get status %d", get.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(get.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	_ = get.Body.Close()
	data, _ := body["data"].(map[string]any)
	if data["v"].(float64) != 1 {
		t.Fatalf("data = %#v", body["data"])
	}

	set := do(t, http.MethodPost, srv.URL+"/znodes/app", `{"v":2}`)
	if set.StatusCode != 200 {
		t.Fatalf("set status %d %s", set.StatusCode, readAll(t, set))
	}

	if res := do(t, http.MethodPut, srv.URL+"/znodes/app/w1", `{}`); res.StatusCode != 201 {
		t.Fatalf("create child %d %s", res.StatusCode, readAll(t, res))
	}

	kids := do(t, http.MethodGet, srv.URL+"/znodes/app/children", "")
	if kids.StatusCode != 200 {
		t.Fatalf("children status %d", kids.StatusCode)
	}
	var listed map[string]any
	if err := json.NewDecoder(kids.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	_ = kids.Body.Close()
	ch, _ := listed["children"].([]any)
	if len(ch) != 1 || ch[0].(string) != "w1" {
		t.Fatalf("children = %#v", listed["children"])
	}

	if res := do(t, http.MethodDelete, srv.URL+"/znodes/app/w1", ""); res.StatusCode != 200 {
		t.Fatalf("delete child %d %s", res.StatusCode, readAll(t, res))
	}
	if res := do(t, http.MethodDelete, srv.URL+"/znodes/app", ""); res.StatusCode != 200 {
		t.Fatalf("delete %d %s", res.StatusCode, readAll(t, res))
	}
	if res := do(t, http.MethodGet, srv.URL+"/znodes/app", ""); res.StatusCode != 404 {
		t.Fatalf("get deleted %d", res.StatusCode)
	}
}

func TestHTTPVersionCAS(t *testing.T) {
	srv := httptest.NewServer(New(nil))
	defer srv.Close()

	created := do(t, http.MethodPut, srv.URL+"/znodes/n", `"a"`)
	if created.StatusCode != 201 {
		t.Fatalf("create %d %s", created.StatusCode, readAll(t, created))
	}
	_ = created.Body.Close()

	mismatch := do(t, http.MethodPost, srv.URL+"/znodes/n?version=9", `"b"`)
	if mismatch.StatusCode != http.StatusConflict {
		t.Fatalf("mismatch status %d %s", mismatch.StatusCode, readAll(t, mismatch))
	}
	_ = mismatch.Body.Close()

	ok := do(t, http.MethodPost, srv.URL+"/znodes/n?version=0", `"b"`)
	if ok.StatusCode != 200 {
		t.Fatalf("cas set %d %s", ok.StatusCode, readAll(t, ok))
	}
	var body map[string]any
	if err := json.NewDecoder(ok.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	_ = ok.Body.Close()
	st, _ := body["stat"].(map[string]any)
	if st["version"].(float64) != 1 || st["mzxid"].(float64) != 2 {
		t.Fatalf("stat = %#v", st)
	}

	delBad := do(t, http.MethodDelete, srv.URL+"/znodes/n?version=0", "")
	if delBad.StatusCode != http.StatusConflict {
		t.Fatalf("delete mismatch %d %s", delBad.StatusCode, readAll(t, delBad))
	}
	_ = delBad.Body.Close()
}

func TestHTTPSequential(t *testing.T) {
	srv := httptest.NewServer(New(nil))
	defer srv.Close()

	if res := do(t, http.MethodPut, srv.URL+"/znodes/lock", `{}`); res.StatusCode != 201 {
		t.Fatalf("parent %d %s", res.StatusCode, readAll(t, res))
	}
	var paths []string
	for i := 0; i < 3; i++ {
		res := do(t, http.MethodPut, srv.URL+"/znodes/lock/guid?sequential=1", `"c"`)
		if res.StatusCode != 201 {
			t.Fatalf("seq create %d %s", res.StatusCode, readAll(t, res))
		}
		var body map[string]any
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		paths = append(paths, body["path"].(string))
	}
	if paths[0] != "/lock/guid-0000000000" || paths[2] != "/lock/guid-0000000002" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestHTTPEphemeralExpire(t *testing.T) {
	clk := znodes.NewFakeClock(time.Unix(1, 0).UTC())
	store := znodes.NewWithClock(clk)
	srv := httptest.NewServer(New(store))
	defer srv.Close()

	if res := do(t, http.MethodPut, srv.URL+"/znodes/workers", `{}`); res.StatusCode != 201 {
		t.Fatalf("parent %d %s", res.StatusCode, readAll(t, res))
	}

	sess := do(t, http.MethodPost, srv.URL+"/sessions", `{"timeoutMs":100}`)
	if sess.StatusCode != 201 {
		t.Fatalf("session %d %s", sess.StatusCode, readAll(t, sess))
	}
	var sbody map[string]any
	if err := json.NewDecoder(sess.Body).Decode(&sbody); err != nil {
		t.Fatal(err)
	}
	_ = sess.Body.Close()
	sid := int64(sbody["sessionId"].(float64))

	req, err := http.NewRequest(http.MethodPut, srv.URL+"/znodes/workers/w1?ephemeral=1", strings.NewReader(`{"who":"a"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Session-Id", strconv.FormatInt(sid, 10))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 201 {
		t.Fatalf("eph create %d %s", res.StatusCode, readAll(t, res))
	}
	_ = res.Body.Close()

	clk.Advance(100 * time.Millisecond)
	gone := do(t, http.MethodGet, srv.URL+"/znodes/workers/w1", "")
	if gone.StatusCode != 404 {
		t.Fatalf("expected 404 after expire, got %d %s", gone.StatusCode, readAll(t, gone))
	}
	_ = gone.Body.Close()
}

func do(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func readAll(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
