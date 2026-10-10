package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExpoSendsAndReportsInvalidTokens(t *testing.T) {
	var got []expoMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.Write([]byte(`{"data":[{"status":"ok","id":"a"},{"status":"error","message":"gone","details":{"error":"DeviceNotRegistered"}}]}`))
	}))
	defer srv.Close()

	e := &Expo{URL: srv.URL}
	invalid, err := e.Notify(context.Background(), []string{"tok1", "tok2"}, Message{Title: "Rex is on alert", Body: "redirect"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].To != "tok1" || got[1].Title != "Rex is on alert" || got[0].Priority != "high" {
		t.Errorf("sent %+v", got)
	}
	if len(invalid) != 1 || invalid[0] != "tok2" {
		t.Errorf("invalid = %v", invalid)
	}
}
