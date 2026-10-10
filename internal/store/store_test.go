package store

import (
	"context"
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

	d, err := s.CreateDog(ctx, "Rex")
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
	d, _ := s.CreateDog(ctx, "Rex")

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
