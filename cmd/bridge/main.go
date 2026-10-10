// Command bridge forwards EMG readings from the Arduino Nano (USB serial) to
// the Canivo server. The Nano has no WiFi/Bluetooth, so this runs on whatever
// it is plugged into: a laptop, a Raspberry Pi, etc.
//
//	go run ./cmd/bridge -dog 1 -port /dev/cu.usbserial-110
//	go run ./cmd/bridge -dog 1 -simulate        # no hardware needed
//
// Serial format (one line per sample, 115200 baud): "<millis>,<value>".
// Lines starting with '#' are logged and otherwise ignored.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/codingcassandra/Canivo/internal/breath"
	"go.bug.st/serial"
)

func main() {
	server := flag.String("server", "http://localhost:8090", "Canivo server URL")
	dog := flag.Int64("dog", 0, "dog id to send samples for (required)")
	port := flag.String("port", "", "serial port, e.g. /dev/cu.usbserial-110 (see -list)")
	baud := flag.Int("baud", 115200, "serial baud rate; must match the sketch")
	list := flag.Bool("list", false, "list serial ports and exit")
	simulate := flag.Bool("simulate", false, "generate fake breathing instead of reading serial")
	calm := flag.Duration("calm", 3*time.Minute, "simulate: calm time before the first alert episode")
	every := flag.Duration("every", 90*time.Second, "simulate: time between alert episodes")
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
	default:
		log.Fatal("pass -port (or -simulate); use -list to see ports")
	}

	forward(fmt.Sprintf("%s/api/dogs/%d/samples", strings.TrimRight(*server, "/"), *dog), samples)
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

// forward batches samples once a second and POSTs them, printing state changes.
func forward(url string, in <-chan breath.Sample) {
	client := &http.Client{Timeout: 10 * time.Second}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var batch []breath.Sample
	lastState := ""
	for {
		select {
		case s := <-in:
			batch = append(batch, s)
		case <-tick.C:
			if len(batch) == 0 {
				continue
			}
			status, err := post(client, url, batch)
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

func post(c *http.Client, url string, batch []breath.Sample) (status, error) {
	var st status
	body, err := json.Marshal(map[string]any{"samples": batch})
	if err != nil {
		return st, err
	}
	resp, err := c.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return st, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return st, fmt.Errorf("server: %s %s", resp.Status, bytes.TrimSpace(msg))
	}
	return st, json.NewDecoder(resp.Body).Decode(&st)
}
