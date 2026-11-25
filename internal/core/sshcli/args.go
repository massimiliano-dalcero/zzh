// Package sshcli contains the knowledge zzh needs about the OpenSSH command
// line client: how its arguments are structured and which version is
// installed. zzh never implements the SSH protocol itself; it only prepares
// the right command line and lets the real ssh binary do the work.
package sshcli

import (
	"errors"
	"fmt"
	"strings"
)

// optionsWithArgument lists the ssh single-letter options that consume a
// value, taken from the getopt string of OpenSSH 9.x ("b:c:e:i:..." etc.).
// All the other letters in optionsWithoutArgument are plain switches.
//
// The parser needs this distinction to know, for example, that in
// "zzh web01 -L 8080:localhost:80 uptime" the token "8080:localhost:80"
// belongs to -L and "uptime" is the remote command.
const (
	optionsWithArgument    = "bceilmopBDEFIJLOPQRSwW"
	optionsWithoutArgument = "1246afgknqstvxyACGKMNTVXY"
)

// Invocation is a connect command line ("zzh [ssh options] name [command]")
// split into its parts.
type Invocation struct {
	// Target is the zzh host name, i.e. the title of the KeePass entry.
	Target string

	// User is set when the target was written as "user@name" and overrides
	// the username stored in the KeePass entry.
	User string

	// Options are the ssh options typed by the user, normalized so that each
	// option and its value are separate tokens ("-L8080:x:80" becomes
	// "-L", "8080:x:80"). They are passed to ssh unchanged in meaning.
	Options []string

	// Command is the remote command with its arguments; empty for an
	// interactive shell.
	Command []string

	// usedLetters and usedConfigKeys remember what the user set explicitly,
	// so that zzh does not add a conflicting value of its own.
	usedLetters    map[byte]bool
	usedConfigKeys map[string]bool
}

// HasOption reports whether the user passed the given single-letter option
// (for example 'p' for -p or 'J' for -J).
func (inv *Invocation) HasOption(letter byte) bool {
	return inv.usedLetters[letter]
}

// HasConfigKey reports whether the user passed "-o Key=..." for the given
// ssh_config keyword. The comparison is case-insensitive, like ssh itself.
func (inv *Invocation) HasConfigKey(key string) bool {
	return inv.usedConfigKeys[strings.ToLower(key)]
}

// ParseInvocation splits the arguments following "zzh" in connect mode.
//
// It follows the same rules as ssh so that every classic option works as
// the user expects:
//   - options may appear both before and after the host name;
//   - switches can be grouped ("-fN", "-vvv") and a value can be attached
//     to its option ("-p2222") or passed as the next argument ("-p 2222");
//   - "--" ends option parsing;
//   - after the host name, the first non-option argument starts the remote
//     command, and everything from there on belongs to the command, even
//     tokens that look like options ("zzh web01 ls -la").
func ParseInvocation(args []string) (*Invocation, error) {
	inv := &Invocation{
		usedLetters:    map[byte]bool{},
		usedConfigKeys: map[string]bool{},
	}

	optionsTerminated := false

	for index := 0; index < len(args); {
		arg := args[index]

		if !optionsTerminated && arg == "--" {
			optionsTerminated = true
			index++
			continue
		}

		// A lone "-" is not an option for ssh, so it falls through to the
		// positional handling below.
		if !optionsTerminated && len(arg) > 1 && arg[0] == '-' {
			consumed, err := inv.parseOptionGroup(args, index)
			if err != nil {
				return nil, err
			}
			index += consumed
			continue
		}

		if inv.Target == "" {
			inv.setTarget(arg)
			index++
			continue
		}

		inv.Command = append([]string(nil), args[index:]...)
		break
	}

	if inv.Target == "" {
		return nil, errors.New("missing host name")
	}

	return inv, nil
}

// parseOptionGroup handles one argument starting with "-", which may contain
// several grouped switches and at most one option with a value (which must
// be the last letter of the group, as in getopt).
//
// It returns how many elements of args were consumed: 1 normally, 2 when the
// option value is the following argument.
func (inv *Invocation) parseOptionGroup(args []string, index int) (int, error) {
	group := args[index][1:]

	for position := 0; position < len(group); position++ {
		letter := group[position]

		switch {
		case strings.IndexByte(optionsWithoutArgument, letter) >= 0:
			inv.usedLetters[letter] = true
			inv.Options = append(inv.Options, "-"+string(letter))

		case strings.IndexByte(optionsWithArgument, letter) >= 0:
			// The value is either the rest of this same argument ("-p2222")
			// or, if nothing follows the letter, the next argument.
			value := group[position+1:]
			consumed := 1

			if value == "" {
				if index+1 >= len(args) {
					return 0, fmt.Errorf("option -%c requires a value", letter)
				}
				value = args[index+1]
				consumed = 2
			}

			inv.recordOptionWithValue(letter, value)
			return consumed, nil

		default:
			return 0, fmt.Errorf("unknown ssh option -%c", letter)
		}
	}

	return 1, nil
}

func (inv *Invocation) recordOptionWithValue(letter byte, value string) {
	inv.usedLetters[letter] = true
	inv.Options = append(inv.Options, "-"+string(letter), value)

	// For -o we also remember the keyword, so that a user-provided
	// "-o Port=2222" prevents zzh from adding the port stored in KeePass.
	// ssh accepts both "Key=Value" and "Key Value".
	if letter == 'o' {
		key := value
		if cut := strings.IndexAny(value, "= \t"); cut >= 0 {
			key = value[:cut]
		}
		inv.usedConfigKeys[strings.ToLower(strings.TrimSpace(key))] = true
	}
}

// setTarget stores the host name, splitting an optional "user@" prefix.
// The last "@" is used because some environments (e.g. Active Directory
// logins like "max@corp.local@web01") put an "@" inside the user name.
func (inv *Invocation) setTarget(target string) {
	if at := strings.LastIndex(target, "@"); at > 0 {
		inv.User = target[:at]
		inv.Target = target[at+1:]
		return
	}
	inv.Target = target
}
