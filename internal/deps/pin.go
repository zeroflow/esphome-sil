//go:build sil_pin

// Package deps keeps modules pinned in go.mod before the packages that use
// them exist. Each blank import is deleted when the real import lands (kong
// in 01-03, go.yaml.in/yaml/v3 in 01-08, flynn/noise in 01-09); 01-13 removes
// this file. The sil_pin tag keeps it out of every normal build.
package deps

import (
	_ "github.com/alecthomas/kong"
	_ "github.com/flynn/noise"
	_ "go.yaml.in/yaml/v3"
)
