// Package breath turns a stream of EMG envelope samples into breathing
// features: breaths per minute and overall muscle tension.
//
// Each breath shows up as a burst of respiratory-muscle activity, i.e. a
// bump in the EMG envelope. The detector smooths the signal, subtracts a slow
// moving trend, and counts upward crossings with hysteresis.
package breath

import "math"

// Sample is one reading from the sensor. T is the device timestamp in
// milliseconds (Arduino millis()), V is the EMG envelope value (ADC units).
type Sample struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}

// Features summarizes the most recent window of signal.
type Features struct {
	At      int64   `json:"at"`       // device ms of the newest sample
	RateBPM float64 `json:"rate_bpm"` // breaths per minute
	Tension float64 `json:"tension"`  // mean smoothed envelope over the window
	Breaths int     `json:"breaths"`  // breaths detected in the window
	Ready   bool    `json:"ready"`    // false until a full window has been seen
}

const (
	smoothTauMs   = 150.0  // removes sample noise, keeps breath bumps
	trendTauMs    = 4000.0 // slow baseline the bumps are measured against
	minBreathGap  = 250    // ms; caps detection at 240 bpm
	maxSampleGap  = 2000   // ms; a bigger gap (or a clock reset) restarts detection
	triggerFactor = 0.5    // breath triggers at trend + 0.5 * running SD
)

// Detector is not safe for concurrent use.
type Detector struct {
	windowMs int64

	started bool
	firstT  int64
	lastT   int64

	smooth, trend, variance float64
	armed                   bool

	breaths []int64  // breath onset times within the window
	levels  []tlevel // smoothed values within the window
	sum     float64  // running sum of levels[].v
}

type tlevel struct {
	t int64
	v float64
}

// NewDetector returns a detector that reports features over windowMs.
func NewDetector(windowMs int64) *Detector {
	return &Detector{windowMs: windowMs}
}

// Reset clears all state, e.g. after the device restarts.
func (d *Detector) Reset() {
	*d = Detector{windowMs: d.windowMs}
}

// Push feeds one sample. Samples must arrive in timestamp order; duplicates
// are ignored and backwards jumps or long gaps restart detection.
func (d *Detector) Push(s Sample) {
	if d.started {
		dt := s.T - d.lastT
		if dt == 0 {
			return
		}
		if dt < 0 || dt > maxSampleGap {
			d.Reset()
		}
	}
	if !d.started {
		d.started = true
		d.firstT, d.lastT = s.T, s.T
		d.smooth, d.trend = s.V, s.V
		d.addLevel(s.T, s.V)
		return
	}

	dt := float64(s.T - d.lastT)
	d.lastT = s.T
	d.smooth = ema(d.smooth, s.V, dt, smoothTauMs)
	d.trend = ema(d.trend, d.smooth, dt, trendTauMs)
	dev := d.smooth - d.trend
	d.variance = ema(d.variance, dev*dev, dt, trendTauMs)
	sd := math.Sqrt(d.variance)

	// Re-arm once the signal falls back below trend, then count the next
	// rise above trend + k*SD as one breath.
	if dev < 0 {
		d.armed = true
	} else if d.armed && sd > 0 && dev > triggerFactor*sd {
		if len(d.breaths) == 0 || s.T-d.breaths[len(d.breaths)-1] >= minBreathGap {
			d.breaths = append(d.breaths, s.T)
			d.armed = false
		}
	}

	d.addLevel(s.T, d.smooth)
	d.trim(s.T - d.windowMs)
}

// Features returns the current window summary.
func (d *Detector) Features() Features {
	f := Features{At: d.lastT, Breaths: len(d.breaths)}
	if !d.started {
		return f
	}
	f.Ready = d.lastT-d.firstT >= d.windowMs
	if n := len(d.levels); n > 0 {
		f.Tension = d.sum / float64(n)
	}
	f.RateBPM = d.rate()
	return f
}

func (d *Detector) rate() float64 {
	n := len(d.breaths)
	countRate := float64(n) * 60000 / float64(d.windowMs)
	if n < 2 {
		return countRate
	}
	// Mean breath interval is more precise than a raw count, but only while
	// breathing is ongoing. If the dog has paused (e.g. holding its breath),
	// fall back to the count so the rate drops.
	span := float64(d.breaths[n-1] - d.breaths[0])
	mean := span / float64(n-1)
	if float64(d.lastT-d.breaths[n-1]) > 2*mean {
		return countRate
	}
	return 60000 / mean
}

func (d *Detector) addLevel(t int64, v float64) {
	d.levels = append(d.levels, tlevel{t, v})
	d.sum += v
}

func (d *Detector) trim(cutoff int64) {
	i := 0
	for i < len(d.levels) && d.levels[i].t < cutoff {
		d.sum -= d.levels[i].v
		i++
	}
	d.levels = d.levels[i:]

	j := 0
	for j < len(d.breaths) && d.breaths[j] < cutoff {
		j++
	}
	d.breaths = d.breaths[j:]
}

// ema is an exponential moving average that handles uneven sample spacing.
func ema(prev, x, dtMs, tauMs float64) float64 {
	alpha := 1 - math.Exp(-dtMs/tauMs)
	return prev + alpha*(x-prev)
}
