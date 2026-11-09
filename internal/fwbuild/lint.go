package fwbuild

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/zeroflow/esphome-sil/internal/apperr"
)

// Identity is what sil injected into the build; the lint asserts that the
// resolved config carries exactly these values and not hand-written ones.
type Identity struct {
	DeviceName string // esphome.name, e.g. sil-dev
	MAC        string // host.mac_address, compared case-insensitively
	Port       int    // api.port
}

// Problem is one reason a config is rejected.
type Problem struct {
	Component string `json:"component"`
	File      string `json:"file"`
	Message   string `json:"message"`
	Hint      string `json:"hint,omitempty"`
}

// bannedHint returns the replacement hint of a component that cannot be part
// of a host-platform sim build, matched by exact, case-sensitive name. It is a
// switch, not a package-level map, so the lint holds no mutable state.
func bannedHint(name string) (string, bool) {
	switch name {
	case "wifi":
		return "there is no Wi-Fi on the host; use api and on_boot hooks", true
	case "ledc":
		return "use output platform sim_bus", true
	case "pulse_counter":
		return "use sensor platform sim_bus; RPM is simulated", true
	case "esp32_rmt_led_strip":
		return "use light platform sim_bus with partition lights for the segments", true
	case "i2c":
		return "remove it; use sensor platform sim_bus with mode: latest for temperature/humidity", true
	case "ota":
		return "there is no OTA on the host", true
	case "http_request", "update":
		return "not available on the host", true
	case "captive_portal", "web_server":
		return "not available on the host", true
	case "sntp":
		return "use time platform homeassistant", true
	case "esp32":
		return "the sim build targets host:, remove the esp32: block", true
	case "json":
		return "it pulls ArduinoJson at build time and breaks the offline build; remove the component that loads it (web_server, mqtt, ...)", true
	}
	return "", false
}

// LintConfig is stage 1 of the lint: it checks the resolved `esphome config`
// output against the banned components and the identity assertions. It
// returns nil or an *apperr.Error with code lint_rejected whose detail lists
// every Problem (never just the first).
func LintConfig(resolved []byte, configPath string, want Identity) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(trimPreamble(resolved), &doc); err != nil {
		return apperr.Failed("config_invalid", configPath+": the resolved config is not valid YAML: "+err.Error(),
			map[string]any{"file": configPath})
	}
	root := &yaml.Node{}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		root = doc.Content[0]
	}

	var ps []Problem
	add := func(component, msg, hint string) {
		ps = append(ps, Problem{Component: component, File: configPath, Message: msg, Hint: hint})
	}

	// Banned components: exact top-level keys and exact platform: values.
	if root.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(root.Content); i += 2 {
			key, val := root.Content[i], root.Content[i+1]
			if key.Kind != yaml.ScalarNode {
				continue
			}
			if hint, ok := bannedHint(key.Value); ok {
				add(key.Value, "component "+key.Value+" cannot be used in a host-platform sim build", hint)
			}
			for _, entry := range platformEntries(val) {
				if hint, ok := bannedHint(entry); ok {
					add(entry, key.Value+" platform "+entry+" cannot be used in a host-platform sim build", hint)
				}
			}
		}
	}

	// Assertions on the esphome, host and api blocks.
	esphome := mapGet(root, "esphome")
	if name := scalarOf(mapGet(esphome, "name")); name != want.DeviceName {
		add("esphome.name", fmt.Sprintf("esphome.name is %s but sil injects %s", shown(name), want.DeviceName),
			"derive the name from the sil_name substitution: name: sil-${sil_name}")
	}
	if !hasStatic(mapGet(esphome, "build_flags")) {
		add("esphome.build_flags", "esphome.build_flags does not contain -static",
			"add build_flags: [-static] so the program runs on hosts with an older libc (BUILD-02)")
	}
	mac := scalarOf(mapGet(mapGet(root, "host"), "mac_address"))
	if !strings.EqualFold(mac, want.MAC) {
		add("host.mac_address", fmt.Sprintf("host.mac_address is %s but sil injects %s", shown(mac), want.MAC),
			"use host: mac_address: ${sil_mac}")
	}
	api := mapGet(root, "api")
	if port := scalarOf(mapGet(api, "port")); port != strconv.Itoa(want.Port) {
		add("api.port", fmt.Sprintf("api.port is %s but sil injects %d", shown(port), want.Port),
			"use api: port: ${sil_port}")
	}
	if key := scalarOf(mapGet(mapGet(api, "encryption"), "key")); key == "" {
		add("api.encryption", "api.encryption.key is missing",
			"use api: encryption: key: !env_var SIL_API_KEY; sil injects the key")
	}
	rt := mapGet(api, "reboot_timeout")
	switch zero, ok := zeroPeriod(scalarOf(rt)); {
	case rt == nil:
		add("api.reboot_timeout", "api.reboot_timeout is missing (ESPHome defaults to 15min and would reboot the firmware when no client connects)",
			"set reboot_timeout: 0s")
	case !ok:
		add("api.reboot_timeout", "api.reboot_timeout is "+shown(scalarOf(rt))+", which is not a time period",
			"set reboot_timeout: 0s")
	case !zero:
		add("api.reboot_timeout", "api.reboot_timeout is "+scalarOf(rt)+"; the firmware would reboot when no client is connected",
			"set reboot_timeout: 0s")
	}

	return rejection(ps, configPath)
}

// LintStorage is stage 2: loaded_integrations and loaded_platforms from the
// ESPHome storage JSON show what other components auto-loaded, which stage 1
// cannot see.
func LintStorage(storageJSON []byte, configPath string) error {
	var st struct {
		Integrations []string `json:"loaded_integrations"`
		Platforms    []string `json:"loaded_platforms"`
	}
	if err := json.Unmarshal(storageJSON, &st); err != nil {
		return apperr.Infra("build_failed", "cannot parse the ESPHome storage JSON: "+err.Error(), map[string]any{"file": configPath})
	}
	var ps []Problem
	seen := map[string]bool{}
	add := func(component, msg, hint string) {
		if seen[component] {
			return
		}
		seen[component] = true
		ps = append(ps, Problem{Component: component, File: configPath, Message: msg, Hint: hint})
	}
	for _, name := range st.Integrations {
		if hint, ok := bannedHint(name); ok {
			add(name, "component "+name+" is loaded, directly or auto-loaded by another component, and cannot be used in a host-platform sim build", hint)
		}
	}
	for _, p := range st.Platforms {
		_, platform, found := strings.Cut(p, "/")
		if !found {
			platform = p
		}
		if hint, ok := bannedHint(platform); ok {
			add(platform, "platform "+p+" is loaded, directly or auto-loaded by another component, and cannot be used in a host-platform sim build", hint)
		}
	}
	return rejection(ps, configPath)
}

// rejection turns the collected problems into the lint_rejected error, or nil.
func rejection(ps []Problem, configPath string) error {
	if len(ps) == 0 {
		return nil
	}
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = p.Component + ": " + p.Message
		if p.Hint != "" {
			parts[i] += " (" + p.Hint + ")"
		}
	}
	msg := fmt.Sprintf("%s: %d problem(s): %s", configPath, len(ps), strings.Join(parts, "; "))
	return apperr.Failed("lint_rejected", msg, map[string]any{"file": configPath, "problems": ps})
}

// trimPreamble drops everything before the first top-level YAML key. The
// image entrypoint prints "Creating cache directory ..." on stdout the first
// time it sees a fresh /cache mount.
func trimPreamble(out []byte) []byte {
	loc := topLevelKey.FindIndex(out)
	if loc == nil {
		return out
	}
	return out[loc[0]:]
}

var topLevelKey = regexp.MustCompile(`(?m)^[A-Za-z_][A-Za-z0-9_]*:`)

// platformEntries returns the platform: scalars of a top-level value, which is
// either a list of mappings (the usual shape) or one mapping.
func platformEntries(v *yaml.Node) []string {
	var out []string
	one := func(n *yaml.Node) {
		if p := mapGet(n, "platform"); p != nil && p.Kind == yaml.ScalarNode {
			out = append(out, p.Value)
		}
	}
	switch v.Kind {
	case yaml.SequenceNode:
		for _, item := range v.Content {
			one(item)
		}
	case yaml.MappingNode:
		one(v)
	}
	return out
}

// mapGet returns the value node of key in a mapping node, or nil.
func mapGet(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Kind == yaml.ScalarNode && n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func scalarOf(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func shown(s string) string {
	if s == "" {
		return "missing"
	}
	return strconv.Quote(s)
}

// hasStatic reports whether build_flags (a list, or one scalar) holds -static.
func hasStatic(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == yaml.ScalarNode {
		return n.Value == "-static"
	}
	for _, c := range n.Content {
		if c.Kind == yaml.ScalarNode && c.Value == "-static" {
			return true
		}
	}
	return false
}

var periodRE = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*(us|µs|ms|s|sec|min|h|d)?$`)

// zeroPeriod parses an ESPHome time period (0s, 0ms, 0min, plain 0, ...) and
// reports whether it is zero. ok is false when s is not a time period.
func zeroPeriod(s string) (zero, ok bool) {
	m := periodRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return false, false
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return false, false
	}
	return f == 0, true
}
