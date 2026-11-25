// Package edithost implements "zzh edit": it changes the fields of an
// existing host, leaving untouched everything that is not mentioned (D-032).
package edithost

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

const usage = `usage: zzh edit <host> [--rename name] [--address [user@]address[:port]]
                 [--port n] [--user name] [--password | --clear-password]
                 [--folder path] [--tag t]... [--option Key=Value]...
an empty value clears a field: --user= --port= --folder= --tag= --option=`

// PasswordReader asks the user for the new host password (with
// confirmation).
type PasswordReader func(message string) (string, error)

// Modifier opens the database, applies a change and saves it
// (see session.Modify).
type Modifier func(change func(v *vault.Vault) error) error

// Run parses the command line and applies the changes.
//
// reserved lists the zzh subcommand names, which a renamed host cannot take.
func Run(args []string, reserved []string, modify Modifier, askPassword PasswordReader, out io.Writer) error {
	parsed, err := cliflags.Parse(args, cliflags.Spec{
		Values:   []string{"rename", "address", "port", "user", "folder", "tag", "option"},
		Switches: []string{"password", "clear-password"},
	})
	if err != nil {
		return err
	}
	if len(parsed.Positional) != 1 {
		return errors.New(usage)
	}
	hostName := parsed.Positional[0]

	changes, summary, err := buildChanges(parsed, reserved)
	if err != nil {
		return err
	}

	// The new password is asked before the master password, so that a typo
	// in the confirmation does not cost a database unlock.
	if parsed.Switch("password") {
		password, err := askPassword(fmt.Sprintf("New password for %s: ", hostName))
		if err != nil {
			return err
		}
		changes.Password = &password
	}

	if changes.IsEmpty() {
		return errors.New(usage)
	}

	if err := modify(func(v *vault.Vault) error { return v.EditHost(hostName, changes) }); err != nil {
		return err
	}

	fmt.Fprintf(out, "Host %q updated: %s\n", hostName, strings.Join(summary, ", "))
	return nil
}

// buildChanges turns the options into vault.HostChanges, together with a
// readable summary for the confirmation message. Only the options actually
// given produce a change.
func buildChanges(parsed cliflags.Result, reserved []string) (vault.HostChanges, []string, error) {
	var changes vault.HostChanges
	var summary []string

	if parsed.Has("rename") {
		name := parsed.Value("rename")
		if err := vault.ValidateEntryName(name); err != nil {
			return changes, nil, err
		}
		if slices.Contains(reserved, name) {
			return changes, nil, fmt.Errorf("%q is a zzh command and cannot be used as a host name", name)
		}
		changes.Name = &name
		summary = append(summary, "renamed to "+name)
	}

	// --address accepts the same forms as "zzh add": a port or a user written
	// in it are applied too, unless --port or --user are also given.
	if parsed.Has("address") {
		address, port, user, err := vault.ParseAddress(parsed.Value("address"))
		if err != nil {
			return changes, nil, err
		}
		changes.Address = &address
		summary = append(summary, "address "+address)
		if port != 0 && !parsed.Has("port") {
			changes.Port = &port
		}
		if user != "" && !parsed.Has("user") {
			changes.User = &user
		}
	}

	if parsed.Has("port") {
		port := 0
		if text := parsed.Value("port"); text != "" {
			var err error
			port, err = strconv.Atoi(text)
			if err != nil || port < 1 || port > 65535 {
				return changes, nil, fmt.Errorf("invalid --port %q", text)
			}
		}
		changes.Port = &port
	}
	if changes.Port != nil {
		summary = append(summary, describe("port", strconv.Itoa(*changes.Port), *changes.Port == 0))
	}

	if parsed.Has("user") {
		user := parsed.Value("user")
		changes.User = &user
	}
	if changes.User != nil {
		summary = append(summary, describe("user", *changes.User, *changes.User == ""))
	}

	if parsed.Switch("password") && parsed.Switch("clear-password") {
		return changes, nil, errors.New("--password and --clear-password cannot be used together")
	}
	if parsed.Switch("password") {
		summary = append(summary, "password changed")
	}
	if parsed.Switch("clear-password") {
		empty := ""
		changes.Password = &empty
		summary = append(summary, "password removed")
	}

	if parsed.Has("folder") {
		folder := parsed.Value("folder")
		changes.Folder = &folder
		summary = append(summary, describe("folder", folder, folder == ""))
	}

	if parsed.Has("tag") {
		tags := nonEmpty(parsed.Values("tag"))
		changes.Tags = &tags
		summary = append(summary, describe("tags", strings.Join(tags, ","), len(tags) == 0))
	}

	if parsed.Has("option") {
		options := nonEmpty(parsed.Values("option"))
		changes.Options = &options
		summary = append(summary, describe("options", strings.Join(options, " "), len(options) == 0))
	}

	return changes, summary, nil
}

// nonEmpty drops the empty values used to clear a list ("--tag=").
func nonEmpty(values []string) []string {
	result := []string{}
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func describe(field, value string, cleared bool) string {
	if cleared {
		return field + " cleared"
	}
	return field + " " + value
}
