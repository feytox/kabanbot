package telegram

import (
	"math"
	"testing"
)

func TestRouteRoundTrip(t *testing.T) {
	for _, r := range []route{
		{op: opHome},
		{op: opGroup, id: -1001234567890},
		{op: opGroupToggle, id: -100, word: toggleSummary},
		{op: opGroupModel, id: math.MinInt64, id2: math.MaxInt64},
		{op: opGroupModel, id: -100, id2: 0},
		{op: opProviderCreate, word: "gemini"},
		{op: opProviderEdit, id: 7, word: fieldKey},
		{op: opModelUnbind, id: math.MaxInt64, id2: math.MinInt64},
		{op: opModelEdit, id: 1, word: fieldTemperature},
	} {
		data := r.String()
		if len(data) > callbackDataLimit {
			t.Errorf("%q is %d bytes, more than %d", data, len(data), callbackDataLimit)
		}
		got, err := parseRoute(data)
		if err != nil || got != r {
			t.Errorf("parseRoute(%q) = %+v, %v; want %+v", data, got, err, r)
		}
	}
}

func TestEveryRouteFitsCallbackData(t *testing.T) {
	for op, spec := range routeSpecs {
		r := route{op: op, id: math.MinInt64, id2: math.MinInt64}
		for _, w := range spec.words {
			r.word = w
			if n := len(r.String()); n > callbackDataLimit {
				t.Errorf("%q is %d bytes", r.String(), n)
			}
		}
		if n := len(r.String()); spec.words == nil && n > callbackDataLimit {
			t.Errorf("%q is %d bytes", r.String(), n)
		}
	}
}

func TestParseRouteRejectsGarbage(t *testing.T) {
	for _, data := range []string{
		"",
		":",
		"g",
		"g:",
		"g:abc",
		"g:1:2",
		"g:1.5",
		"g: 1",
		"g:99999999999999999999",
		"gt:1",
		"gt:1:bogus",
		"gt:1:sum:extra",
		"gm:1",
		"pk:anthropic",
		"pk:",
		"home:1",
		"unknown:1",
		"G:1",
		"g:1\x00",
		"\xff\xfe",
		"mu:1:2:3:4:5:6:7:8:9:10:11:12:13:14:15:16:17:18:19:20:21:22:23:24:25:26:27:28:29",
	} {
		if r, err := parseRoute(data); err == nil {
			t.Errorf("parseRoute(%q) = %+v, want an error", data, r)
		}
	}
}

func FuzzParseRoute(f *testing.F) {
	for _, s := range []string{"g:-100", "gt:1:on", "pk:openai", "mu:1:-2", "home", "::::"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data string) {
		r, err := parseRoute(data)
		if err != nil {
			return
		}
		again, err := parseRoute(r.String())
		if err != nil || again != r {
			t.Errorf("%q parsed as %+v, which does not round-trip", data, r)
		}
	})
}
