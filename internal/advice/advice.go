// Package advice is Canivo's AI assistant: it writes a short, personal tip for
// calming a dog that is on high alert, to go in the owner's push notification,
// and answers the owner's questions about their dog in the app's chat.
package advice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/codingcassandra/Canivo/internal/store"
)

// Dog is what the assistant knows about the dog.
type Dog struct {
	Name         string
	Profile      store.Profile
	UsualRateBPM float64       // calm breathing rate; 0 = not calibrated yet
	RecentAlerts []store.Alert // newest first
}

// Situation is the dog at the moment an alert starts. Breathing isn't part
// of it: tips never talk about the dog's breathing.
type Situation struct {
	Dog
	Danger       bool    // danger level, not just high alert
	TensionUpPct float64 // muscle tension above baseline, in percent; <= 0 if not raised
}

// Advisor is the assistant.
type Advisor interface {
	// Suggest returns one way to calm the dog down right now.
	Suggest(ctx context.Context, s Situation) (string, error)
	// Chat answers the last message in history (oldest first, ending with the owner's question).
	Chat(ctx context.Context, d Dog, history []store.ChatMessage) (string, error)
}

const (
	// MaxLen keeps the tip short enough to read on a lock screen or watch.
	MaxLen = 160
	// MaxReplyLen caps a chat answer.
	MaxReplyLen = 3000
)

// Gemini runs the assistant on Google's Gemini API.
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

const tipPrompt = `You help dog owners calm their dog in the moment. A sensor on the dog has just detected it is on high alert (stressed, reactive or fearful), or at the more serious danger level.
Reply with ONE practical, gentle, humane thing the owner can do right now to calm or redirect the dog, addressed to the owner, using the dog's name. Don't guess the dog's sex: say "they" unless the owner's notes say otherwise.
Personalize it: if the owner has said what helps the dog calm down, prefer those things; take the dog's known triggers, age, size and health into account (e.g. no treats that clash with allergies or diet). Don't repeat the recent tips listed; vary the wording and the technique.
Rules: a single sentence, under 140 characters, plain text, no emoji, no greeting, no quotes. Never mention the dog's breathing, breaths, panting or respiration. Never suggest punishment, yelling, leash corrections or medication. At the danger level, tell the owner to go to the dog now, move them somewhere cool and quiet, and call a vet if they don't settle.`

const chatPrompt = `You are Canivo's dog-care assistant, inside an app where an owner watches their dog's stress through a harness that senses breathing and muscle tension. Answer the owner's questions about their dog: food and nutrition, treats, training, behavior, exercise, grooming, health and general dog facts. Personalize every answer to the dog described below (breed, age, weight, diet, health, triggers) and use the dog's name. Don't guess the dog's sex: say "they" unless the owner's notes say otherwise.
- Format for a quick read on a phone: answer with 2 to 4 bullet points, each starting with "- " on its own line and under 15 words. No intro or closing sentence, no sub-bullets, no headings, bold, tables or emoji. If something needs a warning (like seeing a vet), make it one of the bullets. If the owner asks for more detail, you may use up to 6 bullets.
- Food: give specific suggestions that suit the dog's age, size, health and allergies. Never recommend anything toxic to dogs (chocolate, grapes or raisins, onion, garlic, xylitol, macadamia nuts, alcohol, caffeine, cooked bones). Suggest checking big diet changes with a vet.
- Health: you are not a vet. For symptoms, injuries, suspected poisoning or anything urgent, clearly tell the owner to contact a vet (or an emergency vet) now.
- Training and stress: only humane, reward-based methods. Never suggest punishment, prong or shock collars, or leash corrections.
- If asked about something unrelated to dogs or pet care, briefly say you can only help with dog questions.
- If knowing something missing from the profile would clearly improve your answer, you may suggest the owner add it to the dog's profile in the app.`

type geminiPart struct {
	Text    string `json:"text"`
	Thought bool   `json:"thought,omitempty"`
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
		Content      geminiContent `json:"content"`
		FinishReason string        `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}

func (g *Gemini) Suggest(ctx context.Context, s Situation) (string, error) {
	// High temperature for some variety, so a dog that alerts often doesn't
	// get the same tip every time.
	text, err := g.generate(ctx, tipPrompt, []geminiContent{userText(describe(s))}, 0.9, 512)
	if err != nil {
		return "", err
	}
	if tip := clean(text); tip != "" {
		return tip, nil
	}
	return "", errors.New("gemini: empty suggestion")
}

func (g *Gemini) Chat(ctx context.Context, d Dog, history []store.ChatMessage) (string, error) {
	system := chatPrompt + "\n\n" + about(d, time.Now()) + "\nToday is " + time.Now().UTC().Format("Monday, January 2, 2006") + "."
	var contents []geminiContent
	for _, m := range history {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		if len(contents) == 0 && role != "user" {
			continue // Gemini wants the conversation to start with the user
		}
		contents = append(contents, geminiContent{Role: role, Parts: []geminiPart{{Text: m.Text}}})
	}
	if len(contents) == 0 {
		return "", errors.New("chat: no question")
	}
	text, err := g.generate(ctx, system, contents, 0.7, 2048)
	if err != nil {
		return "", err
	}
	if reply := cleanReply(text); reply != "" {
		return reply, nil
	}
	return "", errors.New("gemini: empty reply")
}

func userText(s string) geminiContent {
	return geminiContent{Role: "user", Parts: []geminiPart{{Text: s}}}
}

// generate calls generateContent and returns the answer's text.
func (g *Gemini) generate(ctx context.Context, system string, contents []geminiContent, temperature float64, maxTokens int) (string, error) {
	req := geminiRequest{
		SystemInstruction: geminiContent{Parts: []geminiPart{{Text: system}}},
		Contents:          contents,
	}
	req.GenerationConfig.Temperature = temperature
	req.GenerationConfig.MaxOutputTokens = maxTokens
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
		client = &http.Client{Timeout: 45 * time.Second} // callers set tighter deadlines via ctx
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
	if r := out.PromptFeedback.BlockReason; r != "" {
		return "", fmt.Errorf("gemini: blocked (%s)", r)
	}
	if len(out.Candidates) == 0 {
		return "", errors.New("gemini: no answer")
	}
	var text strings.Builder
	for _, p := range out.Candidates[0].Content.Parts {
		if !p.Thought {
			text.WriteString(p.Text)
		}
	}
	return text.String(), nil
}

// about describes the dog for the model, e.g.
//
//	About the dog:
//	Name: Rex
//	Breed: border collie
//	...
func about(d Dog, now time.Time) string {
	var b strings.Builder
	b.WriteString("About the dog:\nName: " + d.Name + "\n")
	p := d.Profile
	for _, f := range []struct{ label, v string }{
		{"Breed", p.Breed},
		{"Age", p.Age},
		{"Weight", p.Weight},
		{"Food and diet", p.Diet},
		{"Health", p.Health},
		{"What sets them off", p.Triggers},
		{"What helps them calm down", p.Calming},
		{"Other notes from the owner", p.Notes},
	} {
		if v := strings.TrimSpace(f.v); v != "" {
			b.WriteString(f.label + ": " + strings.Join(strings.Fields(v), " ") + "\n")
		}
	}
	if d.UsualRateBPM > 0 {
		fmt.Fprintf(&b, "Calm breathing rate: %.0f breaths/min\n", d.UsualRateBPM)
	}
	week := 0
	for _, a := range d.RecentAlerts {
		if now.Sub(a.StartedAt) < 7*24*time.Hour {
			week++
		}
	}
	fmt.Fprintf(&b, "High-alert episodes in the last 7 days: %d\n", week)
	if len(d.RecentAlerts) > 0 {
		fmt.Fprintf(&b, "Most recent high alert: %s ago\n", roughly(now.Sub(d.RecentAlerts[0].StartedAt)))
	}
	return b.String()
}

// describe turns the alert into the tip prompt. It leaves out breathing
// entirely, so the tip can't bring it up.
func describe(s Situation) string {
	var b strings.Builder
	d := s.Dog
	d.UsualRateBPM = 0
	b.WriteString(about(d, time.Now()))
	if s.Danger {
		b.WriteString("\nRight now: DANGER level, the most serious.")
	} else {
		b.WriteString("\nRight now: high alert.")
	}
	if s.TensionUpPct >= 10 {
		fmt.Fprintf(&b, " Muscle tension up %.0f%%.", s.TensionUpPct)
	}
	var tips []string
	for _, a := range s.RecentAlerts {
		if a.Tip != "" && len(tips) < 3 {
			tips = append(tips, "- "+a.Tip)
		}
	}
	if len(tips) > 0 {
		b.WriteString("\nRecent tips (don't repeat):\n" + strings.Join(tips, "\n"))
	}
	return b.String()
}

func roughly(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}

// clean flattens the tip to one line and caps its length.
func clean(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, `"'* `)
	return capLen(s, MaxLen)
}

// mentionsBreathing catches a tip that talks about breathing despite the
// instructions; such a tip is dropped for the standard wording.
var mentionsBreathing = regexp.MustCompile(`(?i)breath|\bpant|respirat|\binhal|\bexhal`)

// MentionsBreathing reports whether text talks about the dog's breathing.
func MentionsBreathing(text string) bool { return mentionsBreathing.MatchString(text) }

var (
	headingRe   = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	bulletRe    = regexp.MustCompile(`(?m)^[ \t]*(?:[-*•]|\d+[.)])[ \t]+`)
	blankRunsRe = regexp.MustCompile(`\n{3,}`)
)

// cleanReply strips the markdown the app can't render (bold, headings), turns
// any list marker ("- ", "* ", "1. ") into "• ", and caps the length.
func cleanReply(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = headingRe.ReplaceAllString(s, "")
	s = bulletRe.ReplaceAllString(s, "• ")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = blankRunsRe.ReplaceAllString(strings.TrimSpace(s), "\n\n")
	return capLen(s, MaxReplyLen)
}

func capLen(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n-1])) + "…"
	}
	return s
}
