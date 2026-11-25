package vault

import (
	"reflect"
	"testing"
)

func TestParseAddress(t *testing.T) {
	cases := []struct {
		raw     string
		address string
		port    int
		user    string
	}{
		{"ssh://web01.example.com", "web01.example.com", 0, ""},
		{"ssh://web01.example.com:2222", "web01.example.com", 2222, ""},
		{"ssh://root@web01:22", "web01", 22, "root"},
		{"web01", "web01", 0, ""},
		{"web01:2222", "web01", 2222, ""},
		{"deploy@10.0.0.5", "10.0.0.5", 0, "deploy"},
		{"  ssh://spaced  ", "spaced", 0, ""},
		{"ssh://[2001:db8::1]:2222", "2001:db8::1", 2222, ""},
		{"2001:db8::1", "2001:db8::1", 0, ""},
	}

	for _, tc := range cases {
		address, port, user, err := ParseAddress(tc.raw)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.raw, err)
			continue
		}
		if address != tc.address || port != tc.port || user != tc.user {
			t.Errorf("%q: expected (%q, %d, %q), got (%q, %d, %q)",
				tc.raw, tc.address, tc.port, tc.user, address, port, user)
		}
	}
}

func TestParseAddressErrors(t *testing.T) {
	for _, raw := range []string{
		"",
		"https://web01",
		"ssh://",
		"ssh://web01:0",
		"ssh://web01:70000",
		"ssh://web01:abc",
		"not:an:ipv6",
	} {
		if _, _, _, err := ParseAddress(raw); err == nil {
			t.Errorf("%q: expected an error", raw)
		}
	}
}

func TestParseOptions(t *testing.T) {
	options, err := parseOptions("ServerAliveInterval=30\r\n# comment\n\n  ForwardAgent=no  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(options, []string{"ServerAliveInterval=30", "ForwardAgent=no"}) {
		t.Fatalf("unexpected options: %q", options)
	}

	if _, err := parseOptions("ServerAliveInterval 30"); err == nil {
		t.Fatalf("an option without '=' must be rejected")
	}
}

func TestSplitTags(t *testing.T) {
	tags := splitTags("prod; web ,eu;;")
	if !reflect.DeepEqual(tags, []string{"prod", "web", "eu"}) {
		t.Fatalf("unexpected tags: %q", tags)
	}
}
