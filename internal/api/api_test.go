package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codingcassandra/Canivo/internal/breath"
	"github.com/codingcassandra/Canivo/internal/monitor"
	"github.com/codingcassandra/Canivo/internal/notify"
	"github.com/codingcassandra/Canivo/internal/store"
)

type sent struct {
	tokens []string
	msg    notify.Message
}

type fakeNotifier chan sent

func (f fakeNotifier) Notify(_ context.Context, tokens []string, msg notify.Message) ([]string, error) {
	f <- sent{tokens, msg}
	return nil, nil
}

func setup(t *testing.T) (http.Handler, fakeNotifier) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := monitor.DefaultConfig()
	cfg.Calibration = 30 * time.Second
	n := make(fakeNotifier, 10)
	return New(st, n, cfg, 2*time.Minute).Handler(), n
}

func do(t *testing.T, h http.Handler, method, path, body string, out any) int {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, path, rec.Body.String(), err)
		}
	}
	return rec.Code
}

// stream posts durMs of synthetic signal in one-second batches, like the bridge.
func stream(t *testing.T, h http.Handler, path string, s *breath.Synth, now *int64, durMs int64) statusResponse {
	t.Helper()
	var st statusResponse
	for end := *now + durMs; *now < end; {
		var batch []breath.Sample
		for i := 0; i < 50; i++ {
			batch = append(batch, s.Next(*now, 20))
			*now += 20
		}
		body, _ := json.Marshal(map[string]any{"samples": batch})
		if code := do(t, h, "POST", path, string(body), &st); code != 200 {
			t.Fatalf("ingest status %d", code)
		}
	}
	return st
}

func TestFullFlow(t *testing.T) {
	h, pushes := setup(t)

	var dog store.Dog
	if code := do(t, h, "POST", "/api/dogs", `{"name":"Rex"}`, &dog); code != 201 || dog.ID == 0 {
		t.Fatalf("create dog: %d %+v", code, dog)
	}
	if code := do(t, h, "POST", "/api/dogs/1/push-tokens", `{"token":"ExponentPushToken[abc]"}`, nil); code != 204 {
		t.Fatalf("add token: %d", code)
	}

	s := breath.NewSynth(18, 200)
	var now int64
	st := stream(t, h, "/api/dogs/1/samples", s, &now, 50000)
	if st.State != monitor.Calm || st.Baseline == nil {
		t.Fatalf("after calibration: %+v", st)
	}
	do(t, h, "GET", "/api/dogs/1", "", &dog)
	if dog.Baseline == nil {
		t.Fatal("baseline not persisted")
	}

	s.RateBPM, s.Tension = 40, 320
	st = stream(t, h, "/api/dogs/1/samples", s, &now, 30000)
	if st.State != monitor.Alert || st.AlertID == 0 {
		t.Fatalf("expected alert: %+v", st)
	}

	select {
	case p := <-pushes:
		if len(p.tokens) != 1 || p.tokens[0] != "ExponentPushToken[abc]" || !strings.Contains(p.msg.Title, "Rex") ||
			!strings.Contains(p.msg.Body, "usually 18") || !strings.Contains(p.msg.Body, "tension up") {
			t.Errorf("push = %+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no push sent")
	}

	s.RateBPM, s.Tension = 18, 200
	st = stream(t, h, "/api/dogs/1/samples", s, &now, 60000)
	if st.State != monitor.Calm {
		t.Fatalf("expected calm: %+v", st)
	}

	var alerts []store.Alert
	do(t, h, "GET", "/api/dogs/1/alerts", "", &alerts)
	if len(alerts) != 1 || alerts[0].EndedAt == nil || alerts[0].Tension < 280 {
		t.Fatalf("alerts = %+v", alerts)
	}

	// A second alert inside the cooldown is recorded but not pushed again.
	s.RateBPM, s.Tension = 40, 320
	stream(t, h, "/api/dogs/1/samples", s, &now, 30000)
	select {
	case p := <-pushes:
		t.Fatalf("unexpected second push %+v", p)
	case <-time.After(100 * time.Millisecond):
	}

	var cal statusResponse
	do(t, h, "POST", "/api/dogs/1/calibrate", "", &cal)
	if cal.State != monitor.Calibrating || cal.AlertID != 0 {
		t.Fatalf("recalibrate: %+v", cal)
	}
}

func TestErrors(t *testing.T) {
	h, _ := setup(t)
	var e map[string]string
	cases := []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/api/dogs/99", "", 404},
		{"GET", "/api/dogs/abc", "", 400},
		{"POST", "/api/dogs", `{}`, 400},
		{"POST", "/api/dogs", `not json`, 400},
		{"POST", "/api/dogs/99/samples", `{"samples":[]}`, 404},
		{"GET", "/api/dogs/99/alerts", "", 404},
	}
	for _, c := range cases {
		if code := do(t, h, c.method, c.path, c.body, &e); code != c.want || e["error"] == "" {
			t.Errorf("%s %s = %d %v, want %d", c.method, c.path, code, e, c.want)
		}
	}

	big, _ := json.Marshal(map[string]any{"samples": make([]breath.Sample, maxSamplesPerRequest+1)})
	do(t, h, "POST", "/api/dogs", `{"name":"Rex"}`, nil)
	req := httptest.NewRequest("POST", "/api/dogs/1/samples", bytes.NewReader(big))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized batch = %d", rec.Code)
	}
}
