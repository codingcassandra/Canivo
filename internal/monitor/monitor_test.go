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

	// Dog spots something: fast breathing, tense muscles.
	s.RateBPM, s.Tension = 40, 320
	evs = feed(m, s, &now, 30000)
	if got := kinds(evs); len(got) != 1 || got[0] != AlertStarted {
		t.Fatalf("alert events = %v (status %+v)", got, m.Status())
	}

	s.RateBPM, s.Tension = 18, 200
	evs = feed(m, s, &now, 60000)
	if got := kinds(evs); len(got) != 1 || got[0] != AlertCleared {
		t.Fatalf("clear events = %v (status %+v)", got, m.Status())
	}
}

func TestBreathHoldFreezeAlerts(t *testing.T) {
	b := &Baseline{RateMean: 18, RateSD: 2, TensionMean: 232, TensionSD: 12}
	m := New(DefaultConfig(), b)
	s := breath.NewSynth(18, 200)
	var now int64
	if evs := feed(m, s, &now, 60000); len(evs) != 0 {
		t.Fatalf("calm events = %v", kinds(evs))
	}

	s.Amplitude, s.Tension = 0, 260 // frozen, holding breath, stiff
	evs := feed(m, s, &now, 30000)
	if got := kinds(evs); len(got) != 1 || got[0] != AlertStarted {
		t.Fatalf("freeze events = %v (status %+v)", got, m.Status())
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
