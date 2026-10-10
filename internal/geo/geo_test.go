package geo

import (
	"math"
	"testing"
	"time"
)

func TestDistance(t *testing.T) {
	// One degree of latitude is ~111.2 km everywhere.
	if d := Distance(40, -74, 41, -74); math.Abs(d-111195) > 100 {
		t.Fatalf("1 deg lat = %.0f m", d)
	}
	if d := Distance(40.7, -74, 40.7, -74); d != 0 {
		t.Fatalf("same point = %f", d)
	}
}

// north returns the point m meters north of the zone center.
func north(z Zone, m float64) (float64, float64) {
	return z.Lat + m/111195, z.Lon
}

func TestFence(t *testing.T) {
	z := Zone{Lat: 40.7, Lon: -74, RadiusM: 100}
	var f Fence
	t0 := time.Now()
	step := func(m, acc float64) Event {
		lat, lon := north(z, m)
		t0 = t0.Add(10 * time.Second)
		return f.Update(z, Fix{At: t0, Lat: lat, Lon: lon, AccuracyM: acc})
	}

	// Wandering the yard, and a jittery fix that could be inside: no alert.
	for _, m := range []float64{0, 50, 90, 120, 60} {
		if ev := step(m, 30); ev != None {
			t.Fatalf("at %v m: %v", m, ev)
		}
	}
	// A single clearly-outside fix isn't enough.
	if ev := step(300, 10); ev != None {
		t.Fatal("one fix flipped the fence")
	}
	if ev := step(50, 10); ev != None {
		t.Fatal("unexpected event")
	}
	// Vague fixes are ignored entirely.
	if ev := step(1000, 500); ev != None {
		t.Fatal("vague fix counted")
	}
	// Two clear fixes outside: left.
	step(200, 10)
	if ev := step(260, 10); ev != Left {
		t.Fatalf("expected Left, got %v", ev)
	}
	if away, since := f.Away(); !away || !since.Equal(t0) {
		t.Fatalf("away=%v since=%v", away, since)
	}
	// Further away: still just away.
	if ev := step(800, 10); ev != None {
		t.Fatal("re-fired")
	}
	// Back inside for two fixes: returned.
	step(80, 20)
	if ev := step(40, 20); ev != Returned {
		t.Fatalf("expected Returned, got %v", ev)
	}
	if away, _ := f.Away(); away {
		t.Fatal("still away")
	}
}
