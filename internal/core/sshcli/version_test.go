package sshcli

import "testing"

func TestParseVersion(t *testing.T) {
	cases := map[string]Version{
		"OpenSSH_for_Windows_9.5p2, LibreSSL 3.8.2":           {Major: 9, Minor: 5},
		"OpenSSH_9.6p1 Ubuntu-3ubuntu13.5, OpenSSL 3.0.13":    {Major: 9, Minor: 6},
		"OpenSSH_9.8p1, LibreSSL 3.3.6":                       {Major: 9, Minor: 8},
		"OpenSSH_for_Windows_8.1p1, LibreSSL 3.0.2\r\n":       {Major: 8, Minor: 1},
		"OpenSSH_10.0p2 Debian-5, OpenSSL 3.5.1 1 Jul 2025\n": {Major: 10, Minor: 0},
	}

	for banner, expected := range cases {
		got, err := ParseVersion(banner)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", banner, err)
			continue
		}
		if got.Major != expected.Major || got.Minor != expected.Minor {
			t.Errorf("%q: expected %s, got %s", banner, expected, got)
		}
	}
}

func TestParseVersionRejectsOtherClients(t *testing.T) {
	if _, err := ParseVersion("PuTTY Release 0.81"); err == nil {
		t.Fatalf("a non-OpenSSH client must be rejected")
	}
}

func TestAtLeast(t *testing.T) {
	minimum := Version{Major: 8, Minor: 5}

	if !(Version{Major: 9, Minor: 0}).AtLeast(minimum) {
		t.Errorf("9.0 must satisfy 8.5")
	}
	if !(Version{Major: 8, Minor: 5}).AtLeast(minimum) {
		t.Errorf("8.5 must satisfy 8.5")
	}
	if (Version{Major: 8, Minor: 1}).AtLeast(minimum) {
		t.Errorf("8.1 must not satisfy 8.5")
	}
}
