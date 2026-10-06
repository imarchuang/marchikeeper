package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
