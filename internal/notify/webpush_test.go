package notify

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// browserSub makes a subscription with real browser-style keys so the
// payload encryption runs for real.
func browserSub(t *testing.T, endpoint string) WebSubscription {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	var s WebSubscription
	s.Endpoint = endpoint
	s.Keys.P256dh = base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
	s.Keys.Auth = base64.RawURLEncoding.EncodeToString(auth)
	return s
}

func TestWebPush(t *testing.T) {
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		if r.Header.Get("Urgency") != "high" || r.Header.Get("Content-Encoding") != "aes128gcm" {
			t.Errorf("headers = %v", r.Header)
		}
		if strings.HasSuffix(r.URL.Path, "/expired") {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	pub, priv, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	wp := &WebPush{PublicKey: pub, PrivateKey: priv, Subject: "owner@example.com"}
	subs := []WebSubscription{browserSub(t, srv.URL+"/ok"), browserSub(t, srv.URL+"/expired")}

	gone, err := wp.NotifyWeb(context.Background(), subs, Message{Title: "Rex is on high alert", Body: "redirect"})
	if err != nil {
		t.Fatal(err)
	}
	if len(gone) != 1 || !strings.HasSuffix(gone[0], "/expired") {
		t.Errorf("gone = %v", gone)
	}
	if len(auths) != 2 || !strings.HasPrefix(auths[0], "vapid t=") || !strings.Contains(auths[0], "k="+pub) {
		t.Errorf("authorization = %v", auths)
	}
}
