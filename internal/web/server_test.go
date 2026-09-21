package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ttsworkbench/internal/rheo"
	"ttsworkbench/internal/store"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	raw, err := os.ReadFile("../../fixtures/sweeps.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx rheo.Fixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.ImportFixture(fx); err != nil {
		t.Fatal(err)
	}
	return NewServer(st, fx)
}

func get(t *testing.T, srv http.Handler, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	if rec.Code != 200 {
		t.Fatalf("GET %s: status %d", path, rec.Code)
	}
	return rec.Body.String()
}

func post(t *testing.T, srv http.Handler, path string, form url.Values) {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s: status %d, body %s", path, rec.Code, rec.Body.String())
	}
}

func TestIndexShowsTitle(t *testing.T) {
	srv := testServer(t)
	body := get(t, srv, "/")
	if !strings.Contains(body, "流变叠时台") {
		t.Fatal("index must show 流变叠时台")
	}
	if !strings.Contains(body, "<svg") {
		t.Fatal("index must render SVG charts")
	}
}

func TestRunFlowAndReplay(t *testing.T) {
	srv := testServer(t)
	form := url.Values{"seed": {"42"}, "tol_deg": {"2.0"}, "ref_curve_id": {"2"}}
	post(t, srv, "/run", form)
	body := get(t, srv, "/")
	if !strings.Contains(body, "指纹") {
		t.Fatal("page must show the run fingerprint")
	}
	// The isolated curve must appear as a range, not a precise value.
	if !strings.Contains(body, "不可识别") {
		t.Fatal("page must show the unidentifiable diagnostic")
	}
	if !strings.Contains(body, "WLF 不可辨识") {
		t.Fatal("page must report underdetermined WLF fit")
	}
	// Replay with the same seed: identical fingerprint, second run recorded.
	post(t, srv, "/run", form)
	body2 := get(t, srv, "/")
	extract := func(s string) string {
		i := strings.Index(s, `class="mono">`)
		if i < 0 {
			return ""
		}
		return s[i : i+80]
	}
	if extract(body) != extract(body2) {
		t.Fatal("same seed must replay the same fingerprint")
	}
}

func TestExclusionReasonTraceable(t *testing.T) {
	srv := testServer(t)
	post(t, srv, "/run", url.Values{"seed": {"42"}, "tol_deg": {"2.0"}, "ref_curve_id": {"2"}})
	body := get(t, srv, "/")
	if !strings.Contains(body, "模量非正") {
		t.Fatal("auto exclusion reason for non-positive modulus must be visible")
	}
	if !strings.Contains(body, "相位角与模量不一致") {
		t.Fatal("auto exclusion reason for phase mismatch must be visible")
	}
	// User exclusion with a custom reason.
	post(t, srv, "/exclude", url.Values{"point_id": {"5"}, "op": {"exclude"}, "reason": {"超出线性黏弹区"}})
	body = get(t, srv, "/")
	if !strings.Contains(body, "用户排除：超出线性黏弹区") {
		t.Fatal("user exclusion reason must be queryable on the page")
	}
	// Restore.
	post(t, srv, "/exclude", url.Values{"point_id": {"5"}, "op": {"restore"}})
	if strings.Contains(get(t, srv, "/"), "用户排除：超出线性黏弹区") {
		t.Fatal("restored point must no longer show the exclusion reason")
	}
}

func TestManualShiftNotOverwritten(t *testing.T) {
	srv := testServer(t)
	post(t, srv, "/state", url.Values{"curve_id": {"3"}, "manual_h": {"-6.5"}, "manual_v": {"0.1"}, "locked": {"on"}})
	post(t, srv, "/run", url.Values{"seed": {"42"}, "tol_deg": {"2.0"}, "ref_curve_id": {"2"}})
	body := get(t, srv, "/")
	if !strings.Contains(body, `value="-6.5000"`) {
		t.Fatal("manual shift must survive analysis runs")
	}
	if !strings.Contains(body, "WLF: C1=") {
		t.Fatal("WLF must fit once the third curve is locked")
	}
}

func TestExportAndReset(t *testing.T) {
	srv := testServer(t)
	post(t, srv, "/run", url.Values{"seed": {"42"}, "tol_deg": {"2.0"}, "ref_curve_id": {"2"}})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/export", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "fingerprint") {
		t.Fatalf("export failed: %d", rec.Code)
	}
	post(t, srv, "/reset", url.Values{})
	body := get(t, srv, "/")
	if !strings.Contains(body, "尚未运行分析") {
		t.Fatal("reset must clear runs")
	}
	if !strings.Contains(body, "流变叠时台") {
		t.Fatal("page must still render after reset")
	}
}
