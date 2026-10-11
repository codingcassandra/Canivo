package advice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codingcassandra/Canivo/internal/store"
)

// fakeGemini answers every request with reply and records the last request.
func fakeGemini(t *testing.T, reply string) (*Gemini, *geminiRequest, *string) {
	t.Helper()
	var got geminiRequest
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if r.Header.Get("x-goog-api-key") != "k3y" {
			t.Errorf("api key = %q", r.Header.Get("x-goog-api-key"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return &Gemini{APIKey: "k3y", URL: srv.URL}, &got, &path
}

var rex = Dog{
	Name:         "Rex",
	Profile:      store.Profile{Breed: "beagle", Diet: "chicken allergy", Calming: "sniffing games"},
	UsualRateBPM: 18,
	RecentAlerts: []store.Alert{{StartedAt: time.Now().Add(-2 * time.Hour), Tip: "Scatter treats for Rex to sniff out."}},
}

func TestGeminiSuggest(t *testing.T) {
	g, got, path := fakeGemini(t, `{"candidates":[{"content":{"role":"model","parts":[
		{"text":"planning...","thought":true},
		{"text":"\"Ask Rex for a sit,\n"},{"text":"then reward with a treat.\"\n"}]}}]}`)
	tip, err := g.Suggest(context.Background(), Situation{Dog: rex, RateBPM: 34, TensionUpPct: 40})
	if err != nil {
		t.Fatal(err)
	}
	if tip != "Ask Rex for a sit, then reward with a treat." {
		t.Errorf("tip = %q", tip)
	}
	if *path != "/models/"+DefaultModel+":generateContent" {
		t.Errorf("path %q", *path)
	}
	prompt := got.Contents[0].Parts[0].Text
	for _, want := range []string{"Name: Rex", "Breed: beagle", "What helps them calm down: sniffing games",
		"34 breaths/min (usually 18)", "up 40%", "- Scatter treats", "last 7 days: 1"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestGeminiChat(t *testing.T) {
	g, got, _ := fakeGemini(t, `{"candidates":[{"content":{"parts":[{"text":"## Snacks\n**Good picks** for Rex:\n- carrots\n\n\n\n- apple slices"}]}}]}`)
	reply, err := g.Chat(context.Background(), rex, []store.ChatMessage{
		{Role: "assistant", Text: "left over from a cleared question"},
		{Role: "user", Text: "Hi"},
		{Role: "assistant", Text: "Hello!"},
		{Role: "user", Text: "What snacks can Rex have?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Snacks\nGood picks for Rex:\n- carrots\n\n- apple slices" {
		t.Errorf("reply = %q", reply)
	}
	if len(got.Contents) != 3 || got.Contents[0].Role != "user" || got.Contents[1].Role != "model" ||
		got.Contents[2].Parts[0].Text != "What snacks can Rex have?" {
		t.Errorf("contents = %+v", got.Contents)
	}
	if sys := got.SystemInstruction.Parts[0].Text; !strings.Contains(sys, "Food and diet: chicken allergy") {
		t.Errorf("system prompt missing profile:\n%s", sys)
	}
}

func TestGeminiErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		code int
		body string
	}{
		"http error": {400, `{"error":{"message":"API key not valid"}}`},
		"no answer":  {200, `{"candidates":[]}`},
		"blocked":    {200, `{"promptFeedback":{"blockReason":"SAFETY"}}`},
		"empty":      {200, `{"candidates":[{"content":{"parts":[{"text":"  "}]}}]}`},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.code)
			w.Write([]byte(tc.body))
		}))
		g := &Gemini{URL: srv.URL}
		if _, err := g.Suggest(context.Background(), Situation{Dog: Dog{Name: "Rex"}}); err == nil {
			t.Errorf("%s: suggest: expected error", name)
		}
		if _, err := g.Chat(context.Background(), Dog{Name: "Rex"}, []store.ChatMessage{{Role: "user", Text: "hi"}}); err == nil {
			t.Errorf("%s: chat: expected error", name)
		}
		srv.Close()
	}
}

func TestCleanCapsLength(t *testing.T) {
	if got := clean(strings.Repeat("calm ", 100)); len([]rune(got)) != MaxLen || !strings.HasSuffix(got, "…") {
		t.Errorf("len %d: %q", len([]rune(got)), got)
	}
}
