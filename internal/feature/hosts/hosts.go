// Package hosts implements "zzh ls": the list of hosts stored in KeePass.
package hosts

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"zzh/internal/core/cliflags"
	"zzh/internal/core/style"
	"zzh/internal/core/table"
	"zzh/internal/core/vault"
)

// Run prints the hosts: a boxed, colored table on a terminal, or one
// tab-separated line per host with --grep or when the output is a pipe or a
// file (see the table package).
//
// args may contain "--tag <name>" (repeatable): only hosts carrying all the
// given tags are listed. Invalid entries are listed too, with their problem,
// so that the user can fix them in KeePassXC.
func Run(args []string, hostList []vault.Host, out io.Writer) error {
	parsed, err := cliflags.Parse(args, cliflags.Spec{Values: []string{"tag"}, Switches: []string{"grep"}})
	if err != nil {
		return fmt.Errorf("%w (usage: zzh ls [--tag name]... [--grep])", err)
	}
	if len(parsed.Positional) > 0 {
		return fmt.Errorf("unknown argument %q (usage: zzh ls [--tag name]... [--grep])", parsed.Positional[0])
	}

	return buildTable(hostList, parsed.Values("tag"), style.For(out)).Render(out, parsed.Switch("grep"))
}

// buildTable turns the hosts into table rows, colored by meaning: the name
// in bold, the folder dimmed, a stored password in bold magenta so that it
// is never overlooked, tags in cyan, problems in red.
func buildTable(hostList []vault.Host, tags []string, s style.Styler) table.Table {
	t := table.Table{Headers: []string{"NAME", "ALIASES", "FOLDER", "USER", "ADDRESS", "KEY", "PASSWORD", "TAGS"}}

	for _, host := range hostList {
		if !hasAllTags(host, tags) {
			continue
		}

		address := table.Plain(host.Address)
		if host.Port != 0 {
			address.Text += ":" + strconv.Itoa(host.Port)
		}
		if host.Problem != "" {
			address = table.Cell{Text: "INVALID: " + host.Problem, Paint: s.Error}
		}

		key := table.Plain(keyLabel(host))
		if host.KeyID != "" && host.KeyName == "" {
			key.Paint = s.Error
		}

		t.Add(
			table.Cell{Text: host.Name, Paint: s.Name},
			table.Plain(strings.Join(host.Aliases, ",")),
			table.Cell{Text: host.Folder, Paint: s.Dim},
			table.Plain(host.User),
			address,
			key,
			table.Cell{Text: passwordLabel(host), Paint: s.Stored},
			table.Cell{Text: strings.Join(host.Tags, ","), Paint: s.Tag},
		)
	}
	return t
}

// passwordLabel tells whether a login password is stored, never what it is:
// the listing is often shown on screen or pasted in chats and tickets.
func passwordLabel(host vault.Host) string {
	if host.Password == "" {
		return ""
	}
	return "stored"
}

// keyLabel shows the key a host uses. A reference to a key entry that no
// longer exists (deleted in KeePassXC) is flagged instead of hidden, because
// connections relying on it would fail.
func keyLabel(host vault.Host) string {
	switch {
	case host.KeyID == "":
		return ""
	case host.KeyName == "":
		return "MISSING KEY"
	default:
		return host.KeyName
	}
}

// hasAllTags compares tags case-insensitively: KeePassXC users often type
// tags with inconsistent capitalization ("Prod", "prod").
func hasAllTags(host vault.Host, wanted []string) bool {
	for _, tag := range wanted {
		found := false
		for _, hostTag := range host.Tags {
			if strings.EqualFold(hostTag, tag) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
