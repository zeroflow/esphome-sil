// Package fans embeds the shipped fan library: one TOML file per fan model.
package fans

import "embed"

// FS holds the shipped fan parameter files.
//
//go:embed *.toml
var FS embed.FS
