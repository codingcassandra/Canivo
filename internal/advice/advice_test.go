package advice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiSuggest(t *testing.T) {
	var got geminiRequest
	var path, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, key = r.URL.Path, r.Header.Get("x-goog-api-key")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"\"Ask Rex for a sit,\n"},{"text":"then reward with a treat.\"\n"}]}}]}`))
	}))
	defer srv.Close()

	g := &Gemini{APIKey: "k3y", URL: srv.URL}
	tip, err := g.Suggest(context.Background(), Situation{Name: "Rex", RateBPM: 34, UsualRateBPM: 18, TensionUpPct: 40})
	if err != nil {
		t.Fatal(err)
	}
	if tip != "Ask Rex for a sit, then reward with a treat." {
		t.Errorf("tip = %q", tip)
	}
	if path != "/models/"+DefaultModel+":generateContent" || key != "k3y" {
		t.Errorf("path %q key %q", path, key)
	}
	prompt := got.Contents[0].Parts[0].Text
	if !strings.Contains(prompt, "Rex") || !strings.Contains(prompt, "34 breaths/min (usually 18)") || !strings.Contains(prompt, "up 40%") {
		t.Errorf("prompt = %q", prompt)
	}
}

func TestGeminiErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		code int
		body string
	}{
		"http error": {400, `{"error":{"message":"API key not valid"}}`},
		"no text":    {200, `{"candidates":[]}`},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.code)
			w.Write([]byte(tc.body))
		}))
		_, err := (&Gemini{URL: srv.URL}).Suggest(context.Background(), Situation{Name: "Rex"})
		srv.Close()
		if err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestCleanCapsLength(t *testing.T) {
	if got := clean(strings.Repeat("calm ", 100)); len([]rune(got)) != MaxLen || !strings.HasSuffix(got, "…") {
		t.Errorf("len %d: %q", len([]rune(got)), got)
	}
}
