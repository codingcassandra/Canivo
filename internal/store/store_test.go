package store

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/codingcassandra/Canivo/internal/geo"
	"github.com/codingcassandra/Canivo/internal/monitor"
	"github.com/codingcassandra/Canivo/internal/notify"
)

// Runs against SQLite by default. Set TEST_DATABASE_URL=postgres://... to
// run the same checks against Postgres (it must be an empty, throwaway DB).
func open(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = ":memory:"
	}
	s, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	d, err := s.CreateDog(ctx, "Rex", 0)
	if err != nil || d.ID == 0 {
		t.Fatalf("create: %+v %v", d, err)
	}
	if _, err := s.Dog(ctx, d.ID+1000); err != ErrNotFound {
		t.Fatalf("missing dog err = %v", err)
	}

	b := &monitor.Baseline{RateMean: 18, RateSD: 2, TensionMean: 220, TensionSD: 11, Points: 120}
	if err := s.SetBaseline(ctx, d.ID, b); err != nil {
		t.Fatal(err)
	}
	got, err := s.Dog(ctx, d.ID)
	if err != nil || got.Baseline == nil || *got.Baseline != *b {
		t.Fatalf("baseline = %+v %v", got.Baseline, err)
	}

	start := time.Now().UTC().Truncate(time.Millisecond)
	id, err := s.StartAlert(ctx, Alert{DogID: d.ID, StartedAt: start, Score: 3.1, RateBPM: 40, Tension: 310})
	if err != nil || id == 0 {
		t.Fatalf("start alert: %d %v", id, err)
	}
	if err := s.EndAlert(ctx, id, start.Add(20*time.Second)); err != nil {
		t.Fatal(err)
	}
	alerts, err := s.Alerts(ctx, d.ID, 10)
	if err != nil || len(alerts) != 1 || alerts[0].EndedAt == nil || !alerts[0].StartedAt.Equal(start) || alerts[0].Score != 3.1 {
		t.Fatalf("alerts = %+v %v", alerts, err)
	}

	for range 2 { // second insert is a no-op
		if err := s.AddPushToken(ctx, d.ID, "ExponentPushToken[a]"); err != nil {
			t.Fatal(err)
		}
	}
	if toks, _ := s.PushTokens(ctx, d.ID); len(toks) != 1 {
		t.Fatalf("tokens = %v", toks)
	}
	if err := s.RemovePushToken(ctx, "ExponentPushToken[a]"); err != nil {
		t.Fatal(err)
	}
	if toks, _ := s.PushTokens(ctx, d.ID); len(toks) != 0 {
		t.Fatalf("tokens after remove = %v", toks)
	}

	var sub notify.WebSubscription
	sub.Endpoint = "https://web.push.apple.com/abc"
	sub.Keys.P256dh, sub.Keys.Auth = "p1", "a1"
	s.AddWebPush(ctx, d.ID, sub)
	sub.Keys.P256dh = "p2" // browser re-subscribed with new keys
	if err := s.AddWebPush(ctx, d.ID, sub); err != nil {
		t.Fatal(err)
	}
	subs, err := s.WebPushSubs(ctx, d.ID)
	if err != nil || len(subs) != 1 || subs[0].Keys.P256dh != "p2" {
		t.Fatalf("web subs = %+v %v", subs, err)
	}
	s.RemoveWebPush(ctx, sub.Endpoint)
	if subs, _ := s.WebPushSubs(ctx, d.ID); len(subs) != 0 {
		t.Fatalf("web subs after remove = %+v", subs)
	}
}

func TestZonesAndFixes(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	d, _ := s.CreateDog(ctx, "Rex", 0)

	if z, err := s.Zone(ctx, d.ID); z != nil || err != nil {
		t.Fatalf("zone before set = %+v %v", z, err)
	}
	s.SetZone(ctx, d.ID, &geo.Zone{Lat: 1, Lon: 2, RadiusM: 50})
	want := geo.Zone{Lat: 40.7, Lon: -74, RadiusM: 120}
	if err := s.SetZone(ctx, d.ID, &want); err != nil {
		t.Fatal(err)
	}
	if z, err := s.Zone(ctx, d.ID); err != nil || z == nil || *z != want {
		t.Fatalf("zone = %+v %v", z, err)
	}
	s.SetZone(ctx, d.ID, nil)
	if z, _ := s.Zone(ctx, d.ID); z != nil {
		t.Fatalf("zone after delete = %+v", z)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	fixes := []geo.Fix{
		{At: now.Add(-48 * time.Hour), Lat: 9, Lon: 9, AccuracyM: 5}, // pruned
		{At: now.Add(-2 * time.Minute), Lat: 40.70, Lon: -74, AccuracyM: 8},
		{At: now.Add(-1 * time.Minute), Lat: 40.71, Lon: -74, AccuracyM: 9},
		{At: now, Lat: 40.72, Lon: -74, AccuracyM: 10},
	}
	if err := s.AddFixes(ctx, d.ID, fixes); err != nil {
		t.Fatal(err)
	}
	got, err := s.Fixes(ctx, d.ID, time.Time{}, 100)
	if err != nil || len(got) != 3 || !got[0].At.Equal(fixes[1].At) || got[2] != fixes[3] {
		t.Fatalf("fixes = %+v %v", got, err)
	}
	// limit keeps the newest, still oldest first.
	got, _ = s.Fixes(ctx, d.ID, time.Time{}, 2)
	if len(got) != 2 || got[0].Lat != 40.71 || got[1].Lat != 40.72 {
		t.Fatalf("limited fixes = %+v", got)
	}
	got, _ = s.Fixes(ctx, d.ID, now.Add(-90*time.Second), 100)
	if len(got) != 2 {
		t.Fatalf("since filter = %+v", got)
	}
}

func TestUsersAndSessions(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	orphan, _ := s.CreateDog(ctx, "Old dog", 0)
	u, err := s.CreateUser(ctx, "sam@example.com", "hash1")
	if err != nil || u.ID == 0 {
		t.Fatalf("create user: %+v %v", u, err)
	}
	if _, err := s.CreateUser(ctx, "sam@example.com", "hash2"); err != ErrExists {
		t.Fatalf("duplicate email err = %v", err)
	}
	got, hash, err := s.UserByEmail(ctx, "sam@example.com")
	if err != nil || got.ID != u.ID || hash != "hash1" {
		t.Fatalf("by email = %+v %q %v", got, hash, err)
	}
	if _, _, err := s.UserByEmail(ctx, "nobody@example.com"); err != ErrNotFound {
		t.Fatalf("missing email err = %v", err)
	}
	if n, _ := s.CountUsers(ctx); n != 1 {
		t.Fatalf("count = %d", n)
	}

	// Ownership.
	other, _ := s.CreateUser(ctx, "kim@example.com", "h")
	mine, _ := s.CreateDog(ctx, "Rex", u.ID)
	s.CreateDog(ctx, "Kim's dog", other.ID)
	if n, err := s.ClaimOrphanDogs(ctx, u.ID); n < 1 || err != nil {
		t.Fatalf("claimed %d %v", n, err)
	}
	if again, _ := s.ClaimOrphanDogs(ctx, u.ID); again != 0 {
		t.Fatalf("claimed %d dogs twice", again)
	}
	owned := map[int64]bool{}
	dogs, _ := s.Dogs(ctx, u.ID)
	for _, d := range dogs {
		if d.OwnerID != u.ID {
			t.Fatalf("listed someone else's dog: %+v", d)
		}
		owned[d.ID] = true
	}
	if !owned[orphan.ID] || !owned[mine.ID] {
		t.Fatalf("sam's dogs = %+v", dogs)
	}
	if theirs, _ := s.Dogs(ctx, other.ID); len(theirs) != 1 || theirs[0].Name != "Kim's dog" {
		t.Fatalf("kim's dogs = %+v", theirs)
	}

	// Sessions.
	now := time.Now()
	if err := s.CreateSession(ctx, "tok", u.ID, time.Hour, now); err != nil {
		t.Fatal(err)
	}
	ss, err := s.Session(ctx, "tok", now)
	if err != nil || ss.UserID != u.ID || ss.TTL != time.Hour {
		t.Fatalf("session = %+v %v", ss, err)
	}
	if _, err := s.Session(ctx, "tok", now.Add(2*time.Hour)); err != ErrNotFound {
		t.Fatalf("expired session err = %v", err)
	}
	s.ExtendSession(ctx, "tok", now.Add(3*time.Hour))
	if _, err := s.Session(ctx, "tok", now.Add(2*time.Hour)); err != nil {
		t.Fatalf("extended session err = %v", err)
	}
	s.DeleteExpiredSessions(ctx, now.Add(4*time.Hour))
	if _, err := s.Session(ctx, "tok", now); err != ErrNotFound {
		t.Fatalf("cleaned-up session err = %v", err)
	}
	s.CreateSession(ctx, "tok2", u.ID, time.Hour, now)
	s.DeleteSession(ctx, "tok2")
	if _, err := s.Session(ctx, "tok2", now); err != ErrNotFound {
		t.Fatalf("deleted session err = %v", err)
	}
}

// A database from before accounts existed gets dogs.owner_id added.
func TestMigrateOwner(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") != "" {
		t.Skip("SQLite only")
	}
	path := t.TempDir() + "/old.db"
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE dogs (id INTEGER PRIMARY KEY, name TEXT NOT NULL, created_at TEXT NOT NULL, baseline TEXT);
		INSERT INTO dogs(name, created_at) VALUES ('Rex', '2026-10-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d, err := s.Dog(context.Background(), 1)
	if err != nil || d.Name != "Rex" || d.OwnerID != 0 {
		t.Fatalf("migrated dog = %+v %v", d, err)
	}
}
