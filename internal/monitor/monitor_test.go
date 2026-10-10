package monitor

import (
	"testing"

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
	evs := feed(m, s, &now, 2000) // 2s spike, under the 5s sustain
	s.Tension = 200
	evs = append(evs, feed(m, s, &now, 30000)...)
	if len(evs) != 0 {
		t.Fatalf("brief spike produced %v", kinds(evs))
	}
}
