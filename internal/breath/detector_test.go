package breath

import (
	"math"
	"testing"
)

func run(d *Detector, s *Synth, startT, durMs int64) int64 {
	const dt = 20 // 50 Hz, what the firmware sends
	t := startT
	for ; t < startT+durMs; t += dt {
		d.Push(s.Next(t, dt))
	}
	return t
}

func TestDetectsBreathingRate(t *testing.T) {
	for _, rate := range []float64{12, 20, 35, 60} {
		d := NewDetector(15000)
		run(d, NewSynth(rate, 200), 0, 40000)
		f := d.Features()
		if !f.Ready {
			t.Fatalf("rate %v: features not ready", rate)
		}
		if math.Abs(f.RateBPM-rate) > rate*0.1 {
			t.Errorf("rate %v: got %.1f bpm", rate, f.RateBPM)
		}
		if math.Abs(f.Tension-200-80*0.4*2/math.Pi) > 10 {
			t.Errorf("rate %v: tension %.1f", rate, f.Tension)
		}
	}
}

func TestRateDropsWhenBreathingStops(t *testing.T) {
	d := NewDetector(15000)
	s := NewSynth(20, 200)
	end := run(d, s, 0, 30000)

	s.Amplitude = 0 // breath hold
	run(d, s, end, 10000)
	if f := d.Features(); f.RateBPM > 10 {
		t.Errorf("after 10s breath hold rate = %.1f, want < 10", f.RateBPM)
	}
}

func TestResetsOnClockJump(t *testing.T) {
	d := NewDetector(15000)
	run(d, NewSynth(20, 200), 100000, 20000)
	if !d.Features().Ready {
		t.Fatal("expected ready")
	}
	d.Push(Sample{T: 5, V: 200}) // Arduino rebooted, millis() restarted
	if d.Features().Ready {
		t.Error("expected detector to restart after clock went backwards")
	}
}
