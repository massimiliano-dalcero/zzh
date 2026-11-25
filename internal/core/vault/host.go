package vault

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/tobischo/gokeepasslib/v3"
)

// Names of the KeePass fields used by zzh entries. The standard fields
// (Title, UserName, Password, URL) are the ones shown in the main KeePassXC
// form; the "zzh." ones are custom attributes in the "Advanced" tab.
const (
	fieldTitle    = "Title"
	fieldUserName = "UserName"
	fieldPassword = "Password"
	fieldURL      = "URL"
	fieldOptions  = "zzh.options"
	fieldKey      = "zzh.key"
	fieldAliases  = "zzh.aliases"
)

// Host is a connection target as stored in KeePass.
type Host struct {
	// Name is the entry title: the word the user types after "zzh".
	Name string

	// Aliases are other words that reach the same entry ("zzh c" for the
	// host "centauri"). Names and aliases share one namespace: each word
	// identifies at most one host (D-031).
	Aliases []string

	// Folder is the path of the entry relative to the Hosts group
	// ("prod/eu"), empty for entries placed directly in Hosts.
	Folder string

	// Address is the network name or IP the ssh client connects to.
	Address string

	// Port is 0 when not specified, meaning "let ssh use its default".
	Port int

	// User is the remote login; empty means "let ssh decide".
	User string

	// Password is the login password, empty when the host uses keys only.
	Password string

	// KeyID is the UUID of the key entry used to log in (zzh.key), empty
	// when no key is associated. KeyName is its title, resolved when the
	// hosts are loaded; it stays empty if the key entry no longer exists.
	KeyID   string
	KeyName string

	// Options are extra ssh_config "Key=Value" settings for this host only.
	Options []string

	// Tags are the KeePass tags of the entry, used for filtering.
	Tags []string

	// Problem describes why the entry cannot be used; empty for valid hosts.
	Problem string
}

// Path returns the folder and name, as shown to the user ("prod/web01").
func (h Host) Path() string {
	return joinPath(h.Folder, h.Name)
}

// Hosts returns all host entries found under <root group>/Hosts, sorted by
// name. Entries with invalid data are included with Host.Problem set, so
// that listing commands can show them to the user instead of silently
// hiding a misconfigured host.
func (v *Vault) Hosts() ([]Host, error) {
	entries, err := v.entriesIn(hostsGroupName)
	if err != nil {
		return nil, err
	}

	keyNames, err := v.keyNamesByID()
	if err != nil {
		return nil, err
	}

	hosts := make([]Host, 0, len(entries))
	for _, located := range entries {
		host := hostFromEntry(located.entry, located.folder)
		host.KeyName = keyNames[host.KeyID]
		hosts = append(hosts, host)
	}

	sort.Slice(hosts, func(i, j int) bool {
		if hosts[i].Name != hosts[j].Name {
			return hosts[i].Name < hosts[j].Name
		}
		return hosts[i].Folder < hosts[j].Folder
	})

	return hosts, nil
}

// FindHost returns the valid host whose name or alias matches exactly.
func (v *Vault) FindHost(name string) (Host, error) {
	located, err := v.findHostEntry(name)
	if err != nil {
		return Host{}, err
	}

	host := hostFromEntry(located.entry, located.folder)
	if host.Problem != "" {
		return Host{}, fmt.Errorf("host %q is not valid: %s", name, host.Problem)
	}

	keyNames, err := v.keyNamesByID()
	if err != nil {
		return Host{}, err
	}
	host.KeyName = keyNames[host.KeyID]

	return host, nil
}

// FindHostByAddress returns the valid host whose address (the host part of
// the URL field) is the given one, so that "zzh 192.168.3.3" works for a
// host stored under a name. Host names are compared case-insensitively, as
// DNS does. It returns ErrNotFound when no host has that address, and an
// error listing the names when several do, since zzh cannot choose.
func (v *Vault) FindHostByAddress(address string) (Host, error) {
	hosts, err := v.Hosts()
	if err != nil {
		return Host{}, err
	}

	var matches []Host
	for _, host := range hosts {
		if host.Problem == "" && strings.EqualFold(host.Address, address) {
			matches = append(matches, host)
		}
	}

	switch len(matches) {
	case 0:
		return Host{}, fmt.Errorf("no host with address %q: %w", address, ErrNotFound)
	case 1:
		return matches[0], nil
	default:
		names := make([]string, 0, len(matches))
		for _, match := range matches {
			names = append(names, match.Name)
		}
		return Host{}, fmt.Errorf("address %q is used by several hosts, use one of their names: %s", address, strings.Join(names, ", "))
	}
}

// findHostEntry returns the single host entry whose title or one of whose
// aliases is name. Several matches (two entries with the same title, or an
// alias equal to another host's name, possibly created in KeePassXC) are an
// error that lists them, because zzh cannot choose.
func (v *Vault) findHostEntry(name string) (locatedEntry, error) {
	entries, err := v.entriesIn(hostsGroupName)
	if err != nil {
		return locatedEntry{}, err
	}

	var matches []locatedEntry
	for _, located := range entries {
		if hostWords(located.entry)[name] {
			matches = append(matches, located)
		}
	}

	switch len(matches) {
	case 0:
		return locatedEntry{}, fmt.Errorf("host %q %w in %s/%s", name, ErrNotFound, v.rootGroup, hostsGroupName)
	case 1:
		return matches[0], nil
	default:
		paths := make([]string, 0, len(matches))
		for _, match := range matches {
			paths = append(paths, joinPath(match.folder, entryTitle(match.entry)))
		}
		return locatedEntry{}, fmt.Errorf("host name or alias %q is used by more than one entry: %v", name, paths)
	}
}

// hostWords returns the words that reach a host entry: its title and its
// aliases.
func hostWords(entry *gokeepasslib.Entry) map[string]bool {
	words := map[string]bool{}
	if title := entryTitle(entry); title != "" {
		words[title] = true
	}
	for _, alias := range parseAliases(entry.GetContent(fieldAliases)) {
		words[alias] = true
	}
	return words
}

// parseAliases reads the zzh.aliases attribute. zzh writes one alias per
// line; spaces and commas are accepted too, since that is what people type
// when editing the attribute by hand in KeePassXC.
func parseAliases(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
}

func entryTitle(entry *gokeepasslib.Entry) string {
	return strings.TrimSpace(entry.GetContent(fieldTitle))
}

// hostFromEntry converts a KeePass entry into a Host. Invalid data does not
// stop the conversion: it is recorded in Problem so that the caller can
// decide whether to show or refuse the host.
func hostFromEntry(entry *gokeepasslib.Entry, folder string) Host {
	host := Host{
		Name:     entryTitle(entry),
		Aliases:  parseAliases(entry.GetContent(fieldAliases)),
		Folder:   folder,
		User:     strings.TrimSpace(entry.GetContent(fieldUserName)),
		Password: entry.GetContent(fieldPassword),
		KeyID:    strings.TrimSpace(entry.GetContent(fieldKey)),
		Tags:     splitTags(entry.Tags),
	}

	if host.Name == "" {
		host.Problem = "the entry has no title"
		return host
	}

	address, port, user, err := ParseAddress(entry.GetContent(fieldURL))
	if err != nil {
		host.Problem = err.Error()
		return host
	}
	host.Address = address
	host.Port = port

	// The UserName field wins; a user embedded in the URL is accepted as a
	// convenience for entries imported from other tools.
	if host.User == "" {
		host.User = user
	}

	options, err := parseOptions(entry.GetContent(fieldOptions))
	if err != nil {
		host.Problem = err.Error()
		return host
	}
	host.Options = options

	return host
}

// ParseAddress reads the URL field of a host entry, and the address given
// to "zzh add".
//
// Accepted forms, so that the user can write what feels natural:
//
//	ssh://host   ssh://host:2222   ssh://user@host:2222   ssh://[2001:db8::1]:22
//	host         host:2222         user@host              2001:db8::1
//
// Any scheme other than ssh is rejected because it usually means the entry
// was meant for something else (e.g. a web login).
func ParseAddress(raw string) (address string, port int, user string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0, "", fmt.Errorf("the URL field is empty (expected ssh://hostname[:port])")
	}

	// A bare IPv6 address contains several colons and no brackets: it cannot
	// carry a port and would confuse the URL parser, so it is taken as is.
	if !strings.Contains(raw, "://") && strings.Count(raw, ":") > 1 && !strings.Contains(raw, "[") {
		if net.ParseIP(raw) == nil {
			return "", 0, "", fmt.Errorf("invalid address %q", raw)
		}
		return raw, 0, "", nil
	}

	if !strings.Contains(raw, "://") {
		raw = "ssh://" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", 0, "", fmt.Errorf("invalid URL %q: %v", raw, err)
	}
	if parsed.Scheme != "ssh" {
		return "", 0, "", fmt.Errorf("URL scheme must be ssh, got %q", parsed.Scheme)
	}

	address = parsed.Hostname()
	if address == "" {
		return "", 0, "", fmt.Errorf("URL %q has no host name", raw)
	}

	if text := parsed.Port(); text != "" {
		port, err = strconv.Atoi(text)
		if err != nil || port < 1 || port > 65535 {
			return "", 0, "", fmt.Errorf("invalid port %q in URL", text)
		}
	}

	if parsed.User != nil {
		user = parsed.User.Username()
	}

	return address, port, user, nil
}

// formatURL builds the value stored in the URL field: always with the ssh
// scheme, so that KeePassXC shows it as an ssh link, and with brackets
// around IPv6 addresses when a port follows.
func formatURL(address string, port int) string {
	host := address
	if port != 0 {
		host = net.JoinHostPort(address, strconv.Itoa(port))
	} else if strings.Contains(address, ":") {
		host = "[" + address + "]"
	}
	return "ssh://" + host
}

// parseOptions reads the zzh.options attribute: one "Key=Value" per line.
// Empty lines and lines starting with "#" are ignored so that the user can
// comment the options directly in KeePassXC.
func parseOptions(raw string) ([]string, error) {
	var options []string

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "=") {
			return nil, fmt.Errorf("%s: %q must be in the form Key=Value", fieldOptions, line)
		}
		options = append(options, line)
	}

	return options, nil
}

// splitTags converts the KeePass tag string into a list. KeePassXC separates
// tags with ";" while older KeePass versions used ",", so both are accepted.
func splitTags(raw string) []string {
	var tags []string
	for _, tag := range strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == ',' }) {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}
