// Package store persists curves, points, user state and analysis runs in SQLite.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"ttsworkbench/internal/rheo"
)

// Store wraps the SQLite handle.
type Store struct {
	db *sql.DB
}

// RunMeta is the summary row of one analysis run.
type RunMeta struct {
	ID          int64   `json:"id"`
	Created     string  `json:"created"`
	Seed        int64   `json:"seed"`
	TolDeg      float64 `json:"tol_deg"`
	RefCurveID  int64   `json:"ref_curve_id"`
	Fingerprint string  `json:"fingerprint"`
}

// Open opens (and migrates) the database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	return s, s.migrate()
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS curves(
  id INTEGER PRIMARY KEY, label TEXT NOT NULL, temp_c REAL NOT NULL, sort INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS points(
  id INTEGER PRIMARY KEY, curve_id INTEGER NOT NULL, freq_hz REAL NOT NULL,
  g1 REAL NOT NULL, g2 REAL NOT NULL, phase_deg REAL NOT NULL,
  user_excluded INTEGER NOT NULL DEFAULT 0, user_reason TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS curve_state(
  curve_id INTEGER PRIMARY KEY, manual_h REAL NOT NULL DEFAULT 0,
  manual_h_set INTEGER NOT NULL DEFAULT 0, manual_v REAL NOT NULL DEFAULT 0,
  locked INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS runs(
  id INTEGER PRIMARY KEY, created TEXT NOT NULL, seed INTEGER NOT NULL,
  tol_deg REAL NOT NULL, ref_curve_id INTEGER NOT NULL,
  fingerprint TEXT NOT NULL, result_json TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS run_exclusions(
  run_id INTEGER NOT NULL, point_id INTEGER NOT NULL, reason TEXT NOT NULL);
`)
	return err
}

// Empty reports whether no curves are loaded.
func (s *Store) Empty() (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM curves`).Scan(&n)
	return n == 0, err
}

// ImportFixture replaces all data with the fixture contents.
// Points with non-positive frequency are rejected.
func (s *Store) ImportFixture(f rheo.Fixture) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM run_exclusions`, `DELETE FROM runs`, `DELETE FROM curve_state`,
		`DELETE FROM points`, `DELETE FROM curves`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	for i, fc := range f.Curves {
		r, err := tx.Exec(`INSERT INTO curves(label, temp_c, sort) VALUES(?,?,?)`, fc.Label, fc.TempC, i)
		if err != nil {
			return err
		}
		cid, _ := r.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO curve_state(curve_id) VALUES(?)`, cid); err != nil {
			return err
		}
		for _, p := range fc.Points {
			if len(p) != 4 {
				return fmt.Errorf("曲线 %s：数据点列数不为 4", fc.Label)
			}
			if p[0] <= 0 {
				return fmt.Errorf("曲线 %s：频率必须大于零，得到 %.6g", fc.Label, p[0])
			}
			if _, err := tx.Exec(`INSERT INTO points(curve_id, freq_hz, g1, g2, phase_deg) VALUES(?,?,?,?,?)`,
				cid, p[0], p[1], p[2], p[3]); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// Curves returns all curves ordered by temperature.
func (s *Store) Curves() ([]rheo.Curve, error) {
	rows, err := s.db.Query(`SELECT id, label, temp_c, sort FROM curves ORDER BY temp_c`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rheo.Curve
	for rows.Next() {
		var c rheo.Curve
		if err := rows.Scan(&c.ID, &c.Label, &c.TempC, &c.Sort); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Points returns all points grouped by curve ID.
func (s *Store) Points() (map[int64][]rheo.Point, error) {
	rows, err := s.db.Query(`SELECT id, curve_id, freq_hz, g1, g2, phase_deg, user_excluded, user_reason FROM points ORDER BY curve_id, freq_hz`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]rheo.Point{}
	for rows.Next() {
		var p rheo.Point
		var ex int
		if err := rows.Scan(&p.ID, &p.CurveID, &p.FreqHz, &p.G1, &p.G2, &p.PhaseDeg, &ex, &p.UserReason); err != nil {
			return nil, err
		}
		p.UserExcluded = ex != 0
		out[p.CurveID] = append(out[p.CurveID], p)
	}
	return out, rows.Err()
}

// States returns per-curve user state.
func (s *Store) States() (map[int64]rheo.CurveState, error) {
	rows, err := s.db.Query(`SELECT curve_id, manual_h, manual_h_set, manual_v, locked FROM curve_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]rheo.CurveState{}
	for rows.Next() {
		var st rheo.CurveState
		var set, locked int
		if err := rows.Scan(&st.CurveID, &st.ManualH, &set, &st.ManualV, &locked); err != nil {
			return nil, err
		}
		st.ManualHSet = set != 0
		st.Locked = locked != 0
		out[st.CurveID] = st
	}
	return out, rows.Err()
}

// SetState upserts one curve's user state.
func (s *Store) SetState(st rheo.CurveState) error {
	_, err := s.db.Exec(`INSERT INTO curve_state(curve_id, manual_h, manual_h_set, manual_v, locked)
VALUES(?,?,?,?,?)
ON CONFLICT(curve_id) DO UPDATE SET manual_h=excluded.manual_h,
  manual_h_set=excluded.manual_h_set, manual_v=excluded.manual_v, locked=excluded.locked`,
		st.CurveID, st.ManualH, b2i(st.ManualHSet), st.ManualV, b2i(st.Locked))
	return err
}

// SetPointExcluded flags or restores a user exclusion with a reason.
func (s *Store) SetPointExcluded(id int64, excluded bool, reason string) error {
	_, err := s.db.Exec(`UPDATE points SET user_excluded=?, user_reason=? WHERE id=?`,
		b2i(excluded), reason, id)
	return err
}

// SaveRun persists a run and its auto-exclusions; returns the run ID.
func (s *Store) SaveRun(res rheo.RunResult) (int64, error) {
	buf, err := json.Marshal(res)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	r, err := tx.Exec(`INSERT INTO runs(created, seed, tol_deg, ref_curve_id, fingerprint, result_json)
VALUES(?,?,?,?,?,?)`, time.Now().UTC().Format(time.RFC3339), res.Seed, res.TolDeg, res.RefCurveID, res.Fingerprint, string(buf))
	if err != nil {
		return 0, err
	}
	id, _ := r.LastInsertId()
	for _, ex := range res.Exclusions {
		if _, err := tx.Exec(`INSERT INTO run_exclusions(run_id, point_id, reason) VALUES(?,?,?)`,
			id, ex.PointID, ex.Reason); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// LatestRun returns the most recent run, or nil when none exists.
func (s *Store) LatestRun() (*RunMeta, *rheo.RunResult, error) {
	row := s.db.QueryRow(`SELECT id, created, seed, tol_deg, ref_curve_id, fingerprint, result_json
FROM runs ORDER BY id DESC LIMIT 1`)
	var m RunMeta
	var js string
	err := row.Scan(&m.ID, &m.Created, &m.Seed, &m.TolDeg, &m.RefCurveID, &m.Fingerprint, &js)
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var res rheo.RunResult
	if err := json.Unmarshal([]byte(js), &res); err != nil {
		return nil, nil, err
	}
	return &m, &res, nil
}

// Runs lists run summaries, newest first.
func (s *Store) Runs() ([]RunMeta, error) {
	rows, err := s.db.Query(`SELECT id, created, seed, tol_deg, ref_curve_id, fingerprint FROM runs ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunMeta
	for rows.Next() {
		var m RunMeta
		if err := rows.Scan(&m.ID, &m.Created, &m.Seed, &m.TolDeg, &m.RefCurveID, &m.Fingerprint); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Export serialises curves, points, states and runs as JSON.
func (s *Store) Export() ([]byte, error) {
	curves, err := s.Curves()
	if err != nil {
		return nil, err
	}
	points, err := s.Points()
	if err != nil {
		return nil, err
	}
	states, err := s.States()
	if err != nil {
		return nil, err
	}
	runs, err := s.Runs()
	if err != nil {
		return nil, err
	}
	type runRow struct {
		RunMeta
		Result json.RawMessage `json:"result"`
	}
	var runRows []runRow
	rows, err := s.db.Query(`SELECT id, result_json FROM runs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := map[int64]json.RawMessage{}
	for rows.Next() {
		var id int64
		var js string
		if err := rows.Scan(&id, &js); err != nil {
			return nil, err
		}
		results[id] = json.RawMessage(js)
	}
	for _, m := range runs {
		runRows = append(runRows, runRow{m, results[m.ID]})
	}
	payload := map[string]any{
		"curves": curves, "points": points, "states": states, "runs": runRows,
	}
	return json.MarshalIndent(payload, "", "  ")
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
