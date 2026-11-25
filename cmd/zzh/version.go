package main

import (
	"runtime/debug"
)

// releaseVersion is the single place where the zzh version is defined.
// Change it when preparing a release, and tag the release commit with the
// same value prefixed by "v" (git tag -a v0.9.0).
const releaseVersion = "0.9.0"

// fullVersion is the version shown by "zzh version" and by the help.
//
// A release binary is built without version control information and shows
// exactly "v0.9.0". A binary built from a git checkout (plain "go build")
// also carries the commit it was built from, which Go embeds automatically:
// "v0.9.0+e9aa47a", and ".dirty" when there were uncommitted changes. The
// "+" part is build metadata in semantic versioning terms: it tells two
// builds of the same version apart without pretending to be another release.
func fullVersion() string {
	version := "v" + releaseVersion

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	return version + buildMetadata(info.Settings)
}

// buildMetadata extracts "+<commit>[.dirty]" from the build settings, or
// returns "" when the binary was built without version control information.
func buildMetadata(settings []debug.BuildSetting) string {
	revision, modified := "", false
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}

	if revision == "" {
		return ""
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}

	metadata := "+" + revision
	if modified {
		metadata += ".dirty"
	}
	return metadata
}
