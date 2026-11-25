package main

import (
	"regexp"
	"runtime/debug"
	"testing"
)

func TestBuildMetadata(t *testing.T) {
	cases := []struct {
		name     string
		settings []debug.BuildSetting
		expected string
	}{
		{"release build, no vcs info", nil, ""},
		{"clean checkout", []debug.BuildSetting{
			{Key: "vcs.revision", Value: "e9aa47a1b2c3d4e5f60718293a4b5c6d7e8f9012"},
			{Key: "vcs.modified", Value: "false"},
		}, "+e9aa47a"},
		{"uncommitted changes", []debug.BuildSetting{
			{Key: "vcs.revision", Value: "e9aa47a1b2c3"},
			{Key: "vcs.modified", Value: "true"},
		}, "+e9aa47a.dirty"},
	}

	for _, tc := range cases {
		if got := buildMetadata(tc.settings); got != tc.expected {
			t.Errorf("%s: expected %q, got %q", tc.name, tc.expected, got)
		}
	}
}

// The version must be a valid semantic version with the "v" prefix used by
// the git tags, possibly followed by build metadata.
func TestFullVersionFormat(t *testing.T) {
	pattern := regexp.MustCompile(`^v\d+\.\d+\.\d+(\+[0-9a-f]{7}(\.dirty)?)?$`)
	if version := fullVersion(); !pattern.MatchString(version) {
		t.Fatalf("unexpected version format %q", version)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(releaseVersion) {
		t.Fatalf("releaseVersion must be MAJOR.MINOR.PATCH without prefix, got %q", releaseVersion)
	}
}
