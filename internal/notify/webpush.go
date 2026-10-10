package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// WebSubscription is a browser PushSubscription, as sent by
// PushSubscription.toJSON() in the web app.
type WebSubscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// WebPusher delivers a message to browser push subscriptions and returns the
// endpoints that no longer exist, so the caller can delete them.
type WebPusher interface {
	NotifyWeb(ctx context.Context, subs []WebSubscription, msg Message) (gone []string, err error)
}

// WebPush sends standard Web Push (VAPID). This is what reaches Safari on an
// iPhone Home Screen web app — and from there, a paired Apple Watch.
type WebPush struct {
	PublicKey  string
	PrivateKey string
	Subject    string // contact email or https URL; Apple requires one
	Client     webpush.HTTPClient
}

// GenerateVAPIDKeys makes a new key pair for WebPush.
func GenerateVAPIDKeys() (public, private string, err error) {
	private, public, err = webpush.GenerateVAPIDKeys()
	return public, private, err
}

func (w *WebPush) NotifyWeb(ctx context.Context, subs []WebSubscription, msg Message) ([]string, error) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	var gone []string
	var lastErr error
	for _, s := range subs {
		sub := &webpush.Subscription{Endpoint: s.Endpoint, Keys: webpush.Keys{P256dh: s.Keys.P256dh, Auth: s.Keys.Auth}}
		resp, err := webpush.SendNotificationWithContext(ctx, payload, sub, &webpush.Options{
			HTTPClient:      w.Client,
			Subscriber:      w.Subject,
			VAPIDPublicKey:  w.PublicKey,
			VAPIDPrivateKey: w.PrivateKey,
			TTL:             300, // an alert older than 5 minutes isn't useful
			Urgency:         webpush.UrgencyHigh,
		})
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
			gone = append(gone, s.Endpoint)
		case resp.StatusCode >= 300:
			log.Printf("web push to %s: %s %s", s.Endpoint, resp.Status, body)
			lastErr = fmt.Errorf("web push: status %d", resp.StatusCode)
		}
	}
	return gone, lastErr
}
