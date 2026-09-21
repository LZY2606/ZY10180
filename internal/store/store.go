// Package store persists the workbench run in SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"

	"ttsbench/internal/tts"
)

// Store wraps the SQLite handle.
type Store struct {
	db *sql.DB
}

// Open opens (and if needed initialises) the database file.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS curves (
  id     INTEGER PRIMARY KEY,
  label  TEXT NOT NULL,
  temp_c REAL NOT NULL,
  ord    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS points (
  id        INTEGER PRIMARY KEY,
  curve_id  INTEGER NOT NULL REFERENCES curves(id),
  freq      REAL NOT NULL,
  g1        REAL NOT NULL,
  g2        REAL NOT NULL,
  phase_deg REAL NOT NULL
);
CREATE TABLE IF NOT EXISTS shifts (
  curve_id     INTEGER PRIMARY KEY REFERENCES curves(id),
  log_a        REAL NOT NULL DEFAULT 0,
  log_b        REAL NOT NULL DEFAULT 0,
  use_vertical INTEGER NOT NULL DEFAULT 0,
  locked       INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS exclusions (
  point_id INTEGER PRIMARY KEY REFERENCES points(id),
  reason   TEXT NOT NULL
);`)
	return err
}

// HasRun reports whether a run has been persisted.
func (s *Store) HasRun() (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM curves`).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// LoadRun reconstructs the run from the database, or returns nil when empty.
func (s *Store) LoadRun() (*tts.Run, error) {
	has, err := s.HasRun()
	if err != nil || !has {
		return nil, err
	}
	r := &tts.Run{}
	meta := map[string]string{}
	rows, err := s.db.Query(`SELECT key, value FROM meta`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return nil, err
		}
		meta[k] = v
	}
	rows.Close()
	if _, err := fmt.Sscanf(meta["seed"], "%d", &r.Seed); err != nil {
		return nil, fmt.Errorf("meta seed: %w", err)
	}
	if _, err := fmt.Sscanf(meta["ref_temp_c"], "%g", &r.RefTempC); err != nil {
		return nil, fmt.Errorf("meta ref_temp_c: %w", err)
	}
	if _, err := fmt.Sscanf(meta["tolerance"], "%g", &r.Tolerance); err != nil {
		return nil, fmt.Errorf("meta tolerance: %w", err)
	}

	crows, err := s.db.Query(`SELECT id, label, temp_c FROM curves ORDER BY ord`)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	for crows.Next() {
		var c tts.Curve
		if err := crows.Scan(&c.ID, &c.Label, &c.TempC); err != nil {
			return nil, err
		}
		r.Curves = append(r.Curves, c)
	}
	crows.Close()

	for i := range r.Curves {
		prows, err := s.db.Query(`SELECT id, freq, g1, g2, phase_deg FROM points WHERE curve_id=? ORDER BY id`, r.Curves[i].ID)
		if err != nil {
			return nil, err
		}
		for prows.Next() {
			var p tts.Point
			if err := prows.Scan(&p.ID, &p.Freq, &p.G1, &p.G2, &p.PhaseDeg); err != nil {
				prows.Close()
				return nil, err
			}
			r.Curves[i].Points = append(r.Curves[i].Points, p)
		}
		prows.Close()
	}

	srows, err := s.db.Query(`SELECT curve_id, log_a, log_b, use_vertical, locked FROM shifts ORDER BY curve_id`)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	for srows.Next() {
		var sh tts.Shift
		var uv, lk int
		if err := srows.Scan(&sh.CurveID, &sh.LogA, &sh.LogB, &uv, &lk); err != nil {
			return nil, err
		}
		sh.UseVertical = uv != 0
		sh.Locked = lk != 0
		r.Shifts = append(r.Shifts, sh)
	}
	srows.Close()

	erows, err := s.db.Query(`SELECT point_id, reason FROM exclusions ORDER BY point_id`)
	if err != nil {
		return nil, err
	}
	defer erows.Close()
	for erows.Next() {
		var e tts.UserExclusion
		if err := erows.Scan(&e.PointID, &e.Reason); err != nil {
			return nil, err
		}
		r.Exclusions = append(r.Exclusions, e)
	}
	erows.Close()
	return r, nil
}

// SaveRun replaces the whole database content with the given run,
// atomically, so a wiped database can be rebuilt from an export.
func (s *Store) SaveRun(r *tts.Run) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM exclusions`, `DELETE FROM shifts`, `DELETE FROM points`,
		`DELETE FROM curves`, `DELETE FROM meta`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	setMeta := func(k string, v string) error {
		_, err := tx.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)`, k, v)
		return err
	}
	if err := setMeta("seed", fmt.Sprintf("%d", r.Seed)); err != nil {
		return err
	}
	if err := setMeta("ref_temp_c", fmt.Sprintf("%.17g", r.RefTempC)); err != nil {
		return err
	}
	if err := setMeta("tolerance", fmt.Sprintf("%.17g", r.Tolerance)); err != nil {
		return err
	}
	for i, c := range r.Curves {
		if _, err := tx.Exec(`INSERT INTO curves(id, label, temp_c, ord) VALUES(?,?,?,?)`,
			c.ID, c.Label, c.TempC, i); err != nil {
			return err
		}
		for _, p := range c.Points {
			if _, err := tx.Exec(`INSERT INTO points(id, curve_id, freq, g1, g2, phase_deg) VALUES(?,?,?,?,?,?)`,
				p.ID, c.ID, p.Freq, p.G1, p.G2, p.PhaseDeg); err != nil {
				return err
			}
		}
	}
	for _, sh := range r.Shifts {
		if _, err := tx.Exec(`INSERT INTO shifts(curve_id, log_a, log_b, use_vertical, locked) VALUES(?,?,?,?,?)`,
			sh.CurveID, sh.LogA, sh.LogB, boolInt(sh.UseVertical), boolInt(sh.Locked)); err != nil {
			return err
		}
	}
	for _, e := range r.Exclusions {
		if e.Reason == "" {
			return errors.New("exclusion reason must not be empty")
		}
		if _, err := tx.Exec(`INSERT INTO exclusions(point_id, reason) VALUES(?,?)`, e.PointID, e.Reason); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
