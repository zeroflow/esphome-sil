package fwbuild

import (
	"debug/elf"
	"strings"

	"github.com/zeroflow/esphome-sil/internal/apperr"
)

// AssertStatic fails with Infra not_static unless path is an ELF executable
// without an interpreter (PT_INTERP, .interp) and without DT_NEEDED entries.
// A dynamically linked program built in the pinned image does not run on
// hosts with an older libc (BUILD-02).
func AssertStatic(path string) error {
	f, err := elf.Open(path)
	if err != nil {
		return apperr.Infra("not_static", path+" is not a readable ELF program: "+err.Error(), map[string]any{"path": path})
	}
	defer f.Close()

	var reasons []string
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			reasons = append(reasons, "it has a PT_INTERP program header")
			break
		}
	}
	if f.Section(".interp") != nil {
		reasons = append(reasons, "it has an .interp section")
	}
	libs, err := f.ImportedLibraries()
	if err != nil {
		return apperr.Infra("not_static", path+": cannot read the dynamic section: "+err.Error(), map[string]any{"path": path})
	}
	if len(libs) > 0 {
		reasons = append(reasons, "it needs shared libraries "+strings.Join(libs, ", "))
	}
	if len(reasons) > 0 {
		return apperr.Infra("not_static",
			path+" is dynamically linked ("+strings.Join(reasons, "; ")+"); the firmware YAML must set esphome: build_flags: [-static]",
			map[string]any{"path": path, "needed": libs})
	}
	return nil
}
