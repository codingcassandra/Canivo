// Command server runs the Canivo backend.
//
// Environment:
//
//	PORT               listen port (default 8090)
//	DB_PATH            SQLite file (default canivo.db)
//	NOTIFIER           "expo" (default) or "log" to print pushes instead of sending
//	EXPO_ACCESS_TOKEN  optional, only if Expo push security is enabled
//	PUSH_COOLDOWN      min time between pushes per dog (default 2m)
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
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

	st, err := store.Open(env("DB_PATH", "canivo.db"))
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()
	// Live state is in memory, so alerts left open by a previous run can never clear.
	if err := st.EndOpenAlerts(context.Background(), time.Now()); err != nil {
		log.Fatalf("close stale alerts: %v", err)
	}

	var n notify.Notifier = &notify.Expo{AccessToken: os.Getenv("EXPO_ACCESS_TOKEN")}
	if env("NOTIFIER", "expo") == "log" {
		n = notify.Log{}
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           api.New(st, n, monitor.DefaultConfig(), cooldown).Handler(),
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
