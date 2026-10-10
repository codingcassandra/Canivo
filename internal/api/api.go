// Package api is Canivo's HTTP interface: register dogs, ingest sensor
// samples and GPS fixes, read live status, location and alert history, set
// the home safe zone, and register push tokens.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codingcassandra/Canivo/internal/breath"
	"github.com/codingcassandra/Canivo/internal/geo"
	"github.com/codingcassandra/Canivo/internal/monitor"
	"github.com/codingcassandra/Canivo/internal/notify"
	"github.com/codingcassandra/Canivo/internal/store"
)

const (
	maxSamplesPerRequest = 5000
	maxFixesPerRequest   = 500
)

type Server struct {
	store    *store.Store
	notifier notify.Notifier
	cfg      monitor.Config
	opts     Options
	now      func() time.Time

	mu   sync.Mutex
	dogs map[int64]*dogRuntime
}

// dogRuntime is the in-memory live state for one dog.
type dogRuntime struct {
	mu        sync.Mutex
	name      string
	mon       *monitor.Monitor
	openAlert int64 // 0 when no alert is open
	lastPush  time.Time
	lastSeen  time.Time

	zone    *geo.Zone // nil = no safe zone set
	fence   geo.Fence
	lastFix *geo.Fix
}

type Options struct {
	PushCooldown time.Duration // min time between pushes per dog
	// APIKey, if set, must be sent by every /api request as
	// "Authorization: Bearer <APIKey>".
	APIKey string
	// WebPush and VAPIDPublicKey enable browser push. Nil disables it.
	WebPush        notify.WebPusher
	VAPIDPublicKey string
	// AllowedOrigins are web app origins (e.g. "https://canivo.vercel.app")
	// allowed to call the API from a browser. "*" allows any.
	AllowedOrigins []string
}

func New(st *store.Store, n notify.Notifier, cfg monitor.Config, opts Options) *Server {
	return &Server{
		store:    st,
		notifier: n,
		cfg:      cfg,
		opts:     opts,
		now:      time.Now,
		dogs:     map[int64]*dogRuntime{},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"service": "canivo API",
			"status":  "ok",
			"note":    "This is the server. Open the Canivo web app to see your dog.",
		})
	})
	mux.HandleFunc("POST /api/dogs", s.createDog)
	mux.HandleFunc("GET /api/dogs", s.listDogs)
	mux.HandleFunc("GET /api/dogs/{id}", s.getDog)
	mux.HandleFunc("POST /api/dogs/{id}/samples", s.ingest)
	mux.HandleFunc("GET /api/dogs/{id}/status", s.status)
	mux.HandleFunc("POST /api/dogs/{id}/calibrate", s.calibrate)
	mux.HandleFunc("GET /api/dogs/{id}/alerts", s.alerts)
	mux.HandleFunc("POST /api/dogs/{id}/locations", s.ingestFixes)
	mux.HandleFunc("GET /api/dogs/{id}/location", s.location)
	mux.HandleFunc("PUT /api/dogs/{id}/zone", s.setZone)
	mux.HandleFunc("DELETE /api/dogs/{id}/zone", s.deleteZone)
	mux.HandleFunc("POST /api/dogs/{id}/push-tokens", s.addPushToken)
	mux.HandleFunc("DELETE /api/push-tokens/{token}", s.removePushToken)
	mux.HandleFunc("GET /api/web-push/key", s.webPushKey)
	mux.HandleFunc("POST /api/dogs/{id}/web-push", s.addWebPush)
	mux.HandleFunc("POST /api/web-push/unsubscribe", s.removeWebPush)
	return s.cors(s.requireKey(mux))
}

// cors lets the web app, hosted on another domain, call the API. Preflight
// requests are answered here, before the API key check (browsers send them
// without credentials).
func (s *Server) cors(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range s.opts.AllowedOrigins {
		allowed[strings.TrimRight(o, "/")] = true
	}
	if len(allowed) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowed["*"] || allowed[origin]) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				h.Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireKey(next http.Handler) http.Handler {
	if s.opts.APIKey == "" {
		return next
	}
	want := []byte("Bearer " + s.opts.APIKey)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") &&
			subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or wrong API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) createDog(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	d, err := s.store.CreateDog(r.Context(), req.Name)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) listDogs(w http.ResponseWriter, r *http.Request) {
	dogs, err := s.store.Dogs(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dogs)
}

func (s *Server) getDog(w http.ResponseWriter, r *http.Request) {
	id, ok := dogID(w, r)
	if !ok {
		return
	}
	d, err := s.store.Dog(r.Context(), id)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

type statusResponse struct {
	DogID int64 `json:"dog_id"`
	monitor.Status
	AlertID  int64      `json:"alert_id,omitempty"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
}

func (rt *dogRuntime) statusLocked(id int64) statusResponse {
	resp := statusResponse{DogID: id, Status: rt.mon.Status(), AlertID: rt.openAlert}
	if !rt.lastSeen.IsZero() {
		t := rt.lastSeen
		resp.LastSeen = &t
	}
	return resp
}

// ingest receives a batch of samples from the bridge, e.g.
// {"samples":[{"t":123400,"v":212},{"t":123420,"v":215}]}
func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	id, ok := dogID(w, r)
	if !ok {
		return
	}
	var req struct {
		Samples []breath.Sample `json:"samples"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Samples) > maxSamplesPerRequest {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("at most %d samples per request", maxSamplesPerRequest))
		return
	}
	rt, err := s.runtime(r.Context(), id)
	if err != nil {
		storeError(w, err)
		return
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.lastSeen = s.now().UTC()
	_, events := rt.mon.Ingest(req.Samples)
	for _, ev := range events {
		if err := s.handleEvent(r.Context(), id, rt, ev); err != nil {
			serverError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, rt.statusLocked(id))
}

func (s *Server) handleEvent(ctx context.Context, dogID int64, rt *dogRuntime, ev monitor.Event) error {
	now := s.now().UTC()
	switch ev.Kind {
	case monitor.Calibrated:
		log.Printf("dog %d: calibrated %+v", dogID, *rt.mon.Baseline())
		return s.store.SetBaseline(ctx, dogID, rt.mon.Baseline())

	case monitor.AlertStarted:
		alertID, err := s.store.StartAlert(ctx, store.Alert{
			DogID: dogID, StartedAt: now, Score: ev.Score, RateBPM: ev.RateBPM, Tension: ev.Tension,
		})
		if err != nil {
			return err
		}
		rt.openAlert = alertID
		log.Printf("dog %d: ALERT %d score=%.1f rate=%.0f", dogID, alertID, ev.Score, ev.RateBPM)
		if now.Sub(rt.lastPush) >= s.opts.PushCooldown {
			rt.lastPush = now
			go s.push(dogID, alertID, rt.name, ev, *rt.mon.Baseline())
		}

	case monitor.AlertCleared:
		log.Printf("dog %d: alert %d cleared", dogID, rt.openAlert)
		if rt.openAlert != 0 {
			if err := s.store.EndAlert(ctx, rt.openAlert, now); err != nil {
				return err
			}
			rt.openAlert = 0
		}
	}
	return nil
}

func (s *Server) push(dogID, alertID int64, name string, ev monitor.Event, b monitor.Baseline) {
	s.send(dogID, notify.Message{
		Title: name + " is on high alert",
		Body:  alertBody(name, ev, b),
		Data:  map[string]any{"type": "alert", "dog_id": dogID, "alert_id": alertID},
	})
}

// send delivers a message to every phone and browser registered for the dog.
// Call it in a goroutine so a slow push service never delays a request.
func (s *Server) send(dogID int64, msg notify.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if tokens, err := s.store.PushTokens(ctx, dogID); err != nil {
		log.Printf("dog %d: load push tokens: %v", dogID, err)
	} else if len(tokens) > 0 {
		invalid, err := s.notifier.Notify(ctx, tokens, msg)
		if err != nil {
			log.Printf("dog %d: push failed: %v", dogID, err)
		}
		for _, t := range invalid {
			if err := s.store.RemovePushToken(ctx, t); err != nil {
				log.Printf("remove push token: %v", err)
			}
		}
	}

	if s.opts.WebPush == nil {
		return
	}
	if subs, err := s.store.WebPushSubs(ctx, dogID); err != nil {
		log.Printf("dog %d: load web push subs: %v", dogID, err)
	} else if len(subs) > 0 {
		gone, err := s.opts.WebPush.NotifyWeb(ctx, subs, msg)
		if err != nil {
			log.Printf("dog %d: web push failed: %v", dogID, err)
		}
		for _, e := range gone {
			if err := s.store.RemoveWebPush(ctx, e); err != nil {
				log.Printf("remove web push sub: %v", err)
			}
		}
	}
}

// alertBody explains what tripped the alert relative to the dog's normal,
// e.g. "Breathing 34 bpm (usually 18), muscle tension up 40%. Time to redirect Rex."
func alertBody(name string, ev monitor.Event, b monitor.Baseline) string {
	body := fmt.Sprintf("Breathing %.0f bpm (usually %.0f)", ev.RateBPM, b.RateMean)
	if b.TensionMean > 0 {
		if pct := (ev.Tension/b.TensionMean - 1) * 100; pct >= 10 {
			body += fmt.Sprintf(", muscle tension up %.0f%%", pct)
		}
	}
	return body + ". Time to redirect " + name + "."
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	id, ok := dogID(w, r)
	if !ok {
		return
	}
	rt, err := s.runtime(r.Context(), id)
	if err != nil {
		storeError(w, err)
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	writeJSON(w, http.StatusOK, rt.statusLocked(id))
}

// calibrate throws away the baseline. Call it while the dog is calm; the
// next couple of minutes of samples become the new baseline.
func (s *Server) calibrate(w http.ResponseWriter, r *http.Request) {
	id, ok := dogID(w, r)
	if !ok {
		return
	}
	rt, err := s.runtime(r.Context(), id)
	if err != nil {
		storeError(w, err)
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.openAlert != 0 {
		if err := s.store.EndAlert(r.Context(), rt.openAlert, s.now()); err != nil {
			serverError(w, err)
			return
		}
		rt.openAlert = 0
	}
	if err := s.store.SetBaseline(r.Context(), id, nil); err != nil {
		serverError(w, err)
		return
	}
	rt.mon.Recalibrate()
	writeJSON(w, http.StatusOK, rt.statusLocked(id))
}

func (s *Server) alerts(w http.ResponseWriter, r *http.Request) {
	id, ok := dogID(w, r)
	if !ok {
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			writeError(w, http.StatusBadRequest, "limit must be 1-500")
			return
		}
		limit = n
	}
	if _, err := s.store.Dog(r.Context(), id); err != nil {
		storeError(w, err)
		return
	}
	alerts, err := s.store.Alerts(r.Context(), id, limit)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, alerts)
}

func (s *Server) addPushToken(w http.ResponseWriter, r *http.Request) {
	id, ok := dogID(w, r)
	if !ok {
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	if _, err := s.store.Dog(r.Context(), id); err != nil {
		storeError(w, err)
		return
	}
	if err := s.store.AddPushToken(r.Context(), id, req.Token); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removePushToken(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RemovePushToken(r.Context(), r.PathValue("token")); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) webPushKey(w http.ResponseWriter, r *http.Request) {
	if s.opts.WebPush == nil {
		writeError(w, http.StatusNotFound, "web push is not configured on this server")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"public_key": s.opts.VAPIDPublicKey})
}

func (s *Server) addWebPush(w http.ResponseWriter, r *http.Request) {
	id, ok := dogID(w, r)
	if !ok {
		return
	}
	var sub notify.WebSubscription
	if !decode(w, r, &sub) {
		return
	}
	if !strings.HasPrefix(sub.Endpoint, "https://") || sub.Keys.P256dh == "" || sub.Keys.Auth == "" {
		writeError(w, http.StatusBadRequest, "invalid push subscription")
		return
	}
	if _, err := s.store.Dog(r.Context(), id); err != nil {
		storeError(w, err)
		return
	}
	if err := s.store.AddWebPush(r.Context(), id, sub); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeWebPush(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.store.RemoveWebPush(r.Context(), req.Endpoint); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fixIn is a GPS fix as the collar sends it. t is Unix time in milliseconds
// (0 or missing = now); accuracy_m is the GPS's horizontal accuracy.
type fixIn struct {
	T         int64    `json:"t"`
	Lat       *float64 `json:"lat"`
	Lon       *float64 `json:"lon"`
	AccuracyM float64  `json:"accuracy_m"`
}

type locationResponse struct {
	DogID     int64      `json:"dog_id"`
	Zone      *geo.Zone  `json:"zone"`
	Last      *geo.Fix   `json:"last,omitempty"`
	DistanceM *float64   `json:"distance_m,omitempty"` // from home, if a zone is set
	Away      bool       `json:"away"`
	AwaySince *time.Time `json:"away_since,omitempty"`
	Trail     []geo.Fix  `json:"trail,omitempty"` // oldest first
}

func (rt *dogRuntime) locationLocked(id int64) locationResponse {
	resp := locationResponse{DogID: id, Zone: rt.zone, Last: rt.lastFix}
	if rt.zone != nil && rt.lastFix != nil {
		d := math.Round(rt.zone.DistanceFrom(*rt.lastFix))
		resp.DistanceM = &d
	}
	if away, since := rt.fence.Away(); away {
		resp.Away = true
		resp.AwaySince = &since
	}
	return resp
}

// ingestFixes receives GPS fixes from the collar, e.g.
// {"fixes":[{"t":1760000000000,"lat":40.7128,"lon":-74.0060,"accuracy_m":8}]}
// and alerts the owner when the dog leaves the safe zone or comes back.
func (s *Server) ingestFixes(w http.ResponseWriter, r *http.Request) {
	id, ok := dogID(w, r)
	if !ok {
		return
	}
	var req struct {
		Fixes []fixIn `json:"fixes"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Fixes) == 0 || len(req.Fixes) > maxFixesPerRequest {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("send 1-%d fixes", maxFixesPerRequest))
		return
	}
	now := s.now().UTC()
	fixes := make([]geo.Fix, 0, len(req.Fixes))
	for _, in := range req.Fixes {
		if in.Lat == nil || in.Lon == nil || !validLatLon(*in.Lat, *in.Lon) {
			writeError(w, http.StatusBadRequest, "each fix needs a valid lat and lon")
			return
		}
		if in.AccuracyM < 0 {
			writeError(w, http.StatusBadRequest, "accuracy_m must not be negative")
			return
		}
		at := now
		if in.T != 0 {
			at = time.UnixMilli(in.T).UTC()
			if at.After(now.Add(time.Minute)) {
				writeError(w, http.StatusBadRequest, "fix time is in the future (t is Unix milliseconds)")
				return
			}
		}
		fixes = append(fixes, geo.Fix{At: at, Lat: *in.Lat, Lon: *in.Lon, AccuracyM: in.AccuracyM})
	}
	sort.SliceStable(fixes, func(i, j int) bool { return fixes[i].At.Before(fixes[j].At) })

	rt, err := s.runtime(r.Context(), id)
	if err != nil {
		storeError(w, err)
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if err := s.store.AddFixes(r.Context(), id, fixes); err != nil {
		serverError(w, err)
		return
	}
	for _, f := range fixes {
		if rt.lastFix != nil && !f.At.After(rt.lastFix.At) {
			continue // late or duplicate fix: kept in history, but don't replay it
		}
		f := f
		rt.lastFix = &f
		if rt.zone == nil {
			continue
		}
		switch rt.fence.Update(*rt.zone, f) {
		case geo.Left:
			log.Printf("dog %d: LEFT safe zone, %.0f m from home", id, rt.zone.DistanceFrom(f))
			go s.send(id, notify.Message{
				Title: rt.name + " left home",
				Body:  rt.name + " is outside the safe zone. Tap to see where " + rt.name + " is.",
				Data:  map[string]any{"type": "away", "dog_id": id},
			})
		case geo.Returned:
			log.Printf("dog %d: back in safe zone", id)
			go s.send(id, notify.Message{
				Title: rt.name + " is back home",
				Body:  rt.name + " is inside the safe zone again.",
				Data:  map[string]any{"type": "home", "dog_id": id},
			})
		}
	}
	writeJSON(w, http.StatusOK, rt.locationLocked(id))
}

// location returns where the dog is, the safe zone, and the recent trail
// (?minutes=, default 30, max 1440).
func (s *Server) location(w http.ResponseWriter, r *http.Request) {
	id, ok := dogID(w, r)
	if !ok {
		return
	}
	minutes := 30
	if v := r.URL.Query().Get("minutes"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1440 {
			writeError(w, http.StatusBadRequest, "minutes must be 1-1440")
			return
		}
		minutes = n
	}
	rt, err := s.runtime(r.Context(), id)
	if err != nil {
		storeError(w, err)
		return
	}
	trail, err := s.store.Fixes(r.Context(), id, s.now().Add(-time.Duration(minutes)*time.Minute), maxFixesPerRequest)
	if err != nil {
		serverError(w, err)
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	resp := rt.locationLocked(id)
	resp.Trail = trail
	writeJSON(w, http.StatusOK, resp)
}

// setZone sets the safe zone, e.g. {"lat":40.7128,"lon":-74.0060,"radius_m":100}.
func (s *Server) setZone(w http.ResponseWriter, r *http.Request) {
	id, ok := dogID(w, r)
	if !ok {
		return
	}
	var z geo.Zone
	if !decode(w, r, &z) {
		return
	}
	if !validLatLon(z.Lat, z.Lon) {
		writeError(w, http.StatusBadRequest, "invalid lat/lon")
		return
	}
	if z.RadiusM < 20 || z.RadiusM > 5000 {
		writeError(w, http.StatusBadRequest, "radius_m must be 20-5000")
		return
	}
	s.updateZone(w, r, id, &z)
}

func (s *Server) deleteZone(w http.ResponseWriter, r *http.Request) {
	id, ok := dogID(w, r)
	if !ok {
		return
	}
	s.updateZone(w, r, id, nil)
}

func (s *Server) updateZone(w http.ResponseWriter, r *http.Request, id int64, z *geo.Zone) {
	rt, err := s.runtime(r.Context(), id)
	if err != nil {
		storeError(w, err)
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if err := s.store.SetZone(r.Context(), id, z); err != nil {
		serverError(w, err)
		return
	}
	rt.zone = z
	rt.fence.Reset()
	writeJSON(w, http.StatusOK, rt.locationLocked(id))
}

func validLatLon(lat, lon float64) bool {
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 && !(lat == 0 && lon == 0)
}

// runtime returns the live state for a dog, loading it on first use.
func (s *Server) runtime(ctx context.Context, id int64) (*dogRuntime, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rt, ok := s.dogs[id]; ok {
		return rt, nil
	}
	d, err := s.store.Dog(ctx, id)
	if err != nil {
		return nil, err
	}
	rt := &dogRuntime{name: d.Name, mon: monitor.New(s.cfg, d.Baseline)}
	if rt.zone, err = s.store.Zone(ctx, id); err != nil {
		return nil, err
	}
	last, err := s.store.Fixes(ctx, id, time.Time{}, 1)
	if err != nil {
		return nil, err
	}
	if len(last) == 1 {
		rt.lastFix = &last[0]
	}
	s.dogs[id] = rt
	return rt, nil
}

func dogID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid dog id")
		return 0, false
	}
	return id, true
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func storeError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "dog not found")
		return
	}
	serverError(w, err)
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("error: %v", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
