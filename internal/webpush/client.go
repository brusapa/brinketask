package webpush

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// NewClient returns the HTTP client for push services (SPEC section 10).
// Users choose the endpoints the server posts to, so the client must not
// become a way into the server's own network (SSRF):
//
//   - it never follows a redirect: the answer to the first request is the
//     answer, and a 3xx is a failure like any other;
//   - it refuses to connect to loopback, private, link-local, shared and
//     other non-public addresses. The check runs on the address being
//     dialled, after name resolution, so a name that resolves to such an
//     address is refused too, also when its DNS answer changes between
//     the registration and the send.
//
// allowLoopback lets it reach 127.0.0.1 and ::1, for the end-to-end
// test's fake push service only (D-73); the server sets it only with
// PUSH_TEST_ENDPOINT_PREFIX. No proxy from the environment is used: the
// address check would apply to the proxy instead of the push service.
func NewClient(timeout time.Duration, allowLoopback bool) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		// Control runs after the name is resolved and before each connect,
		// with the numeric address actually dialled.
		Control: func(_, address string, _ syscall.RawConn) error {
			addrPort, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("webpush: dial %s: %w", address, err)
			}
			addr := addrPort.Addr().Unmap()
			if allowLoopback && addr.IsLoopback() {
				return nil
			}
			if !publicAddress(addr) {
				return fmt.Errorf("webpush: %s is not a public address", addr)
			}
			return nil
		},
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			// http.ErrUseLastResponse makes Do return the 3xx response
			// itself instead of following it.
			return http.ErrUseLastResponse
		},
	}
}

// sharedAddressSpace is 100.64.0.0/10 (RFC 6598, carrier-grade NAT),
// which netip.Addr.IsPrivate does not cover.
var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

// publicAddress reports whether addr may be a push service: a global
// unicast address outside the private ranges (RFC 1918, RFC 4193) and the
// shared address space. Loopback, link-local (cloud metadata services at
// 169.254.169.254 included), multicast and unspecified addresses are not
// global unicast.
func publicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsGlobalUnicast() && !addr.IsPrivate() && !sharedAddressSpace.Contains(addr)
}
