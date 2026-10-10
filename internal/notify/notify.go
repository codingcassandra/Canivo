// Package notify sends push notifications to the owner's phone.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type Message struct {
	Title string         `json:"title"`
	Body  string         `json:"body"`
	Data  map[string]any `json:"data,omitempty"`
}

// Notifier delivers a message to push tokens. It returns tokens the push
// service says are no longer valid, so the caller can delete them.
type Notifier interface {
	Notify(ctx context.Context, tokens []string, msg Message) (invalid []string, err error)
}

// Expo sends through Expo's push service, which forwards to APNs/FCM.
// Tokens look like "ExponentPushToken[xxxx]" and come from the Expo app via
// Notifications.getExpoPushTokenAsync().
type Expo struct {
	URL         string // defaults to Expo's production endpoint
	AccessToken string // optional; only needed if push security is enabled in Expo
	Client      *http.Client
}

const expoPushURL = "https://exp.host/--/api/v2/push/send"

type expoMessage struct {
	To       string         `json:"to"`
	Title    string         `json:"title"`
	Body     string         `json:"body"`
	Data     map[string]any `json:"data,omitempty"`
	Sound    string         `json:"sound"`
	Priority string         `json:"priority"`
}

type expoResponse struct {
	Data []struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Details struct {
			Error string `json:"error"`
		} `json:"details"`
	} `json:"data"`
}

func (e *Expo) Notify(ctx context.Context, tokens []string, msg Message) ([]string, error) {
	if len(tokens) == 0 {
		return nil, nil
	}
	msgs := make([]expoMessage, len(tokens))
	for i, t := range tokens {
		msgs[i] = expoMessage{To: t, Title: msg.Title, Body: msg.Body, Data: msg.Data, Sound: "default", Priority: "high"}
	}
	body, err := json.Marshal(msgs)
	if err != nil {
		return nil, err
	}

	url := e.URL
	if url == "" {
		url = expoPushURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if e.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.AccessToken)
	}

	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("expo push: status %d", resp.StatusCode)
	}

	var out expoResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("expo push: decode response: %w", err)
	}
	// Tickets come back in the same order as the messages.
	var invalid []string
	for i, ticket := range out.Data {
		if ticket.Status == "error" && i < len(tokens) {
			if ticket.Details.Error == "DeviceNotRegistered" {
				invalid = append(invalid, tokens[i])
			} else {
				log.Printf("expo push to %s: %s (%s)", tokens[i], ticket.Message, ticket.Details.Error)
			}
		}
	}
	return invalid, nil
}

// Log prints notifications instead of sending them; handy without a phone.
type Log struct{}

func (Log) Notify(_ context.Context, tokens []string, msg Message) ([]string, error) {
	log.Printf("PUSH to %d device(s): %s — %s %v", len(tokens), msg.Title, msg.Body, msg.Data)
	return nil, nil
}
