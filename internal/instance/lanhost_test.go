package instance

import (
	"net"
	"strings"
	"testing"
)

// TestLANHost: the default-route address is empty or a usable IPv4 that belongs to this host,
// and asking twice gives the same answer.
func TestLANHost(t *testing.T) {
	got := LANHost()
	if again := LANHost(); again != got {
		t.Errorf("LANHost() = %q then %q, want a stable value", got, again)
	}
	if got == "" {
		t.Skip("no default route on this host: lan_host is legitimately unknown")
	}
	ip := net.ParseIP(got)
	if ip == nil || ip.To4() == nil || strings.Contains(got, ":") {
		t.Fatalf("LANHost() = %q, want a dotted IPv4 address", got)
	}
	if ip.IsLoopback() || ip.IsUnspecified() {
		t.Errorf("LANHost() = %q, want neither loopback nor unspecified", got)
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("net.InterfaceAddrs: %v", err)
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(ip) {
			return
		}
	}
	t.Errorf("LANHost() = %q is not an address of any interface of this host", got)
}
