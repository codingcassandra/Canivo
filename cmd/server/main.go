// Command server runs the Canivo backend.
//
// Environment:
//
//	PORT               listen port (default 8090)
//	API_KEY            device key for the bridge / GPS collar, sent as
//	                   "Authorization: Bearer <key>"; reaches every dog. Owners
//	                   sign in with email + password instead.
//	                   unset = requests without a token get full access (local dev only)
//	DATABASE_URL       Postgres URL; if unset, uses SQLite at DB_PATH
//	DB_PATH            SQLite file (default canivo.db)
//	NOTIFIER           "expo" (default) or "log" to print pushes instead of sending
//	EXPO_ACCESS_TOKEN  optional, only if Expo push security is enabled
//	PUSH_COOLDOWN      an alert starting within this long after the previous one
//	                   ended is the same episode and isn't pushed again (default 30s)
//	VAPID_PUBLIC_KEY   } browser push (web app / iPhone Home Screen / Apple Watch);
//	VAPID_PRIVATE_KEY  } generate with: go run ./cmd/vapidkeys
//	VAPID_SUBJECT      contact email or https URL, required by Apple for web push
//	GEMINI_API_KEY     turns on the AI assistant: personalized calming tips in
//	                   alert pushes and the in-app chat (key from
//	                   https://aistudio.google.com/apikey); unset = fixed wording, no chat
//	GEMINI_MODEL       Gemini model (default gemini-3.5-flash-lite)
//	ALLOWED_ORIGINS    comma-separated web app origins allowed to call the API,
//	                   e.g. https://canivo.vercel.app
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/codingcassandra/Canivo/internal/advice"
	"github.com/codingcassandra/Canivo/internal/api"
	"github.com/codingcassandra/Canivo/internal/monitor"
	"github.com/codingcassandra/Canivo/internal/notify"
	"github.com/codingcassandra/Canivo/internal/store"
)

func main() {
	port := env("PORT", "8090")
	cooldown, err := time.ParseDuration(env("PUSH_COOLDOWN", "30s"))
	if err != nil {
		log.Fatalf("PUSH_COOLDOWN: %v", err)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = env("DB_PATH", "canivo.db")
	}
	st, err := store.Open(dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()
	// Live state is in memory, so alerts left open by a previous run can never clear.
	if err := st.EndOpenAlerts(context.Background(), time.Now()); err != nil {
		log.Fatalf("close stale alerts: %v", err)
	}

	apiKey := strings.TrimSpace(os.Getenv("API_KEY")) // a pasted value may carry a stray space/newline
	if apiKey == "" {
		log.Print("WARNING: API_KEY not set; requests without a token get full access (local dev only)")
	}

	var n notify.Notifier = &notify.Expo{AccessToken: os.Getenv("EXPO_ACCESS_TOKEN")}
	if env("NOTIFIER", "expo") == "log" {
		n = notify.Log{}
	}

	opts := api.Options{PushCooldown: cooldown, APIKey: apiKey}
	if pub, priv := os.Getenv("VAPID_PUBLIC_KEY"), os.Getenv("VAPID_PRIVATE_KEY"); pub != "" && priv != "" {
		subject := os.Getenv("VAPID_SUBJECT")
		if subject == "" {
			log.Fatal("VAPID_SUBJECT is required with VAPID keys (your email or site URL)")
		}
		opts.WebPush = &notify.WebPush{PublicKey: pub, PrivateKey: priv, Subject: subject}
		opts.VAPIDPublicKey = pub
	} else {
		log.Print("web push disabled (VAPID_PUBLIC_KEY / VAPID_PRIVATE_KEY not set)")
	}
	if key := strings.TrimSpace(os.Getenv("GEMINI_API_KEY")); key != "" {
		opts.Advisor = &advice.Gemini{APIKey: key, Model: os.Getenv("GEMINI_MODEL")}
	} else {
		log.Print("assistant disabled (GEMINI_API_KEY not set): no calming tips or chat")
	}
	for _, o := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			opts.AllowedOrigins = append(opts.AllowedOrigins, o)
		}
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           api.New(st, n, monitor.DefaultConfig(), opts).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		log.Printf("canivo listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
