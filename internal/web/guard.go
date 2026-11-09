package web

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/control"
)

// GuardOptions configure Guard.
type GuardOptions struct {
	// AllowHosts are the host names, besides the built-in ones, a request may be addressed to
	// (`sil up --allow-host`). Compared in lower case.
	AllowHosts []string
	// Hostname is this machine's name; its bare and ".local" forms are accepted. The lower-cased
	// os.Hostname() when empty.
	Hostname string
}

// Guard puts the browser-facing protections of the LAN listener (D-08) around h, outermost first:
//
//  1. The Host allowlist (T-03-13). The listener has no authentication, so a page on another site
//     that rebinds its DNS name to this machine would otherwise talk to the bench as same-origin.
//     A request is served only when its Host is an IP literal, localhost, this machine's name,
//     <name>.local or an AllowHosts entry; anything else, a trailing dot included, answers 403
//     host_rejected before any handler runs.
//  2. The security headers on every response: the Content-Security-Policy that confines the page
//     to its own files and its own WebSocket, nosniff, X-Frame-Options DENY, Referrer-Policy
//     no-referrer, and Cache-Control no-cache unless the handler sets its own (T-03-16).
//  3. http.CrossOriginProtection (T-03-11): a request with an unsafe method that the browser marks
//     cross-site (Sec-Fetch-Site, else an Origin that is not the Host) answers 403 origin_rejected.
//     Requests with neither header (the CLI, curl, scripts) are not browser requests and pass. Safe
//     methods always pass, which is sound because control.Operations has no GET row that changes
//     anything (TestCSRFCoversEveryMutatingRoute).
//
// The /ws upgrade is a GET, so its origin check is the library's: websocket.Accept refuses an Origin
// that differs from the Host (T-03-12) as long as nobody sets InsecureSkipVerify or OriginPatterns.
//
// Guard never wraps the ResponseWriter, so Flusher and Hijacker reach the handler.
func Guard(h http.Handler, o GuardOptions) http.Handler {
	hostname := strings.ToLower(o.Hostname)
	if hostname == "" {
		if name, err := os.Hostname(); err == nil {
			hostname = strings.ToLower(name)
		}
	}
	allowed := map[string]bool{"localhost": true}
	if hostname != "" {
		allowed[hostname] = true
		allowed[hostname+".local"] = true
	}
	for _, a := range o.AllowHosts {
		allowed[strings.ToLower(a)] = true
	}

	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "Sec-Fetch-Site: " + r.Header.Get("Sec-Fetch-Site")
		}
		control.WriteErrorStatus(w, http.StatusForbidden,
			apperr.Failed("origin_rejected", "cross-origin request rejected: only the bench's own page may change it",
				map[string]any{"origin": origin}))
	}))
	protected := cop.Handler(h)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, ok := requestHost(r.Host, allowed)
		if !ok {
			// r.Host is not trusted: the policy of a refusal names no WebSocket host.
			setSecurityHeaders(w, "")
			control.WriteErrorStatus(w, http.StatusForbidden,
				apperr.Failed("host_rejected",
					fmt.Sprintf("requests for host %q are not accepted; start the bench with `sil up NAME --allow-host HOST` to reach it by that host name", r.Host),
					map[string]any{"host": r.Host}))
			return
		}
		setSecurityHeaders(w, host)
		protected.ServeHTTP(w, r)
	})
}

// requestHost validates the Host header against the allowlist and returns it, lower-cased, in the
// form a CSP host source takes (name[:port], IPv6 in brackets). ok is false for anything else: an
// empty host, a trailing dot, a malformed port, a name that is not allowed.
func requestHost(hostport string, allowed map[string]bool) (host string, ok bool) {
	name, port := hostport, ""
	if h, p, err := net.SplitHostPort(hostport); err == nil {
		name, port = h, p
		if port == "" || len(port) > 5 || strings.Trim(port, "0123456789") != "" {
			return "", false
		}
	} else if strings.HasPrefix(hostport, "[") && strings.HasSuffix(hostport, "]") {
		name = hostport[1 : len(hostport)-1] // a bracketed IPv6 literal without a port
	}
	name = strings.ToLower(name)
	if name == "" {
		return "", false
	}
	ip := net.ParseIP(name)
	if ip == nil && !allowed[name] {
		return "", false
	}
	out := name
	if ip != nil && strings.Contains(name, ":") {
		out = "[" + name + "]"
	}
	if port != "" {
		out += ":" + port
	}
	return out, true
}

// csp is the Content-Security-Policy of every response; %s is the validated Host, named explicitly
// in connect-src because older WebKit does not match ws: against 'self'. The page needs no inline
// script or style, no image from elsewhere and no embedding.
const csp = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self' ws://%s; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// setSecurityHeaders sets the headers of Guard; host is "" when it must not be named.
func setSecurityHeaders(w http.ResponseWriter, host string) {
	policy := strings.Replace(csp, " ws://%s", "", 1)
	if host != "" {
		policy = strings.Replace(csp, "%s", host, 1)
	}
	hdr := w.Header()
	hdr.Set("Content-Security-Policy", policy)
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("X-Frame-Options", "DENY")
	hdr.Set("Referrer-Policy", "no-referrer")
	hdr.Set("Cache-Control", "no-cache")
}
