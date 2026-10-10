// Command server runs the Canivo backend.
//
// Environment:
//
//	PORT               listen port (default 8090)
//	API_KEY            secret clients must send as "Authorization: Bearer <key>";
//	                   unset = no auth (local dev only)
//	DATABASE_URL       Postgres URL; if unset, uses SQLite at DB_PATH
//	DB_PATH            SQLite file (default canivo.db)
//	NOTIFIER           "expo" (default) or "log" to print pushes instead of sending
//	EXPO_ACCESS_TOKEN  optional, only if Expo push security is enabled
//	PUSH_COOLDOWN      min time between pushes per dog (default 2m)
//	VAPID_PUBLIC_KEY   } browser push (web app / iPhone Home Screen / Apple Watch);
//	VAPID_PRIVATE_KEY  } generate with: go run ./cmd/vapidkeys
//	VAPID_SUBJECT      contact email or https URL, required by Apple for web push
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

	"github.com/codingcassandra/Canivo/internal/api"
	"github.com/codingcassandra/Canivo/internal/monitor"
	"github.com/codingcassandra/Canivo/internal/notify"
	"github.com/codingcassandra/Canivo/internal/store"
)

func main() {
	port := env("PORT", "8090")
	cooldown, err := time.ParseDuration(env("PUSH_COOLDOWN", "2m"))
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

	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		log.Print("WARNING: API_KEY not set; anyone who can reach this server can use it")
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
