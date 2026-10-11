// Command bridge forwards EMG readings from the Arduino Nano (USB serial) to
// the Canivo server. The Nano has no WiFi/Bluetooth, so this runs on whatever
// it is plugged into: a laptop, a Raspberry Pi, etc.
//
//	go run ./cmd/bridge -dog 1                  # finds the Arduino on USB
//	go run ./cmd/bridge -dog 1 -simulate        # no hardware needed
//
// For a deployed server, pass -server https://... and the API key via -key or
// the CANIVO_API_KEY environment variable.
//
// Serial format (one line per sample, 115200 baud): "<millis>,<value>".
// Lines starting with '#' are logged and otherwise ignored.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codingcassandra/Canivo/internal/breath"
	"go.bug.st/serial"
)

func main() {
	server := flag.String("server", "http://localhost:8090", "Canivo server URL")
	dog := flag.Int64("dog", 0, "dog id to send samples for (required)")
	key := flag.String("key", os.Getenv("CANIVO_API_KEY"), "server API key (default $CANIVO_API_KEY)")
	port := flag.String("port", "", "serial port, e.g. /dev/cu.usbmodem101 (default: find the Arduino; see -list)")
	baud := flag.Int("baud", 115200, "serial baud rate; must match the sketch")
	list := flag.Bool("list", false, "list serial ports and exit")
	simulate := flag.Bool("simulate", false, "generate fake breathing instead of reading serial")
	calm := flag.Duration("calm", 3*time.Minute, "simulate: calm time before the first alert episode")
	every := flag.Duration("every", 90*time.Second, "simulate: time between alert episodes")
	send := flag.Duration("send", 200*time.Millisecond, "how often to send samples to the server (lower = less delay)")
	flag.Parse()

	if *list {
		ports, err := serial.GetPortsList()
		if err != nil {
			log.Fatal(err)
		}
		for _, p := range ports {
			fmt.Println(p)
		}
		return
	}
	if *dog == 0 {
		log.Fatal("-dog is required (create one with POST /api/dogs)")
	}
	*key = strings.Trim(strings.TrimSpace(*key), `"'`) // pasted keys often carry quotes or a newline

	if !*simulate && *port == "" {
		p, err := findArduino()
		if err != nil {
			log.Fatal(err)
		}
		*port = p
	}

	samples := make(chan breath.Sample, 1000)
	switch {
	case *simulate:
		go runSimulator(samples, *calm, *every)
	case *port != "":
		p, err := serial.Open(*port, &serial.Mode{BaudRate: *baud})
		if err != nil {
			log.Fatalf("open %s: %v", *port, err)
		}
		defer p.Close()
		log.Printf("reading %s at %d baud", *port, *baud)
		go readSerial(p, samples)
	}

	forward(fmt.Sprintf("%s/api/dogs/%d/samples", strings.TrimRight(*server, "/"), *dog), *key, samples, *send)
}

// findArduino returns the one USB serial port that looks like an Arduino. Its
// name depends on which USB socket it's in, so it's looked up each run.
func findArduino() (string, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return "", err
	}
	var found []string
	for _, p := range ports {
		// macOS lists each device twice (tty.* and cu.*); cu.* is the one to open.
		if strings.HasPrefix(p, "/dev/tty.") {
			continue
		}
		for _, k := range []string{"usbmodem", "usbserial", "wchusbserial", "ttyACM", "ttyUSB", "COM"} {
			if strings.Contains(p, k) {
				found = append(found, p)
				break
			}
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no Arduino found: check the USB cable is plugged in (and is a data cable), or pass -port (see -list)")
	case 1:
		log.Printf("found Arduino on %s", found[0])
		return found[0], nil
	default:
		return "", fmt.Errorf("several USB serial ports (%s): pick one with -port", strings.Join(found, ", "))
	}
}

// readSerial parses "millis,value" lines into samples.
func readSerial(r io.Reader, out chan<- breath.Sample) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			log.Printf("nano: %s", line)
			continue
		}
		s, err := parseLine(line)
		if err != nil {
			continue // partial line right after connecting, line noise, etc.
		}
		out <- s
	}
	log.Fatalf("serial closed: %v", sc.Err())
}

func parseLine(line string) (breath.Sample, error) {
	ts, vs, ok := strings.Cut(line, ",")
	if !ok {
		return breath.Sample{}, fmt.Errorf("bad line %q", line)
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return breath.Sample{}, err
	}
	v, err := strconv.ParseFloat(vs, 64)
	if err != nil {
		return breath.Sample{}, err
	}
	return breath.Sample{T: t, V: v}, nil
}

// runSimulator produces 50 Hz fake data in real time: calm, then repeating
// 30-second alert episodes (fast breathing, tense muscles).
func runSimulator(out chan<- breath.Sample, calm, every time.Duration) {
	const dt = 20
	s := breath.NewSynth(18, 200)
	start := time.Now()
	tick := time.NewTicker(dt * time.Millisecond)
	defer tick.Stop()
	alerting := false
	for range tick.C {
		el := time.Since(start)
		want := el > calm && (el-calm)%every < 30*time.Second
		if want != alerting {
			alerting = want
			if alerting {
				s.RateBPM, s.Tension = 40, 320
				log.Print("simulate: dog spotted something (alert episode)")
			} else {
				s.RateBPM, s.Tension = 18, 200
				log.Print("simulate: dog relaxed")
			}
		}
		out <- s.Next(el.Milliseconds(), dt)
	}
}

// forward POSTs whatever samples have arrived every `every` (a slow network
// just makes batches bigger), printing state changes and a status line a second.
func forward(url, key string, in <-chan breath.Sample, every time.Duration) {
	client := &http.Client{Timeout: 10 * time.Second}
	tick := time.NewTicker(every)
	defer tick.Stop()
	var batch []breath.Sample
	lastState := ""
	var lastLog time.Time
	for {
		select {
		case s := <-in:
			batch = append(batch, s)
		case <-tick.C:
			if len(batch) == 0 {
				continue
			}
			status, err := post(client, url, key, batch)
			if errors.Is(err, errUnauthorized) {
				if key == "" {
					log.Fatal("the server needs an API key: run  export CANIVO_API_KEY='<API_KEY from Render → Environment>'  and try again")
				}
				log.Fatalf("the server rejected the API key (%d characters, starts %q): copy API_KEY again from Render → Environment, run  export CANIVO_API_KEY='...'  in this terminal, and try again",
					len(key), key[:min(4, len(key))])
			}
			if err != nil {
				log.Printf("send: %v (keeping %d samples)", err, len(batch))
				if len(batch) > 50*60 { // don't grow forever while the server is down
					batch = batch[len(batch)-50*60:]
				}
				continue
			}
			batch = batch[:0]
			if status.State != lastState {
				log.Printf("state: %s", status.State)
				lastState = status.State
			}
			if time.Since(lastLog) < time.Second {
				continue
			}
			lastLog = time.Now()
			log.Printf("%-11s rate=%5.1f bpm  tension=%6.1f  score=%4.1f  calibration=%3.0f%%",
				status.State, status.RateBPM, status.Tension, status.Score, status.Calibration*100)
		}
	}
}

type status struct {
	State       string  `json:"state"`
	RateBPM     float64 `json:"rate_bpm"`
	Tension     float64 `json:"tension"`
	Score       float64 `json:"score"`
	Calibration float64 `json:"calibration_progress"`
}

var errUnauthorized = errors.New("unauthorized")

func post(c *http.Client, url, key string, batch []breath.Sample) (status, error) {
	var st status
	body, err := json.Marshal(map[string]any{"samples": batch})
	if err != nil {
		return st, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return st, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := c.Do(req)
	if err != nil {
		return st, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return st, errUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return st, fmt.Errorf("server: %s %s", resp.Status, bytes.TrimSpace(msg))
	}
	return st, json.NewDecoder(resp.Body).Decode(&st)
}
