package breath

import (
	"math"
	"math/rand/v2"
)

// Synth generates a fake EMG envelope for tests and the bridge's simulate
// mode: a resting level plus one smooth bump per breath, plus noise.
type Synth struct {
	RateBPM   float64 // breaths per minute
	Tension   float64 // resting envelope level
	Amplitude float64 // height of each breath bump
	Noise     float64 // uniform noise amplitude

	phase float64 // position within the current breath, 0..1
	rng   *rand.Rand
}

// NewSynth returns a generator with a fixed seed so runs are repeatable.
func NewSynth(rateBPM, tension float64) *Synth {
	return &Synth{
		RateBPM:   rateBPM,
		Tension:   tension,
		Amplitude: 80,
		Noise:     6,
		rng:       rand.New(rand.NewPCG(1, 2)),
	}
}

// Next advances the generator by dtMs and returns the sample at time t.
func (s *Synth) Next(t, dtMs int64) Sample {
	s.phase += float64(dtMs) * s.RateBPM / 60000
	s.phase -= math.Floor(s.phase)
	// Inspiration burst during the first 40% of each breath.
	bump := 0.0
	if s.phase < 0.4 {
		bump = math.Sin(math.Pi * s.phase / 0.4)
	}
	noise := (s.rng.Float64()*2 - 1) * s.Noise
	return Sample{T: t, V: s.Tension + s.Amplitude*bump + noise}
}
