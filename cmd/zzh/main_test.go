package main

import (
	"reflect"
	"strings"
	"testing"
)

// Every reserved word must be dispatched to its handler and never be taken
// as a host name (D-008).
func TestReservedWordsAreSubcommands(t *testing.T) {
	for _, name := range []string{"ls", "add", "edit", "rm", "alias", "key", "sync", "unlock", "lock", "status", "daemon", "init", "db", "doctor", "config", "version", "help"} {
		if _, found := subcommands[name]; !found {
			t.Errorf("%q is not registered as a subcommand", name)
		}
	}
}

// An invalid ssh option must be rejected before any config or password
// prompt is involved.
func TestConnectValidatesArgumentsFirst(t *testing.T) {
	err := run([]string{"-Z", "web01"})
	if err == nil || !strings.Contains(err.Error(), "unknown ssh option -Z") {
		t.Fatalf("expected an argument error, got %v", err)
	}
}

func TestParseGlobalOptions(t *testing.T) {
	cases := []struct {
		args     []string
		database string
		rest     []string
	}{
		{[]string{"web01"}, "", []string{"web01"}},
		{[]string{"--db", "work", "web01", "-v"}, "work", []string{"web01", "-v"}},
		{[]string{"--db=work", "ls"}, "work", []string{"ls"}},
		// After the host, "--db" belongs to the remote command.
		{[]string{"web01", "mysql", "--db", "x"}, "", []string{"web01", "mysql", "--db", "x"}},
	}

	for _, tc := range cases {
		options, rest, err := parseGlobalOptions(tc.args)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.args, err)
			continue
		}
		if options.database != tc.database || !reflect.DeepEqual(rest, tc.rest) {
			t.Errorf("%q: got database=%q rest=%q", tc.args, options.database, rest)
		}
	}

	for _, args := range [][]string{{"--db"}, {"--db="}} {
		if _, _, err := parseGlobalOptions(args); err == nil {
			t.Errorf("%q: expected an error", args)
		}
	}
}

func TestDatabaseFlagRejectedWhereMeaningless(t *testing.T) {
	err := run([]string{"--db", "work", "db", "ls"})
	if err == nil || !strings.Contains(err.Error(), "cannot be used") {
		t.Fatalf("expected --db to be rejected for \"db\", got %v", err)
	}
}

// ssh runs zzh with the question as only argument; zzh must recognize this
// only when the askpass variables are present, and never for a subcommand.
func TestIsAskpassInvocation(t *testing.T) {
	question := []string{"max@dd.lan's password: "}

	t.Setenv("ZZH_ASKPASS_ENDPOINT", "")
	if isAskpassInvocation(question) {
		t.Fatalf("without the askpass variables it is a normal command line")
	}

	t.Setenv("ZZH_ASKPASS_ENDPOINT", `\.\pipe\zzh-askpass-test`)
	if !isAskpassInvocation(question) {
		t.Fatalf("a single question with the askpass variables is an askpass call")
	}
	if isAskpassInvocation([]string{"ls"}) {
		t.Fatalf("a subcommand is never an askpass call")
	}
	if isAskpassInvocation([]string{"web01", "uptime"}) {
		t.Fatalf("several arguments are never an askpass call")
	}
}

// The help ends with the author credits.
func TestHelpShowsCredits(t *testing.T) {
	if !strings.Contains(usage, "Developed by Massimiliano Dal Cero - DRIFT Linux") ||
		!strings.Contains(usage, "https://www.linkedin.com/in/dalcero/") {
		t.Fatalf("the help must end with the credits")
	}
}
