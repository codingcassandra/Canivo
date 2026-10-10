// Package store persists dogs, their baselines, alerts and owners' push
// tokens in SQLite. Raw sensor samples are not stored.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/codingcassandra/Canivo/internal/monitor"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Dog struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	CreatedAt time.Time         `json:"created_at"`
	Baseline  *monitor.Baseline `json:"baseline,omitempty"`
}

type Alert struct {
	ID        int64      `json:"id"`
	DogID     int64      `json:"dog_id"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Score     float64    `json:"score"`
	RateBPM   float64    `json:"rate_bpm"`
	Tension   float64    `json:"tension"`
}

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS dogs (
	id         INTEGER PRIMARY KEY,
	name       TEXT NOT NULL,
	created_at TEXT NOT NULL,
	baseline   TEXT
);
CREATE TABLE IF NOT EXISTS alerts (
	id         INTEGER PRIMARY KEY,
	dog_id     INTEGER NOT NULL REFERENCES dogs(id) ON DELETE CASCADE,
	started_at TEXT NOT NULL,
	ended_at   TEXT,
	score      REAL NOT NULL,
	rate_bpm   REAL NOT NULL,
	tension    REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS alerts_dog ON alerts(dog_id, started_at);
CREATE TABLE IF NOT EXISTS push_tokens (
	token      TEXT NOT NULL,
	dog_id     INTEGER NOT NULL REFERENCES dogs(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL,
	PRIMARY KEY (token, dog_id)
);`

// Open opens (and migrates) the database at path. Use ":memory:" in tests.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite allows one writer; also keeps :memory: a single DB
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) CreateDog(ctx context.Context, name string) (Dog, error) {
	d := Dog{Name: name, CreatedAt: time.Now().UTC()}
	res, err := s.db.ExecContext(ctx, `INSERT INTO dogs(name, created_at) VALUES (?, ?)`, name, fmtTime(d.CreatedAt))
	if err != nil {
		return Dog{}, err
	}
	d.ID, err = res.LastInsertId()
	return d, err
}

func (s *Store) Dog(ctx context.Context, id int64) (Dog, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, created_at, baseline FROM dogs WHERE id = ?`, id)
	return scanDog(row)
}

func (s *Store) Dogs(ctx context.Context) ([]Dog, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, baseline FROM dogs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dogs := []Dog{}
	for rows.Next() {
		d, err := scanDog(rows)
		if err != nil {
			return nil, err
		}
		dogs = append(dogs, d)
	}
	return dogs, rows.Err()
}

func (s *Store) SetBaseline(ctx context.Context, dogID int64, b *monitor.Baseline) error {
	var v any // NULL clears it
	if b != nil {
		j, err := json.Marshal(b)
		if err != nil {
			return err
		}
		v = string(j)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE dogs SET baseline = ? WHERE id = ?`, v, dogID)
	return err
}

func (s *Store) StartAlert(ctx context.Context, a Alert) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO alerts(dog_id, started_at, score, rate_bpm, tension) VALUES (?, ?, ?, ?, ?)`,
		a.DogID, fmtTime(a.StartedAt), a.Score, a.RateBPM, a.Tension)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) EndAlert(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alerts SET ended_at = ? WHERE id = ?`, fmtTime(at), id)
	return err
}

// EndOpenAlerts closes alerts left open by a previous run.
func (s *Store) EndOpenAlerts(ctx context.Context, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alerts SET ended_at = ? WHERE ended_at IS NULL`, fmtTime(at))
	return err
}

func (s *Store) Alerts(ctx context.Context, dogID int64, limit int) ([]Alert, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, dog_id, started_at, ended_at, score, rate_bpm, tension
		 FROM alerts WHERE dog_id = ? ORDER BY started_at DESC, id DESC LIMIT ?`, dogID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	alerts := []Alert{}
	for rows.Next() {
		var a Alert
		var started string
		var ended sql.NullString
		if err := rows.Scan(&a.ID, &a.DogID, &started, &ended, &a.Score, &a.RateBPM, &a.Tension); err != nil {
			return nil, err
		}
		a.StartedAt = parseTime(started)
		if ended.Valid {
			t := parseTime(ended.String)
			a.EndedAt = &t
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

func (s *Store) AddPushToken(ctx context.Context, dogID int64, token string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO push_tokens(token, dog_id, created_at) VALUES (?, ?, ?)`,
		token, dogID, fmtTime(time.Now()))
	return err
}

func (s *Store) PushTokens(ctx context.Context, dogID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT token FROM push_tokens WHERE dog_id = ?`, dogID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// RemovePushToken deletes a token from every dog (e.g. the app was uninstalled).
func (s *Store) RemovePushToken(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_tokens WHERE token = ?`, token)
	return err
}

type scanner interface{ Scan(...any) error }

func scanDog(r scanner) (Dog, error) {
	var d Dog
	var created string
	var baseline sql.NullString
	if err := r.Scan(&d.ID, &d.Name, &created, &baseline); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Dog{}, ErrNotFound
		}
		return Dog{}, err
	}
	d.CreatedAt = parseTime(created)
	if baseline.Valid {
		d.Baseline = &monitor.Baseline{}
		if err := json.Unmarshal([]byte(baseline.String), d.Baseline); err != nil {
			return Dog{}, err
		}
	}
	return d, nil
}

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
