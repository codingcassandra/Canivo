// Command gpssim pretends to be a GPS collar so the safe-zone feature can be
// tried without hardware. The fake dog plays in the yard, runs off, then
// comes back home, sending a fix every few seconds.
//
//	go run ./cmd/gpssim -server https://canivo.onrender.com -dog 1
//
// It uses the dog's safe zone as home, so set one in the app first.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"time"

	"github.com/codingcassandra/Canivo/internal/geo"
)

func main() {
	server := flag.String("server", "http://localhost:8090", "Canivo server URL")
	dog := flag.Int64("dog", 1, "dog id")
	key := flag.String("key", os.Getenv("CANIVO_API_KEY"), "API key (default $CANIVO_API_KEY)")
	every := flag.Duration("every", 5*time.Second, "time between fixes")
	yard := flag.Duration("yard", 30*time.Second, "time playing in the yard before running off")
	away := flag.Duration("away", 90*time.Second, "time running away before heading home")
	speed := flag.Float64("speed", 4, "running speed, m/s")
	flag.Parse()

	c := client{server: *server, dog: *dog, key: *key}
	var loc struct {
		Zone *geo.Zone `json:"zone"`
	}
	if err := c.do("GET", "/location?minutes=1", nil, &loc); err != nil {
		log.Fatal(err)
	}
	if loc.Zone == nil {
		log.Fatal("this dog has no safe zone yet; set one in the app (Location → Set home here)")
	}
	home := *loc.Zone
	log.Printf("home %.5f,%.5f radius %.0f m", home.Lat, home.Lon, home.RadiusM)

	// Position in meters east/north of home.
	var x, y float64
	heading := rand.Float64() * 2 * math.Pi
	start := time.Now()
	for range time.Tick(*every) {
		t := time.Since(start)
		step := *speed * every.Seconds()
		phase := "in the yard"
		switch {
		case t < *yard:
			// Wander, staying well inside the zone.
			x, y = jitter(x, home.RadiusM*0.3), jitter(y, home.RadiusM*0.3)
		case t < *yard+*away:
			phase = "running off"
			heading += (rand.Float64() - 0.5) * 0.6
			x += step * math.Cos(heading)
			y += step * math.Sin(heading)
		default:
			phase = "heading home"
			d := math.Hypot(x, y)
			if d <= step {
				x, y = 0, 0
				phase = "home"
			} else {
				x -= step * x / d
				y -= step * y / d
			}
		}
		lat := home.Lat + y/111195
		lon := home.Lon + x/(111195*math.Cos(home.Lat*math.Pi/180))
		fix := map[string]any{"t": time.Now().UnixMilli(), "lat": lat, "lon": lon, "accuracy_m": 5 + rand.Float64()*10}

		var resp struct {
			Away      bool     `json:"away"`
			DistanceM *float64 `json:"distance_m"`
		}
		if err := c.do("POST", "/locations", map[string]any{"fixes": []any{fix}}, &resp); err != nil {
			log.Print(err)
			continue
		}
		state := "inside zone"
		if resp.Away {
			state = "AWAY"
		}
		log.Printf("%-12s %4.0f m from home  → server: %s", phase, math.Hypot(x, y), state)
	}
}

func jitter(v, limit float64) float64 {
	v += (rand.Float64() - 0.5) * 6
	return math.Max(-limit, math.Min(limit, v))
}

type client struct {
	server, key string
	dog         int64
}

func (c client) do(method, path string, body, out any) error {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, err := http.NewRequest(method, fmt.Sprintf("%s/api/dogs/%d%s", c.server, c.dog, path), &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error string }
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("%s %s: %s %s", method, path, resp.Status, e.Error)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
