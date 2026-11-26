package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"zzh/internal/core/config"
	"zzh/internal/core/gui"
	"zzh/internal/core/shim"
)

// checkShims reports whether the ssh found first in PATH is zzh (the shim
// of "zzh install --shim"), whether it is turned off, and how programs
// without a terminal are confirmed. These are information lines: having no
// shim, or no dialog, is a valid setup, never a failure.
func checkShims(r *reporter, cfg config.Config) {
	first, err := exec.LookPath("ssh")
	switch {
	case err != nil:
		r.check(true, "shims: none (no ssh in PATH)")
	case shimTarget(first) != "":
		state := "active"
		switch {
		case shim.DisabledByEnvironment():
			state = "turned off by " + shim.EnvShim
		case !cfg.Shim.Enabled:
			state = "turned off by shim.enabled"
		}
		target := shimTarget(first)
		if !isZzh(target) {
			// Typical when doctor runs from a downloaded or freshly built
			// zzh while the installed one is older: worth knowing, since
			// programs run the installed one.
			state += ", runs " + target + ", not this zzh"
		}
		r.check(true, "shims: %s is a zzh shim (%s)", first, state)
	default:
		r.check(true, "shims: not installed, ssh in PATH is %s (\"zzh install --shim\" to make programs use zzh)", first)
	}

	dialog := "available"
	if !gui.Available() {
		dialog = "not available in this session"
	}
	r.check(true, "confirm: mode %s, remember for %s, graphical dialog %s",
		cfg.Confirm.Mode, cfg.Confirm.RememberFor.Std(), dialog)
}

// shimTarget returns the zzh that path runs when path is a zzh shim: the
// running zzh itself, or a link whose target is named zzh. "" otherwise.
func shimTarget(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	if isZzh(resolved) {
		return resolved
	}
	name := strings.ToLower(filepath.Base(resolved))
	if name == "zzh" || name == "zzh.exe" {
		return resolved
	}
	return ""
}

// isZzh reports whether path is the running zzh, following links.
func isZzh(path string) bool {
	self, err := shim.Executable()
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	a, errA := os.Stat(resolved)
	b, errB := os.Stat(self)
	return errA == nil && errB == nil && os.SameFile(a, b)
}
