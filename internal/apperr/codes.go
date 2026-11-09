package apperr

// Registry lists every error code the sil CLI and its libraries emit, with its exit class
// (ExitFailed = 1 user-fixable, ExitInfra = 2 infrastructure). Success is the only exit 0.
//
// Consumers (the product repo pytest, agents) branch on the code and the exit class, so this is
// the single list: TestRegistryMatchesCallSites scans internal/ and cmd/ and fails when a call
// site uses an unregistered code, a different class, both classes, a non-literal code, or when
// a registered code is no longer used.
var Registry = map[string]int{
	// Exit 1: the user can fix it (bad name or argument, bad config, wrong state).
	"already_running":        ExitFailed,
	"button_busy":            ExitFailed,
	"button_not_held":        ExitFailed,
	"compile_failed":         ExitFailed,
	"config_invalid":         ExitFailed,
	"config_not_found":       ExitFailed,
	"fan_toml_invalid":       ExitFailed,
	"firmware_crash_loop":    ExitFailed,
	"firmware_not_connected": ExitFailed,
	"firmware_not_running":   ExitFailed,
	"instance_ambiguous":     ExitFailed,
	"input_not_available":    ExitFailed,
	"instance_busy":          ExitFailed,
	"invalid_argument":       ExitFailed,
	"invalid_name":           ExitFailed,
	"lint_rejected":          ExitFailed,
	"not_running":            ExitFailed,
	"port_claimed":           ExitFailed,
	"stopping":               ExitFailed,

	// Exit 2: infrastructure (docker, ports, sockets, files, internal failures).
	"api_identity_mismatch":  ExitInfra,
	"build_failed":           ExitInfra,
	"control_error":          ExitInfra,
	"control_listen_failed":  ExitInfra,
	"control_unreachable":    ExitInfra,
	"docker_unavailable":     ExitInfra,
	"engine_failed":          ExitInfra,
	"firmware_failed":        ExitInfra,
	"firmware_start_failed":  ExitInfra,
	"handshake_rejected":     ExitInfra,
	"identity_corrupt":       ExitInfra,
	"identity_failed":        ExitInfra,
	"image_missing":          ExitInfra,
	"internal":               ExitInfra,
	"lock_failed":            ExitInfra,
	"lock_timeout":           ExitInfra,
	"no_free_port":           ExitInfra,
	"no_home":                ExitInfra,
	"not_static":             ExitInfra,
	"plaintext_api":          ExitInfra,
	"port_bind_failed":       ExitInfra,
	"port_in_use":            ExitInfra,
	"ready_timeout":          ExitInfra,
	"resync_timeout":         ExitInfra,
	"run_dir_failed":         ExitInfra,
	"run_file_failed":        ExitInfra,
	"run_file_invalid":       ExitInfra,
	"run_file_unreadable":    ExitInfra,
	"runtime_dir_unreadable": ExitInfra,
	"serve_exited":           ExitInfra,
	"serve_start_failed":     ExitInfra,
	"socket_path_too_long":   ExitInfra,
	"spec_invalid":           ExitInfra,
	"spec_unreadable":        ExitInfra,
	"stop_timeout":           ExitInfra,
	"supervisor_panic":       ExitInfra,
	"unsafe_runtime_dir":     ExitInfra,
}
