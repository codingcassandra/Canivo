// Package api is Canivo's HTTP interface: register dogs, ingest sensor
// samples, read live status and alert history, and register push tokens.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/codingcassandra/Canivo/internal/breath"
	"github.com/codingcassandra/Canivo/internal/monitor"
	"github.com/codingcassandra/Canivo/internal/notify"
	"github.com/codingcassandra/Canivo/internal/store"
)

const maxSamplesPerRequest = 5000

type Server struct {
	store        *store.Store
	notifier     notify.Notifier
	cfg          monitor.Config
	pushCooldown time.Duration
	now          func() time.Time

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
}

func New(st *store.Store, n notify.Notifier, cfg monitor.Config, pushCooldown time.Duration) *Server {
	return &Server{
		store:        st,
		notifier:     n,
		cfg:          cfg,
		pushCooldown: pushCooldown,
		now:          time.Now,
		dogs:         map[int64]*dogRuntime{},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/dogs", s.createDog)
	mux.HandleFunc("GET /api/dogs", s.listDogs)
	mux.HandleFunc("GET /api/dogs/{id}", s.getDog)
	mux.HandleFunc("POST /api/dogs/{id}/samples", s.ingest)
	mux.HandleFunc("GET /api/dogs/{id}/status", s.status)
	mux.HandleFunc("POST /api/dogs/{id}/calibrate", s.calibrate)
	mux.HandleFunc("GET /api/dogs/{id}/alerts", s.alerts)
	mux.HandleFunc("POST /api/dogs/{id}/push-tokens", s.addPushToken)
	mux.HandleFunc("DELETE /api/push-tokens/{token}", s.removePushToken)
	return mux
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
		if now.Sub(rt.lastPush) >= s.pushCooldown {
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

// push runs in the background so a slow push service never delays ingest.
func (s *Server) push(dogID, alertID int64, name string, ev monitor.Event, b monitor.Baseline) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tokens, err := s.store.PushTokens(ctx, dogID)
	if err != nil {
		log.Printf("dog %d: load push tokens: %v", dogID, err)
		return
	}
	msg := notify.Message{
		Title: name + " is on high alert",
		Body:  alertBody(name, ev, b),
		Data:  map[string]any{"type": "alert", "dog_id": dogID, "alert_id": alertID},
	}
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
