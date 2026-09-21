package store

import (
	"path/filepath"
	"testing"

	"ttsbench/internal/tts"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	has, err := st.HasRun()
	if err != nil || has {
		t.Fatalf("fresh database must be empty, has=%v err=%v", has, err)
	}

	r := tts.NewRun(1183)
	r.Shifts[1].LogA = 0.9
	r.Shifts[1].UseVertical = true
	r.Shifts[1].LogB = -0.05
	r.Shifts[2].Locked = true
	r.Exclusions = append(r.Exclusions, tts.UserExclusion{PointID: 3, Reason: "超出线性黏弹区"})
	if err := st.SaveRun(r); err != nil {
		t.Fatal(err)
	}

	loaded, err := st.LoadRun()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected a run")
	}
	if tts.Fingerprint(loaded) != tts.Fingerprint(r) {
		t.Fatal("fingerprint must survive a save/load round trip")
	}
	if loaded.Shifts[1].LogA != 0.9 || !loaded.Shifts[1].UseVertical || loaded.Shifts[1].LogB != -0.05 {
		t.Fatal("manual shifts must round-trip exactly")
	}
	if len(loaded.Exclusions) != 1 || loaded.Exclusions[0].Reason != "超出线性黏弹区" {
		t.Fatal("user exclusions must round-trip with reasons")
	}
}

func TestExclusionReasonRequired(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := tts.NewRun(1183)
	r.Exclusions = append(r.Exclusions, tts.UserExclusion{PointID: 3, Reason: ""})
	if err := st.SaveRun(r); err == nil {
		t.Fatal("empty exclusion reason must be rejected")
	}
}
