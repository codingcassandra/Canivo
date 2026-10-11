package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codingcassandra/Canivo/internal/monitor"
	"github.com/codingcassandra/Canivo/internal/notify"
	"github.com/codingcassandra/Canivo/internal/store"
)

type authEnv struct {
	t   *testing.T
	s   *Server
	st  *store.Store
	now time.Time
}

func newAuthEnv(t *testing.T) *authEnv {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := &authEnv{t: t, st: st, now: time.Now()}
	e.s = New(st, notify.Log{}, monitor.DefaultConfig(), Options{APIKey: "devicekey"})
	e.s.now = func() time.Time { return e.now }
	return e
}

// call sends a request with an optional bearer token and decodes the reply.
func (e *authEnv) call(method, path, token, body string, out any) int {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.s.Handler().ServeHTTP(rec, req)
	if out != nil {
		json.Unmarshal(rec.Body.Bytes(), out)
	}
	return rec.Code
}

func (e *authEnv) signUp(email string, remember bool) string {
	e.t.Helper()
	var resp authResponse
	body := `{"email":"` + email + `","password":"correct horse","remember":` + map[bool]string{true: "true", false: "false"}[remember] + `}`
	if code := e.call("POST", "/api/auth/register", "", body, &resp); code != 201 || resp.Token == "" {
		e.t.Fatalf("register %s: %d %+v", email, code, resp)
	}
	return resp.Token
}

func TestAuth(t *testing.T) {
	e := newAuthEnv(t)

	// A dog from before accounts existed (e.g. created with the API key).
	e.call("POST", "/api/dogs", "devicekey", `{"name":"Old Rex"}`, nil)

	// Validation.
	for _, body := range []string{
		`{"email":"nope","password":"correct horse"}`,
		`{"email":"a@b.co","password":"short"}`,
		`{"email":"a@b.co","password":"` + strings.Repeat("x", 73) + `"}`,
	} {
		if code := e.call("POST", "/api/auth/register", "", body, nil); code != 400 {
			t.Errorf("register %s: %d", body, code)
		}
	}

	sam := e.signUp("  Sam@Example.com ", true)
	if code := e.call("POST", "/api/auth/register", "", `{"email":"sam@example.com","password":"another one"}`, nil); code != 409 {
		t.Errorf("duplicate email: %d", code)
	}
	var me store.User
	if code := e.call("GET", "/api/me", sam, "", &me); code != 200 || me.Email != "sam@example.com" {
		t.Fatalf("me: %d %+v", code, me)
	}

	// The first account takes over dogs that had no owner.
	var dogs []store.Dog
	e.call("GET", "/api/dogs", sam, "", &dogs)
	if len(dogs) != 1 || dogs[0].Name != "Old Rex" {
		t.Fatalf("sam's dogs = %+v", dogs)
	}

	// A second owner sees only their own dogs.
	kim := e.signUp("kim@example.com", false)
	var kimDog store.Dog
	if code := e.call("POST", "/api/dogs", kim, `{"name":"Luna"}`, &kimDog); code != 201 {
		t.Fatalf("kim create dog: %d", code)
	}
	e.call("GET", "/api/dogs", kim, "", &dogs)
	if len(dogs) != 1 || dogs[0].Name != "Luna" {
		t.Fatalf("kim's dogs = %+v", dogs)
	}
	for _, path := range []string{"/api/dogs/1", "/api/dogs/1/status", "/api/dogs/1/alerts", "/api/dogs/1/location"} {
		if code := e.call("GET", path, kim, "", nil); code != 404 {
			t.Errorf("kim GET %s (sam's dog): %d", path, code)
		}
	}
	if code := e.call("POST", "/api/dogs/1/calibrate", kim, "", nil); code != 404 {
		t.Errorf("kim recalibrating sam's dog: %d", code)
	}
	if code := e.call("PUT", "/api/dogs/1/zone", kim, `{"lat":40.7,"lon":-74,"radius_m":100}`, nil); code != 404 {
		t.Errorf("kim moving sam's zone: %d", code)
	}
	if code := e.call("GET", "/api/dogs/1/status", sam, "", nil); code != 200 {
		t.Errorf("sam's own dog: %d", code)
	}

	// The device key still reaches every dog, so the bridge keeps working.
	if code := e.call("POST", "/api/dogs/2/samples", "devicekey", `{"samples":[{"t":1,"v":200}]}`, nil); code != 200 {
		t.Errorf("device key ingest: %d", code)
	}
	e.call("GET", "/api/dogs", "devicekey", "", &dogs)
	if len(dogs) != 2 {
		t.Errorf("device key sees %d dogs", len(dogs))
	}

	// No token / bad token.
	if code := e.call("GET", "/api/dogs", "", "", nil); code != 401 {
		t.Errorf("no token: %d", code)
	}
	if code := e.call("GET", "/api/dogs", "made-up", "", nil); code != 401 {
		t.Errorf("bad token: %d", code)
	}

	// Sign in.
	var login authResponse
	if code := e.call("POST", "/api/auth/login", "", `{"email":"SAM@example.com","password":"wrong pass"}`, nil); code != 401 {
		t.Errorf("wrong password: %d", code)
	}
	if code := e.call("POST", "/api/auth/login", "", `{"email":"nobody@example.com","password":"correct horse"}`, nil); code != 401 {
		t.Errorf("unknown email: %d", code)
	}
	if code := e.call("POST", "/api/auth/login", "", `{"email":"SAM@example.com","password":"correct horse","remember":true}`, &login); code != 200 || login.User.Email != "sam@example.com" {
		t.Fatalf("login: %d %+v", code, login)
	}
	if d := login.ExpiresAt.Sub(e.now); d < 89*24*time.Hour {
		t.Errorf("remembered session lasts %v", d)
	}

	// Sign out ends only that session.
	if code := e.call("POST", "/api/auth/logout", login.Token, "", nil); code != 204 {
		t.Fatalf("logout: %d", code)
	}
	if code := e.call("GET", "/api/me", login.Token, "", nil); code != 401 {
		t.Errorf("token after logout: %d", code)
	}
	if code := e.call("GET", "/api/me", sam, "", nil); code != 200 {
		t.Errorf("other session after logout: %d", code)
	}

	// Kim didn't tick "remember me": signed out after 12 hours.
	e.now = e.now.Add(13 * time.Hour)
	if code := e.call("GET", "/api/me", kim, "", nil); code != 401 {
		t.Errorf("short session after 13h: %d", code)
	}

	// Sam's remembered session slides: used every 60 days, it never expires.
	for i := 0; i < 3; i++ {
		e.now = e.now.Add(60 * 24 * time.Hour)
		if code := e.call("GET", "/api/me", sam, "", nil); code != 200 {
			t.Fatalf("remembered session after %d x 60 days: %d", i+1, code)
		}
	}
	// ...but 91 days without opening the app signs you out.
	e.now = e.now.Add(91 * 24 * time.Hour)
	if code := e.call("GET", "/api/me", sam, "", nil); code != 401 {
		t.Errorf("remembered session after 91 idle days: %d", code)
	}
}

func TestLoginLimit(t *testing.T) {
	e := newAuthEnv(t)
	e.signUp("sam@example.com", false)
	bad := `{"email":"sam@example.com","password":"guess guess"}`
	good := `{"email":"sam@example.com","password":"correct horse"}`
	for i := 0; i < maxLoginFailures; i++ {
		if code := e.call("POST", "/api/auth/login", "", bad, nil); code != 401 {
			t.Fatalf("attempt %d: %d", i, code)
		}
	}
	var resp map[string]string
	if code := e.call("POST", "/api/auth/login", "", good, &resp); code != 429 || !strings.Contains(resp["error"], "15 minutes") {
		t.Fatalf("after %d failures: %d %v", maxLoginFailures, code, resp)
	}
	e.now = e.now.Add(loginWindow)
	if code := e.call("POST", "/api/auth/login", "", good, nil); code != 200 {
		t.Fatalf("after waiting: %d", code)
	}
}

// Requests without any token are only allowed when no API key is configured.
func TestNoAPIKeyIsOpen(t *testing.T) {
	h, _ := setup(t)
	if code := do(t, h, "GET", "/api/dogs", "", nil); code != 200 {
		t.Errorf("no key configured, no token: %d", code)
	}
}
