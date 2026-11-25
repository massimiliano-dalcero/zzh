package sshcli

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

// Version is the OpenSSH release of the installed ssh client.
type Version struct {
	Major int
	Minor int

	// Raw is the full banner printed by "ssh -V", kept for diagnostics.
	Raw string
}

// Feature is an OpenSSH capability zzh relies on, with the first release
// that provides it.
type Feature struct {
	Name     string
	Purpose  string
	Required Version
}

// RequiredFeatures are the ssh capabilities that allow zzh to work without
// ever writing secrets to disk. The oldest of them sets the minimum
// supported OpenSSH version.
var RequiredFeatures = []Feature{
	{
		Name:     "SSH_ASKPASS_REQUIRE=force",
		Purpose:  "passwords are supplied by zzh instead of being typed",
		Required: Version{Major: 8, Minor: 4},
	},
	{
		Name:     "KnownHostsCommand",
		Purpose:  "host keys are read from KeePass instead of known_hosts",
		Required: Version{Major: 8, Minor: 5},
	},
}

// The banner looks like "OpenSSH_9.6p1 Ubuntu-3ubuntu13, OpenSSL ..." on
// Linux, "OpenSSH_9.8p1, LibreSSL 3.3.6" on macOS and
// "OpenSSH_for_Windows_9.5p2, LibreSSL 3.8.2" on Windows.
var versionPattern = regexp.MustCompile(`OpenSSH(?:_for_Windows)?_(\d+)\.(\d+)`)

// ParseVersion extracts the OpenSSH version from the "ssh -V" banner.
func ParseVersion(banner string) (Version, error) {
	match := versionPattern.FindStringSubmatch(banner)
	if match == nil {
		return Version{}, fmt.Errorf("not an OpenSSH client: %q", banner)
	}

	// The regular expression only matches digits, so the conversions
	// cannot fail except for absurdly large numbers.
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return Version{}, fmt.Errorf("invalid major version in %q: %w", banner, err)
	}
	minor, err := strconv.Atoi(match[2])
	if err != nil {
		return Version{}, fmt.Errorf("invalid minor version in %q: %w", banner, err)
	}

	return Version{Major: major, Minor: minor, Raw: banner}, nil
}

// DetectVersion runs "<binary> -V" and parses the result.
// ssh prints the banner on stderr, so both streams are captured.
func DetectVersion(binary string) (Version, error) {
	// A short timeout protects against a wrong "binary" setting pointing to
	// a program that waits for input instead of printing a version.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	output, err := exec.CommandContext(ctx, binary, "-V").CombinedOutput()
	if err != nil {
		return Version{}, fmt.Errorf("cannot run %s -V: %w", binary, err)
	}

	return ParseVersion(string(output))
}

// AtLeast reports whether v is the same as or newer than other.
func (v Version) AtLeast(other Version) bool {
	if v.Major != other.Major {
		return v.Major > other.Major
	}
	return v.Minor >= other.Minor
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}
