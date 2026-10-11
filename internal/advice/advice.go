// Package advice writes a short, specific tip for calming a dog that is on
// high alert, to go in the owner's push notification.
package advice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Situation is what we know about the dog when an alert starts.
type Situation struct {
	Name         string
	RateBPM      float64 // breathing rate now
	UsualRateBPM float64 // calm baseline
	TensionUpPct float64 // muscle tension above baseline, in percent; <= 0 if not raised
}

// Advisor suggests one way to calm the dog down.
type Advisor interface {
	Suggest(ctx context.Context, s Situation) (string, error)
}

// MaxLen keeps the tip short enough to read on a lock screen or watch.
const MaxLen = 160

// Gemini asks Google's Gemini API for the tip.
type Gemini struct {
	APIKey string
	Model  string // defaults to DefaultModel
	URL    string // API base URL; defaults to Google's
	Client *http.Client
}

const (
	DefaultModel = "gemini-3.5-flash-lite"
	geminiURL    = "https://generativelanguage.googleapis.com/v1beta"
)

const systemPrompt = `You help dog owners calm their dog in the moment. A sensor on the dog has just detected it is on high alert (stressed, reactive or fearful), based on breathing rate and muscle tension.
Reply with ONE practical, gentle, humane thing the owner can do right now to calm or redirect the dog, addressed to the owner, using the dog's name.
Rules: a single sentence, under 140 characters, plain text, no emoji, no greeting, no quotes. Never suggest punishment, yelling, leash corrections or medication. If the breathing rate is extremely high (over 3x usual), suggest moving the dog somewhere cool and quiet and calling a vet if it doesn't settle.`

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiRequest struct {
	SystemInstruction geminiContent   `json:"systemInstruction"`
	Contents          []geminiContent `json:"contents"`
	GenerationConfig  struct {
		Temperature     float64 `json:"temperature"`
		MaxOutputTokens int     `json:"maxOutputTokens"`
	} `json:"generationConfig"`
}

type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
}

func (g *Gemini) Suggest(ctx context.Context, s Situation) (string, error) {
	req := geminiRequest{
		SystemInstruction: geminiContent{Parts: []geminiPart{{Text: systemPrompt}}},
		Contents:          []geminiContent{{Role: "user", Parts: []geminiPart{{Text: describe(s)}}}},
	}
	// Some variety so a dog that alerts often doesn't get the same tip every time.
	req.GenerationConfig.Temperature = 0.9
	req.GenerationConfig.MaxOutputTokens = 256
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	model, base := g.Model, g.URL
	if model == "" {
		model = DefaultModel
	}
	if base == "" {
		base = geminiURL
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/models/"+model+":generateContent", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", g.APIKey)

	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return "", fmt.Errorf("gemini: status %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}

	var out geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("gemini: decode response: %w", err)
	}
	var text strings.Builder
	if len(out.Candidates) > 0 {
		for _, p := range out.Candidates[0].Content.Parts {
			text.WriteString(p.Text)
		}
	}
	tip := clean(text.String())
	if tip == "" {
		return "", errors.New("gemini: empty suggestion")
	}
	return tip, nil
}

// describe turns the alert into the prompt, e.g.
// "Dog: Rex. Breathing 34 breaths/min (usually 18). Muscle tension up 40%."
func describe(s Situation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Dog: %s. Breathing %.0f breaths/min (usually %.0f).", s.Name, s.RateBPM, s.UsualRateBPM)
	if s.TensionUpPct >= 10 {
		fmt.Fprintf(&b, " Muscle tension up %.0f%%.", s.TensionUpPct)
	}
	return b.String()
}

// clean flattens the model's reply to one line and caps its length.
func clean(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, `"'* `)
	if r := []rune(s); len(r) > MaxLen {
		s = strings.TrimSpace(string(r[:MaxLen-1])) + "…"
	}
	return s
}
