package install

import (
	"strings"
)

// This file edits the value of a Windows PATH variable as stored in the
// registry: entries separated by ";", possibly containing variables such as
// %SystemRoot% (the value is REG_EXPAND_SZ and is expanded by Windows when a
// process starts). The functions are pure, so they are tested on any OS.

// expander expands %VARIABLES% in a PATH entry; registry.ExpandString on
// Windows, a fake in tests.
type expander func(string) string

// sameEntry compares two PATH entries the way Windows resolves them: after
// expanding variables, ignoring case and a trailing backslash.
func sameEntry(a, b string, expand expander) bool {
	normalize := func(entry string) string {
		return strings.TrimRight(strings.ToLower(expand(strings.TrimSpace(entry))), `\/`)
	}
	return normalize(a) == normalize(b)
}

// splitEntries splits a PATH value, dropping empty entries (";;" or a final
// ";" are common and meaningless).
func splitEntries(value string) []string {
	var entries []string
	for _, entry := range strings.Split(value, ";") {
		if strings.TrimSpace(entry) != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

// appendEntry adds entry at the end of the PATH value, unless it is already
// present. It reports whether the value changed. Used for the folder of zzh
// itself, whose position does not matter.
func appendEntry(value, entry string, expand expander) (string, bool) {
	entries := splitEntries(value)
	for _, existing := range entries {
		if sameEntry(existing, entry, expand) {
			return value, false
		}
	}
	return strings.Join(append(entries, entry), ";"), true
}

// insertBefore puts entry right before the first entry for which isTarget is
// true (the Windows OpenSSH folder), or at the very beginning when there is
// none. If entry is already present after the target, it is moved: what
// matters is that the shim folder wins over OpenSSH. It reports whether the
// value changed.
func insertBefore(value, entry string, isTarget func(string) bool, expand expander) (string, bool) {
	var kept []string
	for _, existing := range splitEntries(value) {
		if !sameEntry(existing, entry, expand) {
			kept = append(kept, existing)
		}
	}

	position := 0
	for i, existing := range kept {
		if isTarget(existing) {
			position = i
			break
		}
	}

	result := make([]string, 0, len(kept)+1)
	result = append(result, kept[:position]...)
	result = append(result, entry)
	result = append(result, kept[position:]...)

	joined := strings.Join(result, ";")
	return joined, joined != strings.Join(splitEntries(value), ";")
}

// removeEntry deletes every occurrence of entry from the PATH value and
// reports whether the value changed.
func removeEntry(value, entry string, expand expander) (string, bool) {
	var kept []string
	changed := false
	for _, existing := range splitEntries(value) {
		if sameEntry(existing, entry, expand) {
			changed = true
			continue
		}
		kept = append(kept, existing)
	}
	if !changed {
		return value, false
	}
	return strings.Join(kept, ";"), true
}
