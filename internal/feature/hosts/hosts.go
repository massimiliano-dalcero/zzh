// Package hosts implements "zzh ls": the list of hosts stored in KeePass.
package hosts

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"zzh/internal/core/vault"
)

// Run prints the hosts as an aligned table.
//
// args may contain "--tag <name>" (repeatable): only hosts carrying all the
// given tags are listed. Invalid entries are listed too, with their problem,
// so that the user can fix them in KeePassXC.
func Run(args []string, hostList []vault.Host, out io.Writer) error {
	tags, err := parseTagFilter(args)
	if err != nil {
		return err
	}

	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "NAME\tALIASES\tFOLDER\tUSER\tADDRESS\tKEY\tPASSWORD\tTAGS")

	for _, host := range hostList {
		if !hasAllTags(host, tags) {
			continue
		}

		address := host.Address
		if host.Port != 0 {
			address += ":" + strconv.Itoa(host.Port)
		}
		if host.Problem != "" {
			address = "INVALID: " + host.Problem
		}

		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			host.Name, strings.Join(host.Aliases, ","), host.Folder, host.User, address,
			keyLabel(host), passwordLabel(host), strings.Join(host.Tags, ","))
	}

	return table.Flush()
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

// parseTagFilter reads the "--tag <name>" arguments.
func parseTagFilter(args []string) ([]string, error) {
	var tags []string

	for index := 0; index < len(args); index++ {
		if args[index] != "--tag" {
			return nil, fmt.Errorf("unknown argument %q (usage: zzh ls [--tag name]...)", args[index])
		}
		if index+1 >= len(args) {
			return nil, errors.New("--tag requires a value")
		}
		index++
		tags = append(tags, args[index])
	}

	return tags, nil
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
