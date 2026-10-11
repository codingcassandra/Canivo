package api

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/codingcassandra/Canivo/internal/advice"
	"github.com/codingcassandra/Canivo/internal/monitor"
)

type fakeAdvisor struct {
	tip string
	err error
	got advice.Situation
}

func (f *fakeAdvisor) Suggest(_ context.Context, s advice.Situation) (string, error) {
	f.got = s
	return f.tip, f.err
}

func TestAlertTip(t *testing.T) {
	ev := monitor.Event{RateBPM: 34, Tension: 280}
	b := monitor.Baseline{RateMean: 18, TensionMean: 200}

	a := &fakeAdvisor{tip: "Ask Rex to sit and reward him."}
	s := New(nil, nil, monitor.DefaultConfig(), Options{Advisor: a})
	body := alertBody("Rex", ev, b, s.suggest(1, "Rex", ev, b))
	if body != "Breathing 34 bpm (usually 18), muscle tension up 40%. Ask Rex to sit and reward him." {
		t.Errorf("body = %q", body)
	}
	if a.got.Name != "Rex" || a.got.UsualRateBPM != 18 || math.Round(a.got.TensionUpPct) != 40 {
		t.Errorf("situation = %+v", a.got)
	}

	// A failing advisor (or none) falls back to the fixed wording.
	s = New(nil, nil, monitor.DefaultConfig(), Options{Advisor: &fakeAdvisor{err: errors.New("quota")}})
	if body := alertBody("Rex", ev, b, s.suggest(1, "Rex", ev, b)); !strings.HasSuffix(body, "Time to redirect Rex.") {
		t.Errorf("fallback body = %q", body)
	}
	s = New(nil, nil, monitor.DefaultConfig(), Options{})
	if tip := s.suggest(1, "Rex", ev, b); tip != "" {
		t.Errorf("no advisor tip = %q", tip)
	}
}
