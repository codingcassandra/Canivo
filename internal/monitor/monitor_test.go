package monitor

import (
	"testing"
	"time"

	"github.com/codingcassandra/Canivo/internal/breath"
)

// feed generates durMs of synthetic signal at 50 Hz and collects events.
func feed(m *Monitor, s *breath.Synth, t *int64, durMs int64) []Event {
	var all []Event
	batch := make([]breath.Sample, 0, 50)
	for end := *t + durMs; *t < end; *t += 20 {
		batch = append(batch, s.Next(*t, 20))
		if len(batch) == 50 {
			_, evs := m.Ingest(batch)
			all = append(all, evs...)
			batch = batch[:0]
		}
	}
	_, evs := m.Ingest(batch)
	return append(all, evs...)
}

func kinds(evs []Event) []EventKind {
	var k []EventKind
	for _, e := range evs {
		k = append(k, e.Kind)
	}
	return k
}

func TestCalibrateThenAlertThenClear(t *testing.T) {
	m := New(DefaultConfig(), nil)
	s := breath.NewSynth(18, 200)
	var now int64

	evs := feed(m, s, &now, 15000+2*60000+2000)
	if got := kinds(evs); len(got) != 1 || got[0] != Calibrated {
		t.Fatalf("calibration events = %v", got)
	}
	b := m.Baseline()
	if b == nil || b.RateMean < 16 || b.RateMean > 20 {
		t.Fatalf("baseline = %+v", b)
	}

	if evs := feed(m, s, &now, 60000); len(evs) != 0 {
		t.Fatalf("calm dog produced events %v", kinds(evs))
	}

	// Dog spots something: noticeably faster breathing. Strong enough for an
	// alert, but not for danger.
	s.RateBPM, s.Tension = 26, 235
	evs = feed(m, s, &now, 60000)
	if got := kinds(evs); len(got) != 1 || got[0] != AlertStarted {
		t.Fatalf("alert events = %v (status %+v)", got, m.Status())
	}
	if st := m.Status().State; st != Alert {
		t.Fatalf("state = %v, want alert", st)
	}

	s.RateBPM, s.Tension = 18, 200
	evs = feed(m, s, &now, 60000)
	if got := kinds(evs); len(got) != 1 || got[0] != AlertCleared {
		t.Fatalf("clear events = %v (status %+v)", got, m.Status())
	}
}

func TestBreathHoldFreezeAlertsThenDanger(t *testing.T) {
	b := &Baseline{RateMean: 18, RateSD: 2, TensionMean: 232, TensionSD: 12}
	m := New(DefaultConfig(), b)
	s := breath.NewSynth(18, 200)
	var now int64
	if evs := feed(m, s, &now, 60000); len(evs) != 0 {
		t.Fatalf("calm events = %v", kinds(evs))
	}

	// Frozen, holding breath, stiff. The breathing rate falls away as the
	// window empties, so this alerts first and, once the breath-hold is
	// complete, escalates to danger.
	s.Amplitude, s.Tension = 0, 260
	evs := feed(m, s, &now, 30000)
	want := []EventKind{AlertStarted, DangerStarted}
	if got := kinds(evs); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("freeze events = %v, want %v (status %+v)", got, want, m.Status())
	}
}

func TestStrongReactionEscalatesToDangerThenStepsDown(t *testing.T) {
	b := &Baseline{RateMean: 18, RateSD: 2, TensionMean: 232, TensionSD: 12}
	m := New(DefaultConfig(), b)
	s := breath.NewSynth(18, 200)
	var now int64
	feed(m, s, &now, 30000)

	s.RateBPM, s.Tension = 40, 320 // fast breathing and very tense
	evs := feed(m, s, &now, 40000)
	want := []EventKind{AlertStarted, DangerStarted}
	if got := kinds(evs); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("escalation events = %v, want %v (status %+v)", got, want, m.Status())
	}
	if st := m.Status().State; st != Danger {
		t.Fatalf("state = %v, want danger", st)
	}

	// Back to normal: danger steps down to alert, then the alert clears.
	s.RateBPM, s.Tension = 18, 200
	evs = feed(m, s, &now, 90000)
	want = []EventKind{DangerCleared, AlertCleared}
	if got := kinds(evs); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("recovery events = %v, want %v (status %+v)", got, want, m.Status())
	}
}

// holdScore drives the state machine directly with one reading per second at a
// chosen arousal score. With this baseline the score is |rate-18|/4 (tension
// is held at its mean), so rate = 18 + 4*score.
func holdScore(m *Monitor, now *int64, score float64, secs int) []Event {
	var evs []Event
	for i := 0; i < secs; i++ {
		*now += 1000
		f := breath.Features{At: *now, RateBPM: 18 + 4*score, Tension: 232, Ready: true}
		if ev, ok := m.evaluate(f); ok {
			evs = append(evs, ev)
		}
	}
	return evs
}

func newCalmMonitor() *Monitor {
	return New(DefaultConfig(), &Baseline{RateMean: 18, RateSD: 2, TensionMean: 232, TensionSD: 12})
}

func wantKinds(t *testing.T, label string, evs []Event, want ...EventKind) {
	t.Helper()
	got := kinds(evs)
	if len(got) != len(want) {
		t.Fatalf("%s: events = %v, want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: events = %v, want %v", label, got, want)
		}
	}
}

func TestCalmGoesStraightToDanger(t *testing.T) {
	m := newCalmMonitor()
	var now int64

	// Not held long enough yet (under the 3s sustain).
	wantKinds(t, "short", holdScore(m, &now, 5, 2))
	// Held long enough: one danger event, with no separate alert event.
	wantKinds(t, "sustained", holdScore(m, &now, 5, 3), DangerStarted)
	if st := m.Status().State; st != Danger {
		t.Fatalf("state = %v, want danger", st)
	}
}

func TestAlertEscalatesToDangerAndStepsBackDown(t *testing.T) {
	m := newCalmMonitor()
	var now int64

	wantKinds(t, "alert", holdScore(m, &now, 3.0, 8), AlertStarted)
	wantKinds(t, "danger", holdScore(m, &now, 5.0, 8), DangerStarted)

	// Between the step-down score (3.0) and the danger score (4.0) it stays
	// in danger instead of flapping.
	wantKinds(t, "hysteresis", holdScore(m, &now, 3.5, 30))
	if st := m.Status().State; st != Danger {
		t.Fatalf("state = %v, want danger", st)
	}

	// Below the step-down score it drops to alert, not straight to calm,
	// even though 2.0 is above the alert-clear score.
	wantKinds(t, "step down", holdScore(m, &now, 2.0, 12), DangerCleared)
	if st := m.Status().State; st != Alert {
		t.Fatalf("state = %v, want alert", st)
	}

	// Even a near-zero score takes a second hold to clear the alert.
	wantKinds(t, "clear", holdScore(m, &now, 0.5, 12), AlertCleared)
	if st := m.Status().State; st != Calm {
		t.Fatalf("state = %v, want calm", st)
	}
}

func TestDangerNeverSkipsStraightToCalm(t *testing.T) {
	m := newCalmMonitor()
	var now int64
	wantKinds(t, "danger", holdScore(m, &now, 6, 8), DangerStarted)

	wantKinds(t, "step down", holdScore(m, &now, 0.2, 11), DangerCleared)
	if st := m.Status().State; st != Alert {
		t.Fatalf("state = %v, want alert", st)
	}
	wantKinds(t, "clear", holdScore(m, &now, 0.2, 11), AlertCleared)
}

func TestBriefDangerSpikeStaysAlert(t *testing.T) {
	m := newCalmMonitor()
	var now int64
	wantKinds(t, "alert", holdScore(m, &now, 3.0, 8), AlertStarted)

	// A 2s spike above the danger score, under the 3s sustain, is ignored.
	wantKinds(t, "spike", holdScore(m, &now, 5.0, 2))
	wantKinds(t, "after", holdScore(m, &now, 3.0, 20))
	if st := m.Status().State; st != Alert {
		t.Fatalf("state = %v, want alert", st)
	}
}

func TestBriefSpikeDoesNotAlert(t *testing.T) {
	b := &Baseline{RateMean: 18, RateSD: 2, TensionMean: 232, TensionSD: 12}
	m := New(DefaultConfig(), b)
	s := breath.NewSynth(18, 200)
	var now int64
	feed(m, s, &now, 30000)

	s.Tension = 320
	evs := feed(m, s, &now, 2000) // 2s spike, under the 3s sustain
	s.Tension = 200
	evs = append(evs, feed(m, s, &now, 30000)...)
	if len(evs) != 0 {
		t.Fatalf("brief spike produced %v", kinds(evs))
	}
}

// How soon an alert fires after the dog changes, and that a calm dog never
// alerts. These pin down the speed vs. false-alarm trade-off.
func TestAlertLatency(t *testing.T) {
	for _, c := range []struct {
		name          string
		rate, tension float64
		within        time.Duration
	}{
		// Before the latency work these took 12, 12, 12 and 18 s.
		{"fast breathing and tense", 40, 320, 7 * time.Second},
		{"tense only", 18, 320, 7 * time.Second},
		{"fast breathing only", 40, 200, 9 * time.Second},
		{"moderate", 30, 260, 10 * time.Second},
	} {
		m := New(DefaultConfig(), nil)
		s := breath.NewSynth(18, 200)
		var now int64
		feed(m, s, &now, 15000+2*60000+5000) // calibrate
		s.RateBPM, s.Tension = c.rate, c.tension
		start := now
		for m.Status().State != Alert && now-start < 30000 {
			feed(m, s, &now, 100)
		}
		if took := time.Duration(now-start) * time.Millisecond; m.Status().State != Alert || took > c.within {
			t.Errorf("%s: alert after %v, want within %v (state %s)", c.name, took, c.within, m.Status().State)
		}
	}
}

func TestCalmDogNeverAlerts(t *testing.T) {
	for _, rate := range []float64{8, 12, 18, 30} {
		m := New(DefaultConfig(), nil)
		s := breath.NewSynth(rate, 200)
		var now int64
		if evs := feed(m, s, &now, 30*60000); len(evs) != 1 || evs[0].Kind != Calibrated {
			t.Errorf("calm dog at %v bpm for 30 min: events %v (status %+v)", rate, kinds(evs), m.Status())
		}
	}
}
