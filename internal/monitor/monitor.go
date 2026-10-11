// Package monitor decides when a dog is on high alert by comparing its live
// breathing features against its own calm baseline.
//
// Flow: calibrating (learn the baseline while the dog is calm) -> calm <-> alert.
// The arousal score is a weighted z-score: how far the breathing rate has moved
// from baseline (either direction — alert dogs often freeze and hold their
// breath) plus how much muscle tension has risen.
package monitor

import (
	"math"
	"time"

	"github.com/codingcassandra/Canivo/internal/breath"
)

type State string

const (
	Calibrating State = "calibrating"
	Calm        State = "calm"
	Alert       State = "alert"
)

type Config struct {
	WindowMs      int64         // feature window for the breath detector
	Calibration   time.Duration // how long of calm signal to learn from
	AlertScore    float64       // score that starts an alert...
	AlertSustain  time.Duration // ...if held this long
	ClearScore    float64       // score below which an alert clears...
	ClearSustain  time.Duration // ...if held this long
	RateWeight    float64
	TensionWeight float64
}

func DefaultConfig() Config {
	return Config{
		WindowMs:      15000,
		Calibration:   2 * time.Minute,
		AlertScore:    2.5,
		AlertSustain:  3 * time.Second,
		ClearScore:    1.5,
		ClearSustain:  10 * time.Second,
		RateWeight:    0.5,
		TensionWeight: 0.5,
	}
}

// Baseline is a dog's calm-state reference, persisted between restarts.
type Baseline struct {
	RateMean    float64 `json:"rate_mean"`
	RateSD      float64 `json:"rate_sd"`
	TensionMean float64 `json:"tension_mean"`
	TensionSD   float64 `json:"tension_sd"`
	Points      int     `json:"points"`
}

type Status struct {
	State       State     `json:"state"`
	RateBPM     float64   `json:"rate_bpm"`
	Tension     float64   `json:"tension"`
	Score       float64   `json:"score"`
	Calibration float64   `json:"calibration_progress"` // 0..1
	Baseline    *Baseline `json:"baseline,omitempty"`
}

type EventKind string

const (
	AlertStarted EventKind = "alert_started"
	AlertCleared EventKind = "alert_cleared"
	Calibrated   EventKind = "calibrated"
)

type Event struct {
	Kind    EventKind
	Score   float64
	RateBPM float64
	Tension float64
}

// Monitor is not safe for concurrent use; callers hold a lock per dog.
type Monitor struct {
	cfg      Config
	det      *breath.Detector
	state    State
	baseline *Baseline
	last     Status

	// calibration accumulators
	calStart  int64
	calN      int
	rSum, rSq float64
	tSum, tSq float64

	lastEval  int64 // device ms of last feature evaluation
	crossedAt int64 // device ms when the score first crossed the active threshold; -1 if not crossed
}

// New creates a monitor. Pass a stored baseline to skip calibration.
func New(cfg Config, b *Baseline) *Monitor {
	m := &Monitor{cfg: cfg, det: breath.NewDetector(cfg.WindowMs)}
	m.reset(b)
	return m
}

// Recalibrate discards the baseline and starts learning a new one.
func (m *Monitor) Recalibrate() { m.reset(nil) }

func (m *Monitor) reset(b *Baseline) {
	m.baseline = b
	m.state = Calm
	if b == nil {
		m.state = Calibrating
	}
	m.calStart, m.calN = -1, 0
	m.rSum, m.rSq, m.tSum, m.tSq = 0, 0, 0, 0
	m.crossedAt = -1
	m.lastEval = 0
	m.last = Status{State: m.state, Baseline: b}
}

func (m *Monitor) Status() Status { return m.last }

func (m *Monitor) Baseline() *Baseline { return m.baseline }

// Ingest feeds samples and returns the latest status plus any state changes.
func (m *Monitor) Ingest(samples []breath.Sample) (Status, []Event) {
	var events []Event
	for _, s := range samples {
		before := m.det.Features().At
		m.det.Push(s)
		f := m.det.Features()
		if f.At < before { // detector restarted (device reboot); restart timers
			m.lastEval, m.crossedAt, m.calStart = 0, -1, -1
		}
		if !f.Ready || f.At-m.lastEval < 1000 {
			continue // evaluate once per second of signal
		}
		m.lastEval = f.At
		if ev, ok := m.evaluate(f); ok {
			events = append(events, ev)
		}
	}
	return m.last, events
}

func (m *Monitor) evaluate(f breath.Features) (Event, bool) {
	m.last.RateBPM, m.last.Tension = f.RateBPM, f.Tension

	if m.state == Calibrating {
		return m.calibrate(f)
	}

	score := m.score(f)
	m.last.Score = score
	ev := Event{Score: score, RateBPM: f.RateBPM, Tension: f.Tension}

	switch m.state {
	case Calm:
		if m.sustained(score >= m.cfg.AlertScore, f.At, m.cfg.AlertSustain) {
			m.state, m.last.State = Alert, Alert
			ev.Kind = AlertStarted
			return ev, true
		}
	case Alert:
		if m.sustained(score < m.cfg.ClearScore, f.At, m.cfg.ClearSustain) {
			m.state, m.last.State = Calm, Calm
			ev.Kind = AlertCleared
			return ev, true
		}
	}
	return Event{}, false
}

// sustained reports whether cond has held continuously for d.
func (m *Monitor) sustained(cond bool, at int64, d time.Duration) bool {
	if !cond {
		m.crossedAt = -1
		return false
	}
	if m.crossedAt < 0 {
		m.crossedAt = at
	}
	if at-m.crossedAt >= d.Milliseconds() {
		m.crossedAt = -1
		return true
	}
	return false
}

func (m *Monitor) score(f breath.Features) float64 {
	b := m.baseline
	zRate := math.Abs(f.RateBPM-b.RateMean) / b.RateSD
	zTension := math.Max(0, (f.Tension-b.TensionMean)/b.TensionSD)
	return m.cfg.RateWeight*zRate + m.cfg.TensionWeight*zTension
}

func (m *Monitor) calibrate(f breath.Features) (Event, bool) {
	if m.calStart < 0 {
		m.calStart = f.At
	}
	m.calN++
	m.rSum += f.RateBPM
	m.rSq += f.RateBPM * f.RateBPM
	m.tSum += f.Tension
	m.tSq += f.Tension * f.Tension

	elapsed := f.At - m.calStart
	m.last.Calibration = math.Min(1, float64(elapsed)/float64(m.cfg.Calibration.Milliseconds()))
	if elapsed < m.cfg.Calibration.Milliseconds() {
		return Event{}, false
	}

	n := float64(m.calN)
	rMean, tMean := m.rSum/n, m.tSum/n
	b := &Baseline{
		RateMean:    rMean,
		TensionMean: tMean,
		// Floors stop a very steady calibration from making tiny changes
		// look like huge z-scores.
		RateSD:    math.Max(sd(m.rSq, rMean, n), 2),
		TensionSD: math.Max(sd(m.tSq, tMean, n), math.Max(0.05*math.Abs(tMean), 1)),
		Points:    m.calN,
	}
	m.reset(b)
	m.last.RateBPM, m.last.Tension, m.last.Calibration = f.RateBPM, f.Tension, 1
	return Event{Kind: Calibrated, RateBPM: f.RateBPM, Tension: f.Tension}, true
}

func sd(sumSq, mean, n float64) float64 {
	return math.Sqrt(math.Max(0, sumSq/n-mean*mean))
}
