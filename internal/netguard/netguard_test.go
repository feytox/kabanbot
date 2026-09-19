package netguard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestAllowed(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8":          true,
		"2606:4700::1111":  true,
		"127.0.0.1":        false,
		"::1":              false,
		"10.1.2.3":         false,
		"172.16.0.1":       false,
		"192.168.1.1":      false,
		"169.254.169.254":  false, // cloud metadata
		"100.64.0.1":       false,
		"0.0.0.0":          false,
		"fd00::1":          false,
		"fe80::1":          false,
		"::ffff:127.0.0.1": false,
		"::ffff:1.1.1.1":   true,
		"224.0.0.1":        false,
	} {
		if got := Allowed(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Allowed(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestClientBlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("request reached the loopback server")
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	resp, err := NewClient().Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("want error")
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("err = %v, want ErrBlocked", err)
	}
}
