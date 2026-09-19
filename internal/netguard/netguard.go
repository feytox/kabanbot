// Package netguard provides an HTTP client that only connects to public internet addresses.
// It protects the server from requests to internal services (SSRF) via user-supplied URLs.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// ErrBlocked is returned when a connection to a non-public address is attempted.
var ErrBlocked = errors.New("netguard: connection to a non-public address is not allowed")

// cgnat is the carrier-grade NAT range, which netip does not classify as private.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// Allowed reports whether addr is a public unicast address.
func Allowed(addr netip.Addr) bool {
	addr = addr.Unmap()
	switch {
	case !addr.IsValid(),
		addr.IsLoopback(),
		addr.IsPrivate(),
		addr.IsUnspecified(),
		addr.IsLinkLocalUnicast(),
		addr.IsLinkLocalMulticast(),
		addr.IsInterfaceLocalMulticast(),
		addr.IsMulticast(),
		cgnat.Contains(addr):
		return false
	}
	return true
}

// control checks the resolved address right before connecting, so DNS rebinding cannot bypass it.
func control(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("netguard: %w", err)
	}
	if !Allowed(ap.Addr()) {
		return fmt.Errorf("%w: %s", ErrBlocked, ap.Addr())
	}
	return nil
}

// NewClient returns an HTTP client that refuses to connect to non-public addresses.
func NewClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: control}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// A proxy would be dialed instead of the target, defeating the check.
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, addr)
	}
	return &http.Client{Transport: transport, Timeout: 2 * time.Minute}
}
