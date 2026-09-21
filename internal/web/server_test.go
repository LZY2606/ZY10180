package web

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"ttsbench/internal/store"
	"ttsbench/internal/tts"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveRun(tts.NewRun(1183)); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(st).Handler())
	t.Cleanup(func() {
		srv.Close()
		st.Close()
	})
	return srv, st
}

func get(t *testing.T, srv *httptest.Server, path string) (int, string) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestIndexShowsTitle(t *testing.T) {
	srv, _ := newTestServer(t)
	code, body := get(t, srv, "/")
	if code != http.StatusOK {
		t.Fatalf("GET / = %d", code)
	}
	if !strings.Contains(body, "流变叠时台") {
		t.Fatal("index must show the workbench title")
	}
	if !strings.Contains(body, "不可识别") {
		t.Fatal("index must surface the unidentifiable-shift diagnostic")
	}
	if !strings.Contains(body, "模量非正") {
		t.Fatal("index must list the non-positive modulus exclusion")
	}
}

func TestUnidentifiableShiftShownAsRange(t *testing.T) {
	srv, _ := newTestServer(t)
	_, body := get(t, srv, "/")
	if !strings.Contains(body, "[-7.000, -2.000]") {
		t.Fatal("unidentifiable shift must be displayed as a range, not a point estimate")
	}
}

func postForm(t *testing.T, srv *httptest.Server, path string, v url.Values) *http.Response {
	t.Helper()
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(srv.URL+path, v)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func TestExcludeAndLookupReason(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := postForm(t, srv, "/exclude", url.Values{
		"point_id": {"5"}, "reason": {"超出线性黏弹区（测试）"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("exclude returned %d", resp.StatusCode)
	}
	code, body := get(t, srv, "/api/exclusions")
	if code != http.StatusOK {
		t.Fatalf("GET /api/exclusions = %d", code)
	}
	var ex []tts.ExcludedPoint
	if err := json.Unmarshal([]byte(body), &ex); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range ex {
		if e.PointID == 5 {
			found = true
			if e.Reason != "超出线性黏弹区（测试）" || e.Source != tts.SourceUser {
				t.Fatalf("unexpected exclusion record: %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("excluded point 5 must be queryable with its reason")
	}
	// Restore it.
	postForm(t, srv, "/include", url.Values{"point_id": {"5"}})
	_, body = get(t, srv, "/api/exclusions")
	if strings.Contains(body, "超出线性黏弹区（测试）") {
		t.Fatal("restored point must drop out of the exclusion list")
	}
}

func TestLockedCurveShiftImmutable(t *testing.T) {
	srv, _ := newTestServer(t)
	// Lock curve 2, then attempt to change its shift in the same request.
	postForm(t, srv, "/shifts", url.Values{
		"locked_2": {"on"}, "loga_2": {"5.5"}, "loga_3": {"0.1"},
	})
	_, body := get(t, srv, "/api/export")
	var ef exportFile
	if err := json.Unmarshal([]byte(body), &ef); err != nil {
		t.Fatal(err)
	}
	for _, sh := range ef.Run.Shifts {
		if sh.CurveID == 2 {
			if !sh.Locked || sh.LogA != 0 {
				t.Fatalf("locked curve shift must stay 0, got %+v", sh)
			}
		}
		if sh.CurveID == 3 && sh.LogA != 0.1 {
			t.Fatalf("unlocked curve shift must be saved, got %+v", sh)
		}
	}
}

func TestReplaySameSeedIdenticalFingerprint(t *testing.T) {
	srv, _ := newTestServer(t)
	_, body1 := get(t, srv, "/api/export")
	var ef1 exportFile
	json.Unmarshal([]byte(body1), &ef1)

	// Perturb state, then replay the same seed.
	postForm(t, srv, "/shifts", url.Values{"loga_2": {"0.77"}})
	postForm(t, srv, "/reseed", url.Values{"seed": {"1183"}})

	_, body2 := get(t, srv, "/api/export")
	var ef2 exportFile
	json.Unmarshal([]byte(body2), &ef2)
	if ef1.Fingerprint != ef2.Fingerprint {
		t.Fatalf("replaying seed 1183 must reproduce fingerprint %s, got %s",
			ef1.Fingerprint, ef2.Fingerprint)
	}
}

func TestExportWipeImportRestores(t *testing.T) {
	srv, st := newTestServer(t)
	// Customise the run: manual shift + user exclusion + tolerance change.
	postForm(t, srv, "/settings", url.Values{"ref_temp_c": {"25"}, "tolerance": {"0.2"}})
	postForm(t, srv, "/shifts", url.Values{"loga_2": {"0.9"}, "usevert_2": {"on"}, "logb_2": {"-0.05"}})
	postForm(t, srv, "/exclude", url.Values{"point_id": {"8"}, "reason": {"末端打滑"}})

	_, exported := get(t, srv, "/api/export")
	var ef exportFile
	if err := json.Unmarshal([]byte(exported), &ef); err != nil {
		t.Fatal(err)
	}

	// Wipe the database by reseeding with a different seed.
	postForm(t, srv, "/reseed", url.Values{"seed": {"999"}})
	r, err := st.LoadRun()
	if err != nil || r.Seed != 999 {
		t.Fatal("database should have been replaced")
	}

	// Import the exported record.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "run.json")
	fw.Write([]byte(exported))
	mw.Close()
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Post(srv.URL+"/api/import", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("import returned %d", resp.StatusCode)
	}

	restored, err := st.LoadRun()
	if err != nil {
		t.Fatal(err)
	}
	if tts.Fingerprint(restored) != ef.Fingerprint {
		t.Fatal("re-imported run must reproduce the exported fingerprint")
	}
	if restored.Tolerance != 0.2 {
		t.Fatal("tolerance must survive export/import")
	}
	found := false
	for _, e := range restored.Exclusions {
		if e.PointID == 8 && e.Reason == "末端打滑" {
			found = true
		}
	}
	if !found {
		t.Fatal("user exclusion reasons must survive export/import")
	}
}
