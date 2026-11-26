package hosts

import (
	"bytes"
	"strings"
	"testing"

	"zzh/internal/core/style"
	"zzh/internal/core/vault"
)

var sample = []vault.Host{
	{Name: "bastion", Address: "bastion.example.com", Port: 2222, User: "jump", Tags: []string{"Prod"}},
	{Name: "broken", Folder: "prod", Problem: "the URL field is empty"},
	{Name: "web01", Folder: "prod", Address: "10.0.0.1", User: "deploy", Tags: []string{"prod", "web"}},
	{Name: "lab1", Folder: "lab", Address: "lab1.local", KeyID: "AA", KeyName: "laptop"},
	{Name: "lab2", Folder: "lab", Address: "lab2.local", KeyID: "BB"},
}

func TestListShowsKeys(t *testing.T) {
	var out bytes.Buffer
	if err := Run(nil, sample, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "laptop") || !strings.Contains(out.String(), "MISSING KEY") {
		t.Fatalf("keys not shown correctly:\n%s", out.String())
	}
}

func TestListShowsAllHostsAndProblems(t *testing.T) {
	var out bytes.Buffer
	if err := Run(nil, sample, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := out.String()
	for _, fragment := range []string{"bastion.example.com:2222", "INVALID: the URL field is empty", "prod,web", "lab1.local"} {
		if !strings.Contains(text, fragment) {
			t.Errorf("missing %q in:\n%s", fragment, text)
		}
	}
}

func TestListFiltersByTags(t *testing.T) {
	var out bytes.Buffer
	if err := Run([]string{"--tag", "prod", "--tag", "WEB"}, sample, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "web01") {
		t.Errorf("web01 has both tags and must be listed:\n%s", text)
	}
	if strings.Contains(text, "bastion") || strings.Contains(text, "lab1") {
		t.Errorf("hosts without all tags must be hidden:\n%s", text)
	}
}

func TestListRejectsBadArguments(t *testing.T) {
	var out bytes.Buffer
	if err := Run([]string{"--tag"}, sample, &out); err == nil {
		t.Errorf("missing tag value must fail")
	}
	if err := Run([]string{"-a"}, sample, &out); err == nil {
		t.Errorf("unknown argument must fail")
	}
}

// The listing must say that a password exists without revealing it.
func TestListShowsPasswordPresenceOnly(t *testing.T) {
	var out bytes.Buffer
	hostList := []vault.Host{
		{Name: "with", Address: "a.lan", Password: "s3cret-value"},
		{Name: "without", Address: "b.lan"},
	}
	if err := Run(nil, hostList, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := out.String()
	if strings.Contains(text, "s3cret-value") {
		t.Fatalf("the password must never be printed:\n%s", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "with ") && !strings.Contains(line, "stored") {
			t.Fatalf("a stored password must be flagged:\n%s", text)
		}
		if strings.HasPrefix(line, "without ") && strings.Contains(line, "stored") {
			t.Fatalf("no flag without a password:\n%s", text)
		}
	}
}

// The plain layout (pipes, --grep): one tab-separated line per host, every
// column present, "-" for empty fields, no header.
func TestListGrepLayout(t *testing.T) {
	var out bytes.Buffer
	hostList := []vault.Host{{Name: "web01", Folder: "prod", Address: "10.0.0.1", Port: 22, User: "deploy",
		Password: "x", Tags: []string{"prod", "web"}}}
	if err := Run([]string{"--grep"}, hostList, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "web01\t-\tprod\tdeploy\t10.0.0.1:22\t-\tstored\tprod,web\n"
	if out.String() != expected {
		t.Fatalf("got %q, expected %q", out.String(), expected)
	}
}

// On a terminal "stored" must stand out: it is painted as a stored secret.
func TestListHighlightsStoredPassword(t *testing.T) {
	s := style.Forced(true)
	hostList := []vault.Host{{Name: "with", Address: "a.lan", Password: "x"}}

	var out bytes.Buffer
	if err := buildTable(hostList, nil, s).RenderBoxedForTest(&out, s); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if !strings.Contains(out.String(), s.Stored("stored")) {
		t.Fatalf("stored password not highlighted:\n%q", out.String())
	}
}
