package instance

import "net"

// probeAddr is a TEST-NET-1 documentation address (RFC 5737): it is never routed, and a UDP
// "connect" to it only asks the kernel which local address it would use. No packet is sent.
const probeAddr = "192.0.2.1:9"

// LANHost returns the IPv4 address of the interface that holds the default route, which is the
// address a Home Assistant on another machine of the LAN reaches the firmware's native API at
// (the firmware listens on 0.0.0.0). It returns "" when there is no default route or the answer
// is a loopback or unspecified address; the user then reads the address themselves.
func LANHost() string {
	var d net.Dialer // no Timeout or Deadline: a UDP connect never waits for the network
	conn, err := d.Dial("udp4", probeAddr)
	if err != nil {
		return ""
	}
	defer conn.Close()
	ua, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || ua.IP == nil {
		return ""
	}
	ip := ua.IP.To4()
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return ""
	}
	return ip.String()
}
