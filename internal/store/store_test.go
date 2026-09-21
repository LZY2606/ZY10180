package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ttsworkbench/internal/rheo"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func loadFixture(t *testing.T) rheo.Fixture {
	t.Helper()
	raw, err := os.ReadFile("../../fixtures/sweeps.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx rheo.Fixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	return fx
}

func TestImportAndRoundTrip(t *testing.T) {
	st := testStore(t)
	fx := loadFixture(t)
	if err := st.ImportFixture(fx); err != nil {
		t.Fatal(err)
	}
	curves, err := st.Curves()
	if err != nil || len(curves) != 3 {
		t.Fatalf("want 3 curves, got %d (%v)", len(curves), err)
	}
	points, err := st.Points()
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, ps := range points {
		total += len(ps)
	}
	if total != 75 {
		t.Fatalf("want 75 points, got %d", total)
	}
	states, err := st.States()
	if err != nil || len(states) != 3 {
		t.Fatalf("want 3 states, got %d (%v)", len(states), err)
	}
}

func TestImportRejectsNonPositiveFrequency(t *testing.T) {
	st := testStore(t)
	fx := rheo.Fixture{Curves: []rheo.FixtureCurve{{
		Label: "bad", TempC: 25,
		Points: [][]float64{{0, 1, 1, 45}},
	}}}
	if err := st.ImportFixture(fx); err == nil {
		t.Fatal("non-positive frequency must be rejected at import")
	}
}

func TestRunPersistenceAndExport(t *testing.T) {
	st := testStore(t)
	fx := loadFixture(t)
	if err := st.ImportFixture(fx); err != nil {
		t.Fatal(err)
	}
	curves, _ := st.Curves()
	points, _ := st.Points()
	states, _ := st.States()
	res := rheo.Run(curves, points, states, fx.Seed, 2.0, curves[1].ID)
	id1, err := st.SaveRun(res)
	if err != nil {
		t.Fatal(err)
	}
	// Replay: same configuration must yield the same fingerprint.
	res2 := rheo.Run(curves, points, states, fx.Seed, 2.0, curves[1].ID)
	id2, err := st.SaveRun(res2)
	if err != nil {
		t.Fatal(err)
	}
	if res.Fingerprint != res2.Fingerprint {
		t.Fatalf("replay fingerprint mismatch:\n%s\n%s", res.Fingerprint, res2.Fingerprint)
	}
	meta, got, err := st.LatestRun()
	if err != nil || meta == nil {
		t.Fatalf("latest run: %v", err)
	}
	if meta.ID != id2 || got.Fingerprint != res.Fingerprint {
		t.Fatalf("latest run mismatch: %+v", meta)
	}
	runs, err := st.Runs()
	if err != nil || len(runs) != 2 || runs[0].ID != id2 || runs[1].ID != id1 {
		t.Fatalf("runs listing wrong: %+v", runs)
	}
	buf, err := st.Export()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf), res.Fingerprint) {
		t.Fatal("export must contain run fingerprints")
	}
	if !strings.Contains(string(buf), `"runs"`) || !strings.Contains(string(buf), `"points"`) {
		t.Fatal("export must contain runs and points")
	}
}

// After wiping and re-importing, analysis replays identically.
func TestResetReimportReplay(t *testing.T) {
	st := testStore(t)
	fx := loadFixture(t)
	if err := st.ImportFixture(fx); err != nil {
		t.Fatal(err)
	}
	run := func() string {
		curves, _ := st.Curves()
		points, _ := st.Points()
		states, _ := st.States()
		return rheo.Run(curves, points, states, fx.Seed, 2.0, curves[1].ID).Fingerprint
	}
	first := run()
	if err := st.ImportFixture(fx); err != nil { // wipe + reimport
		t.Fatal(err)
	}
	if second := run(); second != first {
		t.Fatalf("reimport replay mismatch:\n%s\n%s", first, second)
	}
}

func TestUserExclusionPersists(t *testing.T) {
	st := testStore(t)
	fx := loadFixture(t)
	if err := st.ImportFixture(fx); err != nil {
		t.Fatal(err)
	}
	points, _ := st.Points()
	var pid int64
	for _, ps := range points {
		pid = ps[0].ID
	}
	if err := st.SetPointExcluded(pid, true, "超出线性黏弹区"); err != nil {
		t.Fatal(err)
	}
	points, _ = st.Points()
	found := false
	for _, ps := range points {
		for _, p := range ps {
			if p.ID == pid {
				found = true
				if !p.UserExcluded || p.UserReason != "超出线性黏弹区" {
					t.Fatalf("exclusion not persisted: %+v", p)
				}
			}
		}
	}
	if !found {
		t.Fatal("point not found")
	}
	if err := st.SetPointExcluded(pid, false, ""); err != nil {
		t.Fatal(err)
	}
}
