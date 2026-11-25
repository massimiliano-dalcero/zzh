package cliflags

import (
	"reflect"
	"testing"
)

var spec = Spec{Values: []string{"user", "tag"}, Switches: []string{"password"}}

func TestParseMixedArguments(t *testing.T) {
	result, err := Parse([]string{"web02", "--user", "deploy", "10.0.0.2", "--tag=prod", "--password", "--tag", "web"}, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(result.Positional, []string{"web02", "10.0.0.2"}) {
		t.Errorf("positional: %q", result.Positional)
	}
	if result.Value("user") != "deploy" {
		t.Errorf("user: %q", result.Value("user"))
	}
	if !reflect.DeepEqual(result.Values("tag"), []string{"prod", "web"}) {
		t.Errorf("tags: %q", result.Values("tag"))
	}
	if !result.Switch("password") {
		t.Errorf("password switch not set")
	}
	if result.Value("missing") != "" || result.Switch("other") {
		t.Errorf("absent options must be empty")
	}
}

func TestParseLastValueWins(t *testing.T) {
	result, err := Parse([]string{"--user", "a", "--user", "b"}, spec)
	if err != nil || result.Value("user") != "b" {
		t.Fatalf("expected the last value, got %q (%v)", result.Value("user"), err)
	}
}

func TestParseDoubleDash(t *testing.T) {
	result, err := Parse([]string{"name", "--", "--not-an-option"}, spec)
	if err != nil || !reflect.DeepEqual(result.Positional, []string{"name", "--not-an-option"}) {
		t.Fatalf("unexpected result %q (%v)", result.Positional, err)
	}
}

func TestParseErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--unknown"},
		{"--user"},
		{"--password=yes"},
	} {
		if _, err := Parse(args, spec); err == nil {
			t.Errorf("%q: expected an error", args)
		}
	}
}

func TestHasDistinguishesEmptyFromAbsent(t *testing.T) {
	result, err := Parse([]string{"--user="}, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Has("user") || result.Value("user") != "" {
		t.Fatalf("an empty value must still count as given")
	}
	if result.Has("tag") {
		t.Fatalf("an option not given must not be reported")
	}
}
