// Package geo checks a dog's GPS position against a safe zone (a circle
// around home) and decides when the dog has really left it or come back.
package geo

import (
	"math"
	"time"
)

// Fix is one GPS reading from the collar.
type Fix struct {
	At        time.Time `json:"at"`
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	AccuracyM float64   `json:"accuracy_m"` // radius of uncertainty, meters
}

// Zone is the safe area: a circle around home.
type Zone struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	RadiusM float64 `json:"radius_m"`
}

const earthRadiusM = 6371000

// Distance returns the great-circle distance in meters (haversine).
func Distance(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(a)))
}

// DistanceFrom returns how far a fix is from the zone's center, in meters.
func (z Zone) DistanceFrom(f Fix) float64 { return Distance(z.Lat, z.Lon, f.Lat, f.Lon) }

type Event int

const (
	None     Event = iota
	Left           // the dog left the safe zone
	Returned       // the dog is back inside
)

const (
	// MaxAccuracyM: fixes vaguer than this are ignored (GPS just woke up,
	// indoors, ...). They are still shown, but never trigger or clear an alert.
	MaxAccuracyM = 100
	// confirmFixes in a row are needed to change state, so one bad
	// reading doesn't cause a false alarm.
	confirmFixes = 2
)

// Fence tracks whether the dog is away from the zone. GPS jitters, so:
//   - a fix counts as outside only if even its accuracy circle is outside
//     the zone (definitely out);
//   - a fix counts as inside if its center is inside the zone;
//   - it takes confirmFixes consecutive fixes to flip state.
//
// The zero value is "home".
type Fence struct {
	away   bool
	since  time.Time
	streak int
}

// Update feeds one fix and reports whether the state changed.
func (f *Fence) Update(z Zone, fix Fix) Event {
	if fix.AccuracyM > MaxAccuracyM {
		return None
	}
	d := z.DistanceFrom(fix)
	var flip bool
	if f.away {
		flip = d <= z.RadiusM
	} else {
		flip = d-fix.AccuracyM > z.RadiusM
	}
	if !flip {
		f.streak = 0
		return None
	}
	f.streak++
	if f.streak < confirmFixes {
		return None
	}
	f.streak = 0
	f.away = !f.away
	f.since = fix.At
	if f.away {
		return Left
	}
	return Returned
}

// Away reports whether the dog is out of the zone, and since when.
func (f *Fence) Away() (bool, time.Time) { return f.away, f.since }

// Reset forgets the state (e.g. the zone moved).
func (f *Fence) Reset() { *f = Fence{} }
