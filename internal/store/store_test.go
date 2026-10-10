package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/codingcassandra/Canivo/internal/monitor"
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
}
