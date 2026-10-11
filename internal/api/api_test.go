package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	return New(st, n, cfg, Options{PushCooldown: 2 * time.Minute}).Handler(), n
}

func TestAPIKey(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	h := New(st, notify.Log{}, monitor.DefaultConfig(), Options{APIKey: "s3cret"}).Handler()

	for _, c := range []struct {
		path, auth string
		want       int
	}{
		{"/health", "", 200},
		{"/api/dogs", "", 401},
		{"/api/dogs", "Bearer wrong", 401},
		{"/api/dogs", "s3cret", 401},
		{"/api/dogs", "Bearer s3cret", 200},
	} {
		req := httptest.NewRequest("GET", c.path, nil)
		if c.auth != "" {
			req.Header.Set("Authorization", c.auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s auth=%q: %d, want %d", c.path, c.auth, rec.Code, c.want)
		}
	}
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

type fakeWebPusher chan []notify.WebSubscription

func (f fakeWebPusher) NotifyWeb(_ context.Context, subs []notify.WebSubscription, _ notify.Message) ([]string, error) {
	f <- subs
	return []string{subs[0].Endpoint}, nil // pretend it expired
}

func TestWebPush(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	web := make(fakeWebPusher, 1)
	h := New(st, make(fakeNotifier, 1), monitor.DefaultConfig(), Options{
		PushCooldown: time.Minute, WebPush: web, VAPIDPublicKey: "PUBKEY",
	}).Handler()

	var key map[string]string
	if do(t, h, "GET", "/api/web-push/key", "", &key); key["public_key"] != "PUBKEY" {
		t.Fatalf("key = %v", key)
	}

	dog, _ := st.CreateDog(context.Background(), "Rex", 0)
	st.SetBaseline(context.Background(), dog.ID, &monitor.Baseline{RateMean: 18, RateSD: 2, TensionMean: 232, TensionSD: 12})
	if code := do(t, h, "POST", "/api/dogs/1/web-push", `{"endpoint":"http://insecure","keys":{"p256dh":"p","auth":"a"}}`, nil); code != 400 {
		t.Errorf("insecure endpoint accepted: %d", code)
	}
	sub := `{"endpoint":"https://web.push.apple.com/x","expirationTime":null,"keys":{"p256dh":"p","auth":"a"}}`
	if code := do(t, h, "POST", "/api/dogs/1/web-push", sub, nil); code != 204 {
		t.Fatalf("subscribe = %d", code)
	}

	s := breath.NewSynth(18, 200)
	var now int64
	stream(t, h, "/api/dogs/1/samples", s, &now, 30000)
	s.RateBPM, s.Tension = 40, 320
	stream(t, h, "/api/dogs/1/samples", s, &now, 30000)

	select {
	case subs := <-web:
		if len(subs) != 1 || subs[0].Endpoint != "https://web.push.apple.com/x" || subs[0].Keys.Auth != "a" {
			t.Errorf("web push subs = %+v", subs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no web push sent")
	}
	// The pusher reported it gone, so it should be deleted.
	time.Sleep(50 * time.Millisecond)
	if subs, _ := st.WebPushSubs(context.Background(), 1); len(subs) != 0 {
		t.Errorf("expired sub not removed: %+v", subs)
	}
}

func TestWebPushDisabled(t *testing.T) {
	h, _ := setup(t)
	if code := do(t, h, "GET", "/api/web-push/key", "", nil); code != 404 {
		t.Errorf("key without VAPID = %d", code)
	}
}

func TestCORS(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	h := New(st, notify.Log{}, monitor.DefaultConfig(), Options{
		APIKey: "k", AllowedOrigins: []string{"https://canivo.vercel.app/"},
	}).Handler()

	// Preflight: no API key, must still succeed.
	req := httptest.NewRequest("OPTIONS", "/api/dogs", nil)
	req.Header.Set("Origin", "https://canivo.vercel.app")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != "https://canivo.vercel.app" ||
		!strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Errorf("preflight = %d %v", rec.Code, rec.Header())
	}

	// Real request from an allowed origin gets the header; others don't.
	for origin, want := range map[string]string{"https://canivo.vercel.app": "https://canivo.vercel.app", "https://evil.example": ""} {
		req = httptest.NewRequest("GET", "/api/dogs", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Authorization", "Bearer k")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 || rec.Header().Get("Access-Control-Allow-Origin") != want {
			t.Errorf("%s: %d allow=%q", origin, rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
		}
	}
}

func TestSafeZone(t *testing.T) {
	h, n := setup(t)
	do(t, h, "POST", "/api/dogs", `{"name":"Rex"}`, nil)
	do(t, h, "POST", "/api/dogs/1/push-tokens", `{"token":"ExponentPushToken[a]"}`, nil)

	// Fixes before a zone exists are stored but never alert.
	at := time.Now().Add(-10 * time.Minute).UnixMilli()
	fix := func(northM, acc float64) string {
		at += 10000
		return fmt.Sprintf(`{"fixes":[{"t":%d,"lat":%f,"lon":-74,"accuracy_m":%f}]}`, at, 40.7+northM/111195, acc)
	}
	var loc locationResponse
	if code := do(t, h, "POST", "/api/dogs/1/locations", fix(5000, 5), &loc); code != 200 || loc.Zone != nil || loc.Last == nil {
		t.Fatalf("no-zone fix: %d %+v", code, loc)
	}

	for _, c := range []struct{ body string }{
		{`{"lat":40.7,"lon":-74,"radius_m":5}`},
		{`{"lat":91,"lon":-74,"radius_m":100}`},
		{`{"lat":0,"lon":0,"radius_m":100}`},
	} {
		if code := do(t, h, "PUT", "/api/dogs/1/zone", c.body, nil); code != 400 {
			t.Errorf("zone %s: %d", c.body, code)
		}
	}
	if code := do(t, h, "PUT", "/api/dogs/1/zone", `{"lat":40.7,"lon":-74,"radius_m":100}`, &loc); code != 200 || loc.Zone == nil || loc.Zone.RadiusM != 100 {
		t.Fatalf("set zone: %d %+v", code, loc)
	}

	// Playing in the yard, then one stray fix outside: nothing.
	for _, m := range []float64{10, 60, 400, 30} {
		do(t, h, "POST", "/api/dogs/1/locations", fix(m, 10), &loc)
		if loc.Away {
			t.Fatalf("away after %v m", m)
		}
	}
	// Runs off.
	do(t, h, "POST", "/api/dogs/1/locations", fix(250, 10), nil)
	do(t, h, "POST", "/api/dogs/1/locations", fix(400, 10), &loc)
	if !loc.Away || loc.AwaySince == nil || loc.DistanceM == nil || *loc.DistanceM < 390 || *loc.DistanceM > 410 {
		t.Fatalf("after running off: %+v", loc)
	}
	select {
	case s := <-n:
		if s.msg.Title != "Rex left home" || s.msg.Data["type"] != "away" {
			t.Errorf("away push = %+v", s.msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no away push")
	}

	// A late fix from earlier (inside the zone) doesn't count as coming home.
	late := fmt.Sprintf(`{"fixes":[{"t":%d,"lat":40.7,"lon":-74,"accuracy_m":5},{"t":%d,"lat":40.7,"lon":-74,"accuracy_m":5}]}`, at-60000, at-50000)
	do(t, h, "POST", "/api/dogs/1/locations", late, &loc)
	if !loc.Away {
		t.Fatal("late fixes brought the dog home")
	}

	// Comes back: a batch of two fixes inside.
	at += 10000
	back := fmt.Sprintf(`{"fixes":[{"t":%d,"lat":40.7003,"lon":-74,"accuracy_m":8},{"lat":40.7001,"lon":-74,"accuracy_m":8}]}`, at)
	do(t, h, "POST", "/api/dogs/1/locations", back, &loc)
	if loc.Away {
		t.Fatalf("still away: %+v", loc)
	}
	select {
	case s := <-n:
		if s.msg.Title != "Rex is back home" {
			t.Errorf("home push = %+v", s.msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no back-home push")
	}

	// Trail: all fixes from the last 30 minutes, oldest first.
	do(t, h, "GET", "/api/dogs/1/location", "", &loc)
	if len(loc.Trail) != 11 || loc.Trail[0].At.After(loc.Trail[1].At) || loc.Last == nil {
		t.Fatalf("trail = %d fixes, last %+v", len(loc.Trail), loc.Last)
	}
	if code := do(t, h, "GET", "/api/dogs/1/location?minutes=0", "", nil); code != 400 {
		t.Errorf("minutes=0: %d", code)
	}

	// Removing the zone.
	if do(t, h, "DELETE", "/api/dogs/1/zone", "", &loc); loc.Zone != nil || loc.Away {
		t.Fatalf("after delete: %+v", loc)
	}
}

func TestLocationErrors(t *testing.T) {
	h, _ := setup(t)
	do(t, h, "POST", "/api/dogs", `{"name":"Rex"}`, nil)
	future := time.Now().Add(time.Hour).UnixMilli()
	for _, c := range []struct {
		path, body string
		want       int
	}{
		{"/api/dogs/1/locations", `{"fixes":[]}`, 400},
		{"/api/dogs/1/locations", `{"fixes":[{"lon":-74}]}`, 400},
		{"/api/dogs/1/locations", `{"fixes":[{"lat":40.7,"lon":-200}]}`, 400},
		{"/api/dogs/1/locations", `{"fixes":[{"lat":40.7,"lon":-74,"accuracy_m":-1}]}`, 400},
		{"/api/dogs/1/locations", fmt.Sprintf(`{"fixes":[{"t":%d,"lat":40.7,"lon":-74}]}`, future), 400},
		{"/api/dogs/9/locations", `{"fixes":[{"lat":40.7,"lon":-74}]}`, 404},
		{"/api/dogs/1/locations", `{"fixes":[{"lat":40.7,"lon":-74}]}`, 200},
	} {
		if code := do(t, h, "POST", c.path, c.body, nil); code != c.want {
			t.Errorf("%s %s: %d, want %d", c.path, c.body, code, c.want)
		}
	}
	if code := do(t, h, "GET", "/api/dogs/9/location", "", nil); code != 404 {
		t.Errorf("missing dog location: %d", code)
	}
}
