package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/codingcassandra/Canivo/internal/advice"
	"github.com/codingcassandra/Canivo/internal/monitor"
	"github.com/codingcassandra/Canivo/internal/store"
)

type fakeAdvisor struct {
	tip     string
	err     error
	got     advice.Situation
	chatDog advice.Dog
	history []store.ChatMessage
}

func (f *fakeAdvisor) Suggest(_ context.Context, s advice.Situation) (string, error) {
	f.got = s
	return f.tip, f.err
}

func (f *fakeAdvisor) Chat(_ context.Context, d advice.Dog, history []store.ChatMessage) (string, error) {
	f.chatDog, f.history = d, history
	if f.err != nil {
		return "", f.err
	}
	return "Answer to: " + history[len(history)-1].Text, nil
}

func newServer(t *testing.T, a advice.Advisor) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	opts := Options{PushCooldown: time.Minute}
	if a != nil {
		opts.Advisor = a
	}
	return New(st, make(fakeNotifier, 10), monitor.DefaultConfig(), opts), st
}

func TestAlertTip(t *testing.T) {
	ctx := context.Background()
	ev := monitor.Event{RateBPM: 34, Tension: 280}
	b := monitor.Baseline{RateMean: 18, TensionMean: 200}

	a := &fakeAdvisor{tip: "Ask Rex to sit and reward him."}
	s, st := newServer(t, a)
	dog, _ := st.CreateDog(ctx, "Rex", 0)
	st.SetProfile(ctx, dog.ID, store.Profile{Breed: "border collie", Calming: "sniffing games"})
	old, _ := st.StartAlert(ctx, store.Alert{DogID: dog.ID, StartedAt: time.Now().Add(-time.Hour)})
	st.SetAlertTip(ctx, old, "Scatter a few treats for Rex to sniff out.")
	now, _ := st.StartAlert(ctx, store.Alert{DogID: dog.ID, StartedAt: time.Now()})

	body := alertBody("Rex", s.suggest(dog.ID, now, ev, b, false))
	if body != "Ask Rex to sit and reward him." {
		t.Errorf("body = %q", body)
	}
	g := a.got
	if g.Name != "Rex" || g.Profile.Calming != "sniffing games" || g.UsualRateBPM != 18 || math.Round(g.TensionUpPct) != 40 {
		t.Errorf("situation = %+v", g)
	}
	// The advisor sees past alerts (and their tips) but not the one in progress.
	if len(g.RecentAlerts) != 1 || g.RecentAlerts[0].ID != old || g.RecentAlerts[0].Tip == "" {
		t.Errorf("recent alerts = %+v", g.RecentAlerts)
	}
	alerts, _ := st.Alerts(ctx, dog.ID, 1)
	if alerts[0].Tip != "Ask Rex to sit and reward him." {
		t.Errorf("tip not saved on alert: %+v", alerts[0])
	}

	// A failing advisor (or none) falls back to the fixed wording.
	s, st = newServer(t, &fakeAdvisor{err: errors.New("quota")})
	dog, _ = st.CreateDog(ctx, "Rex", 0)
	if body := alertBody("Rex", s.suggest(dog.ID, 1, ev, b, false)); body != "Time to redirect Rex." {
		t.Errorf("fallback body = %q", body)
	}
	s, _ = newServer(t, nil)
	if tip := s.suggest(1, 1, ev, b, false); tip != "" {
		t.Errorf("no advisor tip = %q", tip)
	}

	// A tip that talks about breathing is dropped for the fixed wording.
	s, st = newServer(t, &fakeAdvisor{tip: "Rex is breathing fast, take them outside."})
	dog, _ = st.CreateDog(ctx, "Rex", 0)
	if body := alertBody("Rex", s.suggest(dog.ID, 1, ev, b, false)); body != "Time to redirect Rex." {
		t.Errorf("breathing tip body = %q", body)
	}

	// Danger is passed through to the advisor.
	da := &fakeAdvisor{tip: "Go to Rex now."}
	s, st = newServer(t, da)
	dog, _ = st.CreateDog(ctx, "Rex", 0)
	s.suggest(dog.ID, 1, ev, b, true)
	if !da.got.Danger {
		t.Error("danger not passed to advisor")
	}
}

func TestProfileAndChat(t *testing.T) {
	a := &fakeAdvisor{}
	s, _ := newServer(t, a)
	h := s.Handler()
	do(t, h, "POST", "/api/dogs", `{"name":"Rex"}`, nil)

	var dog store.Dog
	if code := do(t, h, "PUT", "/api/dogs/1/profile", `{"breed":" beagle ","diet":"chicken allergy"}`, &dog); code != 200 ||
		dog.Profile.Breed != "beagle" || dog.Profile.Diet != "chicken allergy" {
		t.Fatalf("set profile: %d %+v", code, dog)
	}
	if code := do(t, h, "PUT", "/api/dogs/1/profile", `{"notes":"`+strings.Repeat("x", 1001)+`"}`, nil); code != 400 {
		t.Errorf("long profile field: %d", code)
	}

	var res struct {
		Enabled  bool                `json:"enabled"`
		Messages []store.ChatMessage `json:"messages"`
	}
	if code := do(t, h, "POST", "/api/dogs/1/chat", `{"text":"What treats can Rex have?"}`, &res); code != 200 {
		t.Fatalf("chat: %d", code)
	}
	if len(res.Messages) != 2 || res.Messages[0].Role != "user" || res.Messages[1].Text != "Answer to: What treats can Rex have?" {
		t.Fatalf("chat reply = %+v", res.Messages)
	}
	if a.chatDog.Profile.Diet != "chicken allergy" {
		t.Errorf("assistant didn't get the profile: %+v", a.chatDog)
	}

	// The next question comes with the conversation so far.
	do(t, h, "POST", "/api/dogs/1/chat", `{"text":"And how many a day?"}`, nil)
	if len(a.history) != 3 || a.history[0].Text != "What treats can Rex have?" {
		t.Errorf("history = %+v", a.history)
	}
	do(t, h, "GET", "/api/dogs/1/chat", "", &res)
	if !res.Enabled || len(res.Messages) != 4 {
		t.Errorf("history = %+v", res)
	}

	// A failed answer saves nothing.
	a.err = errors.New("down")
	if code := do(t, h, "POST", "/api/dogs/1/chat", `{"text":"Hello?"}`, nil); code != 502 {
		t.Errorf("failed chat: %d", code)
	}
	a.err = nil
	do(t, h, "GET", "/api/dogs/1/chat", "", &res)
	if len(res.Messages) != 4 {
		t.Errorf("after failure: %d messages", len(res.Messages))
	}

	for _, body := range []string{`{"text":"  "}`, `{"text":"` + strings.Repeat("x", 2001) + `"}`} {
		if code := do(t, h, "POST", "/api/dogs/1/chat", body, nil); code != 400 {
			t.Errorf("bad message: %d", code)
		}
	}
	if code := do(t, h, "POST", "/api/dogs/9/chat", `{"text":"hi"}`, nil); code != 404 {
		t.Errorf("missing dog: %d", code)
	}

	if code := do(t, h, "DELETE", "/api/dogs/1/chat", "", nil); code != 204 {
		t.Errorf("clear: %d", code)
	}
	do(t, h, "GET", "/api/dogs/1/chat", "", &res)
	if len(res.Messages) != 0 {
		t.Errorf("after clear: %+v", res.Messages)
	}
}

func TestChatLimits(t *testing.T) {
	s, _ := newServer(t, &fakeAdvisor{})
	h := s.Handler()
	do(t, h, "POST", "/api/dogs", `{"name":"Rex"}`, nil)
	now := time.Now()
	s.now = func() time.Time { return now }
	for i := 0; i < chatLimit; i++ {
		if code := do(t, h, "POST", "/api/dogs/1/chat", fmt.Sprintf(`{"text":"q%d"}`, i), nil); code != 200 {
			t.Fatalf("question %d: %d", i, code)
		}
	}
	if code := do(t, h, "POST", "/api/dogs/1/chat", `{"text":"one more"}`, nil); code != 429 {
		t.Errorf("over limit: %d", code)
	}
	now = now.Add(chatWindow)
	if code := do(t, h, "POST", "/api/dogs/1/chat", `{"text":"later"}`, nil); code != 200 {
		t.Errorf("after window: %d", code)
	}

	// Without a Gemini key the chat says so.
	s, _ = newServer(t, nil)
	h = s.Handler()
	do(t, h, "POST", "/api/dogs", `{"name":"Rex"}`, nil)
	if code := do(t, h, "POST", "/api/dogs/1/chat", `{"text":"hi"}`, nil); code != 503 {
		t.Errorf("no advisor: %d", code)
	}
}
