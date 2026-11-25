package vault

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
	"github.com/tobischo/gokeepasslib/v3/wrappers"
)

const testPassword = "correct horse battery staple"

// entry builds a KeePass entry with the given fields; "Password" is stored
// as a protected value, as KeePassXC does.
func entry(fields map[string]string) gokeepasslib.Entry {
	e := gokeepasslib.NewEntry()
	for key, value := range fields {
		data := gokeepasslib.ValueData{Key: key, Value: gokeepasslib.V{Content: value}}
		if key == fieldPassword {
			data.Value.Protected = wrappers.NewBoolWrapper(true)
		}
		e.Values = append(e.Values, data)
	}
	return e
}

func group(name string, entries []gokeepasslib.Entry, children ...gokeepasslib.Group) gokeepasslib.Group {
	g := gokeepasslib.NewGroup()
	g.Name = name
	g.Entries = entries
	g.Groups = children
	return g
}

// writeDatabase saves a KDBX 4 file (the format KeePassXC creates by
// default) whose top-level group contains the given groups.
func writeDatabase(t *testing.T, topLevel ...gokeepasslib.Group) string {
	t.Helper()

	db := gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion4())
	db.Credentials = gokeepasslib.NewPasswordCredentials(testPassword)
	db.Content.Root = &gokeepasslib.RootData{
		Groups: []gokeepasslib.Group{group("Root", nil, topLevel...)},
	}

	if err := db.LockProtectedEntries(); err != nil {
		t.Fatalf("cannot lock entries: %v", err)
	}

	path := filepath.Join(t.TempDir(), "test.kdbx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("cannot create database file: %v", err)
	}
	defer file.Close()

	if err := gokeepasslib.NewEncoder(file).Encode(db); err != nil {
		t.Fatalf("cannot encode database: %v", err)
	}
	return path
}

// standardDatabase contains a realistic layout: hosts in nested folders, a
// duplicate name, an invalid entry and unrelated groups that must be ignored.
func standardDatabase(t *testing.T) string {
	return writeDatabase(t,
		group("Email", []gokeepasslib.Entry{
			entry(map[string]string{fieldTitle: "web01", fieldURL: "https://mail.example.com"}),
		}),
		group("SSH", nil,
			group("Keys", nil),
			group("Hosts",
				[]gokeepasslib.Entry{
					entry(map[string]string{fieldTitle: "bastion", fieldURL: "ssh://bastion.example.com:2222", fieldUserName: "jump"}),
				},
				group("prod", []gokeepasslib.Entry{
					entry(map[string]string{
						fieldTitle:    "web01",
						fieldURL:      "10.0.0.1",
						fieldUserName: "deploy",
						fieldPassword: "s3cret",
						fieldOptions:  "# keep the session alive\nServerAliveInterval=30\n\nCompression=yes",
					}),
					entry(map[string]string{fieldTitle: "broken", fieldURL: "https://wrong.example.com"}),
				}, group("eu", []gokeepasslib.Entry{
					entry(map[string]string{fieldTitle: "db01", fieldURL: "ssh://admin@db01.eu:22"}),
				})),
				group("lab", []gokeepasslib.Entry{
					entry(map[string]string{fieldTitle: "twin", fieldURL: "twin-a"}),
				}),
				group("old", []gokeepasslib.Entry{
					entry(map[string]string{fieldTitle: "twin", fieldURL: "twin-b"}),
				}),
			),
		),
	)
}

func TestOpenWrongPassword(t *testing.T) {
	path := standardDatabase(t)

	_, err := Open(path, "wrong", "SSH")
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got %v", err)
	}
}

func TestHostsWalksNestedFolders(t *testing.T) {
	v, err := Open(standardDatabase(t), testPassword, "SSH")
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}

	hosts, err := v.Hosts()
	if err != nil {
		t.Fatalf("hosts failed: %v", err)
	}

	var paths []string
	for _, host := range hosts {
		paths = append(paths, host.Path())
	}

	// Sorted by name, then folder; the "Email" group must not leak in.
	expected := []string{"bastion", "prod/broken", "prod/eu/db01", "lab/twin", "old/twin", "prod/web01"}
	if !reflect.DeepEqual(paths, expected) {
		t.Fatalf("expected %v, got %v", expected, paths)
	}
}

func TestFindHost(t *testing.T) {
	v, err := Open(standardDatabase(t), testPassword, "SSH")
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}

	web, err := v.FindHost("web01")
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if web.Address != "10.0.0.1" || web.User != "deploy" || web.Port != 0 {
		t.Fatalf("unexpected host data: %+v", web)
	}
	// Protected fields must be readable after opening.
	if web.Password != "s3cret" {
		t.Fatalf("password not unlocked: %q", web.Password)
	}
	if !reflect.DeepEqual(web.Options, []string{"ServerAliveInterval=30", "Compression=yes"}) {
		t.Fatalf("unexpected options: %q", web.Options)
	}

	db, err := v.FindHost("db01")
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	// The user embedded in the URL is used when UserName is empty.
	if db.User != "admin" || db.Port != 22 {
		t.Fatalf("unexpected host data: %+v", db)
	}
}

func TestFindHostErrors(t *testing.T) {
	v, err := Open(standardDatabase(t), testPassword, "SSH")
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}

	cases := map[string]string{
		"missing":   "not found",
		"twin":      "more than one entry",
		"broken":    "scheme must be ssh",
		"web01 ":    "not found",
		"prod/web1": "not found",
	}

	for name, fragment := range cases {
		_, err := v.FindHost(name)
		if err == nil || !strings.Contains(err.Error(), fragment) {
			t.Errorf("%q: expected error containing %q, got %v", name, fragment, err)
		}
	}
}

func TestMissingGroups(t *testing.T) {
	noRoot := writeDatabase(t, group("Other", nil))
	v, err := Open(noRoot, testPassword, "SSH")
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	if _, err := v.Hosts(); err == nil || !strings.Contains(err.Error(), `"SSH" not found`) {
		t.Fatalf("expected missing root group error, got %v", err)
	}

	noHosts := writeDatabase(t, group("SSH", nil, group("Keys", nil)))
	v, err = Open(noHosts, testPassword, "SSH")
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	if _, err := v.Hosts(); err == nil || !strings.Contains(err.Error(), `"Hosts" not found`) {
		t.Fatalf("expected missing Hosts group error, got %v", err)
	}
}

func TestFindHostByAddress(t *testing.T) {
	v, err := Open(standardDatabase(t), testPassword, "SSH")
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}

	host, err := v.FindHostByAddress("10.0.0.1")
	if err != nil || host.Name != "web01" {
		t.Fatalf("expected web01, got %+v (%v)", host, err)
	}
	// DNS names are case-insensitive.
	host, err = v.FindHostByAddress("BASTION.example.com")
	if err != nil || host.Name != "bastion" {
		t.Fatalf("expected bastion, got %+v (%v)", host, err)
	}

	if _, err := v.FindHostByAddress("192.168.3.3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := v.FindHost("192.168.3.3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindHost must also report ErrNotFound, got %v", err)
	}
}

func TestFindHostByAddressAmbiguous(t *testing.T) {
	path := writeDatabase(t, group("SSH", nil, group("Keys", nil), group("Hosts", []gokeepasslib.Entry{
		entry(map[string]string{fieldTitle: "a", fieldURL: "shared.lan"}),
		entry(map[string]string{fieldTitle: "b", fieldURL: "shared.lan:2222"}),
	})))
	v, err := Open(path, testPassword, "SSH")
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}

	_, err = v.FindHostByAddress("shared.lan")
	if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "a, b") {
		t.Fatalf("expected an ambiguity error naming both hosts, got %v", err)
	}
}
