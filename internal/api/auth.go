package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/codingcassandra/Canivo/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	// A "remember me" session lasts this long after the last time it was
	// used, so someone who opens the app now and then stays signed in.
	rememberTTL = 90 * 24 * time.Hour
	// Without "remember me" the app also forgets the token when it closes.
	shortTTL = 12 * time.Hour
	// Remembered sessions are pushed forward at most once a day.
	extendEvery = 24 * time.Hour

	maxLoginFailures = 10
	loginWindow      = 15 * time.Minute
)

// principal is who a request is from: the owner of a session, or a device
// (bridge, collar) using the shared API key, which can reach every dog.
type principal struct {
	admin     bool
	userID    int64
	tokenHash string
}

type ctxKey struct{}

func who(r *http.Request) principal {
	p, _ := r.Context().Value(ctxKey{}).(principal)
	return p
}

// authenticate lets through sign-up/sign-in and non-API paths, and requires
// a session token or the API key everywhere else.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/register" {
			next.ServeHTTP(w, r)
			return
		}
		token, hasBearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		var p principal
		switch {
		case !hasBearer || token == "":
			if s.opts.APIKey != "" {
				writeError(w, http.StatusUnauthorized, "please sign in")
				return
			}
			p.admin = true // no API_KEY configured: local development
		case s.opts.APIKey != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.opts.APIKey)) == 1:
			p.admin = true
		default:
			p.tokenHash = hashToken(token)
			now := s.now()
			sess, err := s.store.Session(r.Context(), p.tokenHash, now)
			if errors.Is(err, store.ErrNotFound) {
				// Either an expired sign-in (app) or a wrong API key (bridge, collar).
				writeError(w, http.StatusUnauthorized, "wrong API key, or your session has expired (sign in again)")
				return
			}
			if err != nil {
				serverError(w, err)
				return
			}
			p.userID = sess.UserID
			if sess.Expires.Sub(now) < sess.TTL-extendEvery {
				if err := s.store.ExtendSession(r.Context(), p.tokenHash, now.Add(sess.TTL)); err != nil {
					log.Printf("extend session: %v", err)
				}
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	})
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}

type authResponse struct {
	Token     string     `json:"token"`
	ExpiresAt time.Time  `json:"expires_at"`
	User      store.User `json:"user"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if !decode(w, r, &req) {
		return
	}
	email := normalizeEmail(req.Email)
	if !validEmail(email) {
		writeError(w, http.StatusBadRequest, "enter a valid email address")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if len(req.Password) > 72 {
		writeError(w, http.StatusBadRequest, "password must be at most 72 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		serverError(w, err)
		return
	}
	first, err := s.store.CountUsers(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	u, err := s.store.CreateUser(r.Context(), email, string(hash))
	if errors.Is(err, store.ErrExists) {
		writeError(w, http.StatusConflict, "an account with this email already exists, try signing in")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	// Dogs from before accounts existed belong to whoever signs up first.
	if first == 0 {
		n, err := s.store.ClaimOrphanDogs(r.Context(), u.ID)
		if err != nil {
			serverError(w, err)
			return
		}
		if n > 0 {
			log.Printf("first account %d took ownership of %d existing dog(s)", u.ID, n)
			s.refreshOwners(r.Context())
		}
	}
	s.startSession(w, r, u, req.Remember, http.StatusCreated)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if !decode(w, r, &req) {
		return
	}
	email := normalizeEmail(req.Email)
	if wait := s.limiter.blocked(email, s.now()); wait > 0 {
		writeError(w, http.StatusTooManyRequests,
			fmt.Sprintf("too many tries, wait %d minutes and try again", int(math.Ceil(wait.Minutes()))))
		return
	}
	u, hash, err := s.store.UserByEmail(r.Context(), email)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		serverError(w, err)
		return
	}
	if err != nil {
		hash = dummyHash // same work as a real check, so timing doesn't reveal which emails exist
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil || err != nil {
		s.limiter.fail(email, s.now())
		writeError(w, http.StatusUnauthorized, "wrong email or password")
		return
	}
	s.limiter.reset(email)
	if err := s.store.DeleteExpiredSessions(r.Context(), s.now()); err != nil {
		log.Printf("clean sessions: %v", err)
	}
	s.startSession(w, r, u, req.Remember, http.StatusOK)
}

var dummyHash, _ = func() (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte("not a real password"), bcrypt.DefaultCost)
	return string(h), err
}()

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u store.User, remember bool, status int) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		serverError(w, err)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	ttl := shortTTL
	if remember {
		ttl = rememberTTL
	}
	now := s.now()
	if err := s.store.CreateSession(r.Context(), hashToken(token), u.ID, ttl, now); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, status, authResponse{Token: token, ExpiresAt: now.Add(ttl).UTC(), User: u})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if p := who(r); p.tokenHash != "" {
		if err := s.store.DeleteSession(r.Context(), p.tokenHash); err != nil {
			serverError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p := who(r)
	if p.admin {
		writeJSON(w, http.StatusOK, map[string]any{"admin": true})
		return
	}
	u, err := s.store.User(r.Context(), p.userID)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

func validEmail(e string) bool {
	at := strings.LastIndex(e, "@")
	return at > 0 && at < len(e)-3 && strings.Contains(e[at:], ".") && len(e) <= 254 && !strings.ContainsAny(e, " \t\r\n")
}

// loginLimiter slows down password guessing: after maxLoginFailures wrong
// passwords for an email, that email is blocked for the rest of loginWindow.
type loginLimiter struct {
	mu    sync.Mutex
	fails map[string]*failures
}

type failures struct {
	count int
	start time.Time
}

func (l *loginLimiter) blocked(email string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.fails[email]
	if f == nil || now.Sub(f.start) >= loginWindow || f.count < maxLoginFailures {
		return 0
	}
	return f.start.Add(loginWindow).Sub(now)
}

func (l *loginLimiter) fail(email string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fails == nil {
		l.fails = map[string]*failures{}
	}
	if len(l.fails) > 10000 { // keep memory bounded
		for k, f := range l.fails {
			if now.Sub(f.start) >= loginWindow {
				delete(l.fails, k)
			}
		}
	}
	f := l.fails[email]
	if f == nil || now.Sub(f.start) >= loginWindow {
		f = &failures{start: now}
		l.fails[email] = f
	}
	f.count++
}

func (l *loginLimiter) reset(email string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, email)
}
