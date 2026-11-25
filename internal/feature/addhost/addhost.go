// Package addhost implements "zzh add": it creates a host entry in KeePass.
package addhost

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"zzh/internal/core/cliflags"
	"zzh/internal/core/vault"
)

const usage = `usage: zzh add <name> <[user@]address[:port]> [--user name] [--port n]
                 [--folder path] [--key name] [--alias name]... [--tag tag]...
                 [--option Key=Value]...
                 [--password]`

// PasswordReader asks the user for the host password (with confirmation).
type PasswordReader func(message string) (string, error)

// Modifier opens the database, applies a change and saves it
// (see session.Modify).
type Modifier func(change func(v *vault.Vault) error) error

// Run parses the command line and stores the new host.
//
// reserved lists the zzh subcommand names: a host called like one of them
// could never be reached with "zzh <name>" (D-008), so it is refused here.
func Run(args []string, reserved []string, modify Modifier, askPassword PasswordReader, out io.Writer) error {
	parsed, err := cliflags.Parse(args, cliflags.Spec{
		Values:   []string{"user", "port", "folder", "key", "tag", "option", "alias"},
		Switches: []string{"password"},
	})
	if err != nil {
		return err
	}
	if len(parsed.Positional) != 2 {
		return errors.New(usage)
	}

	host, err := buildHost(parsed, reserved)
	if err != nil {
		return err
	}

	// The host password is asked before the master password so that a
	// typo in the confirmation does not cost a database unlock.
	if parsed.Switch("password") {
		label := host.Name
		if host.User != "" {
			label = host.User + "@" + host.Name
		}
		host.Password, err = askPassword(fmt.Sprintf("Password for %s: ", label))
		if err != nil {
			return err
		}
	}

	if err := modify(func(v *vault.Vault) error { return v.AddHost(host) }); err != nil {
		return err
	}

	fmt.Fprintf(out, "Host %q added (%s)\n", host.Name, describe(host))
	return nil
}

// buildHost turns the parsed command line into a vault.NewHost, checking
// everything that does not need the database.
func buildHost(parsed cliflags.Result, reserved []string) (vault.NewHost, error) {
	name, rawAddress := parsed.Positional[0], parsed.Positional[1]

	if err := vault.ValidateEntryName(name); err != nil {
		return vault.NewHost{}, err
	}
	// Aliases are typed like names ("zzh <alias>"), so the same rule applies.
	aliases := parsed.Values("alias")
	for _, word := range append([]string{name}, aliases...) {
		if slices.Contains(reserved, word) {
			return vault.NewHost{}, fmt.Errorf("%q is a zzh command and cannot be used as a host name or alias", word)
		}
	}

	// The address accepts the same forms as the KeePass URL field
	// ("deploy@10.0.0.2:2222", "ssh://web02"...); explicit options win.
	address, port, user, err := vault.ParseAddress(rawAddress)
	if err != nil {
		return vault.NewHost{}, err
	}

	if value := parsed.Value("user"); value != "" {
		user = value
	}
	if value := parsed.Value("port"); value != "" {
		port, err = strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return vault.NewHost{}, fmt.Errorf("invalid --port %q", value)
		}
	}

	return vault.NewHost{
		Name:    name,
		Folder:  parsed.Value("folder"),
		Address: address,
		Port:    port,
		User:    user,
		KeyName: parsed.Value("key"),
		Tags:    parsed.Values("tag"),
		Options: parsed.Values("option"),
		Aliases: aliases,
	}, nil
}

// describe summarizes the new host for the confirmation message.
func describe(host vault.NewHost) string {
	text := host.Address
	if host.Port != 0 {
		text += ":" + strconv.Itoa(host.Port)
	}
	if host.User != "" {
		text = host.User + "@" + text
	}
	if host.KeyName != "" {
		text += ", key " + host.KeyName
	}
	if host.Password != "" {
		text += ", password stored"
	}
	if host.Folder != "" {
		text += ", folder " + host.Folder
	}
	if len(host.Aliases) > 0 {
		text += ", aliases " + strings.Join(host.Aliases, " ")
	}
	return text
}
