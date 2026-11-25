package sshcli

import (
	"reflect"
	"testing"
)

func TestParseInvocation(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		target  string
		user    string
		options []string
		command []string
	}{
		{
			name:   "plain host",
			args:   []string{"web01"},
			target: "web01",
		},
		{
			name:    "forwards after host",
			args:    []string{"web01", "-L", "8080:localhost:80", "-D", "1080", "-R", "9000:localhost:9000"},
			target:  "web01",
			options: []string{"-L", "8080:localhost:80", "-D", "1080", "-R", "9000:localhost:9000"},
		},
		{
			name:    "options before host",
			args:    []string{"-v", "-p", "2222", "web01"},
			target:  "web01",
			options: []string{"-v", "-p", "2222"},
		},
		{
			name:    "grouped switches with attached value",
			args:    []string{"-fNL8080:db:5432", "bastion"},
			target:  "bastion",
			options: []string{"-f", "-N", "-L", "8080:db:5432"},
		},
		{
			name:    "remote command keeps its own dashes",
			args:    []string{"web01", "-t", "ls", "-la", "/tmp"},
			target:  "web01",
			options: []string{"-t"},
			command: []string{"ls", "-la", "/tmp"},
		},
		{
			name:    "double dash ends options",
			args:    []string{"web01", "--", "-weird-command"},
			target:  "web01",
			command: []string{"-weird-command"},
		},
		{
			name:   "user override",
			args:   []string{"root@web01"},
			target: "web01",
			user:   "root",
		},
		{
			name:   "user containing at sign",
			args:   []string{"max@corp.local@web01"},
			target: "web01",
			user:   "max@corp.local",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv, err := ParseInvocation(tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if inv.Target != tc.target {
				t.Errorf("target: expected %q, got %q", tc.target, inv.Target)
			}
			if inv.User != tc.user {
				t.Errorf("user: expected %q, got %q", tc.user, inv.User)
			}
			if !reflect.DeepEqual(inv.Options, tc.options) {
				t.Errorf("options: expected %q, got %q", tc.options, inv.Options)
			}
			if !reflect.DeepEqual(inv.Command, tc.command) {
				t.Errorf("command: expected %q, got %q", tc.command, inv.Command)
			}
		})
	}
}

func TestParseInvocationTracksExplicitSettings(t *testing.T) {
	inv, err := ParseInvocation([]string{"-p2222", "-o", "ServerAliveInterval=30", "-o", "User root", "web01"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !inv.HasOption('p') {
		t.Errorf("-p should be reported as used")
	}
	if inv.HasOption('l') {
		t.Errorf("-l was not used")
	}
	if !inv.HasConfigKey("serveraliveinterval") || !inv.HasConfigKey("USER") {
		t.Errorf("-o keys should be tracked case-insensitively")
	}
	if inv.HasConfigKey("Port") {
		t.Errorf("Port was not set through -o")
	}
}

func TestParseInvocationErrors(t *testing.T) {
	cases := map[string][]string{
		"no host":         {"-v"},
		"empty":           {},
		"missing value":   {"web01", "-L"},
		"unknown option":  {"-Z", "web01"},
		"unknown grouped": {"-vZ", "web01"},
	}

	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseInvocation(args); err == nil {
				t.Fatalf("expected an error for %q", args)
			}
		})
	}
}
