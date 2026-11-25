// Command zzh drives the system OpenSSH client using hosts, keys and
// passwords stored in a KeePass (.kdbx) database.
//
// The first argument is either one of the reserved subcommands below or the
// name of a host to connect to: "zzh web01 -L 8080:localhost:80" connects to
// the KeePass entry "web01" and passes every other argument to ssh.
//
// This package only dispatches: the logic of each command lives in its own
// module under internal/feature.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"zzh/internal/core/askpass"
	"zzh/internal/core/config"
	"zzh/internal/core/daemon"
	"zzh/internal/core/prompt"
	"zzh/internal/core/session"
	"zzh/internal/core/sshcli"
	"zzh/internal/core/vault"
	"zzh/internal/feature/addhost"
	"zzh/internal/feature/aliases"
	"zzh/internal/feature/configcmd"
	"zzh/internal/feature/connect"
	"zzh/internal/feature/daemoncmd"
	"zzh/internal/feature/dbcmd"
	"zzh/internal/feature/doctor"
	"zzh/internal/feature/edithost"
	"zzh/internal/feature/hosts"
	"zzh/internal/feature/initdb"
	"zzh/internal/feature/keys"
	"zzh/internal/feature/removehost"
	"zzh/internal/feature/syncout"
)

// usage is the help text; runHelp prints it after a first line with the
// name and the version (see version.go).
const usage = `
Usage:
  zzh [--db name] <name> [--key k] [ssh options] [command]
                                  connect to the host stored as <name>
                                  (--key: use the KeePass key k this time)
  zzh [--db name] ls [--tag name]...
                                  list the hosts stored in KeePass
  zzh [--db name] add <name> <[user@]address[:port]> [--user u] [--port n]
          [--folder path] [--key name] [--alias a]... [--tag t]...
          [--option K=V]... [--password]
                                  add a host
  zzh [--db name] edit <host> [--rename n] [--address a] [--port n] [--user u]
          [--password | --clear-password] [--folder path] [--tag t]...
          [--option K=V]...
                                  change a host (only the given fields;
                                  an empty value clears: --user= --tag=)
  zzh [--db name] alias add <host> <alias>...
                                  more names for an existing host
  zzh [--db name] alias rm <alias>...
                                  remove aliases
  zzh [--db name] rm <host> [--yes]
                                  remove a host (to the KeePass recycle bin)

  zzh [--db name] key gen <name> [--type ed25519|ecdsa|rsa] [--bits n]
          [--comment text] [--folder path]
                                  create a key pair stored in KeePass
  zzh [--db name] key import <name> <file|-> [--comment text] [--folder path]
                                  store an existing private key (- = paste it)
  zzh [--db name] key ls          list the keys and the hosts using them
  zzh [--db name] key rm <name> [--force] [--yes]
                                  remove a key (--force: also from its hosts)
  zzh [--db name] key pub <name>  print the public key
  zzh [--db name] key use <host> <key>|--none
                                  choose the key a host logs in with

  zzh [--db name] sync out [--dir path] [--dry-run]
                                  write hosts and keys to ~/.ssh for plain ssh
                                  (hosts in ~/.ssh/zzh_config, included by ~/.ssh/config;
                                  changed files are kept as <file>_<n>.bkp)

  zzh [--db name] unlock          keep the master password in the zzh daemon
  zzh [--db name] lock            forget it (without --db: every database)
  zzh status                      unlocked databases and when they lock
  zzh daemon stop                 stop the daemon, locking everything

  zzh init <name> <path.kdbx>     create a new database and bookmark it
  zzh db ls                       list the bookmarked databases
  zzh db add <name> <path.kdbx>   bookmark an existing database
  zzh db rm <name>                forget a bookmark (the file is kept)
  zzh db default <name>           choose the database used without --db

  zzh doctor                      check ssh client and configuration
  zzh config path|show|init       inspect or create the config file
  zzh config get <setting>        read one setting (e.g. ssh.use_system_keys)
  zzh config set <setting> <value>...
                                  change a setting without editing the file
  zzh config unset <setting>      restore a setting to its default
  zzh version                     print the zzh version
  zzh help                        show this help

--db must come right after "zzh" and selects a bookmarked database
instead of the default one.

The master password comes from ZZH_PASSWORD when set (zzh removes it from
the environment of every program it starts), otherwise from the zzh daemon,
which keeps it after the first use (see lock.idle_timeout,
lock.max_lifetime, daemon.autostart), otherwise it is asked on the terminal.

Every classic ssh option (-L, -R, -D, -J, -v, -t, -o ...) can be used
before or after <name> and is passed to ssh unchanged. Options typed on the
command line always win over the values stored in KeePass: "user@<name>" or
-l changes the user, -p the port, --key the key, for that connection only.

Developed by Massimiliano Dal Cero - DRIFT Linux
https://www.linkedin.com/in/dalcero/
`

// globalOptions are the zzh options that precede the subcommand or host.
type globalOptions struct {
	// database is the bookmark selected with "--db"; empty means default.
	database string
}

// subcommands maps the reserved words to their handlers. Any first argument
// not listed here is treated as a host name, which is why host entries
// cannot be called like a subcommand (see decision D-008).
//
// The map is filled in init() rather than in its declaration because "add"
// reads it (to refuse host names equal to a subcommand), and Go rejects a
// variable whose initializer refers back to itself.
var subcommands map[string]func(options globalOptions, args []string) error

func init() {
	subcommands = map[string]func(options globalOptions, args []string) error{
		"ls":      runList,
		"add":     runAdd,
		"edit":    runEdit,
		"rm":      runRemove,
		"alias":   runAlias,
		"key":     runKey,
		"sync":    runSync,
		"unlock":  runUnlock,
		"lock":    runLock,
		"status":  runStatus,
		"daemon":  runDaemon,
		"init":    runInit,
		"db":      runDatabases,
		"doctor":  runDoctor,
		"config":  runConfig,
		"version": runVersion,
		"help":    runHelp,
	}
}

// usesDatabaseFlag lists the commands for which "--db" makes sense, so that
// a misplaced flag ("zzh --db work db ls") is reported instead of ignored.
var usesDatabaseFlag = map[string]bool{
	"ls":     true,
	"add":    true,
	"edit":   true,
	"rm":     true,
	"alias":  true,
	"key":    true,
	"sync":   true,
	"unlock": true,
	"lock":   true,
}

func main() {
	// First thing: take ZZH_PASSWORD out of the environment, before any
	// child process (ssh, icacls...) could inherit it (D-028).
	if err := session.TakePasswordFromEnvironment(); err != nil {
		fmt.Fprintln(os.Stderr, "zzh:", err)
		os.Exit(1)
	}

	if isAskpassInvocation(os.Args[1:]) {
		os.Exit(runAskpass(os.Args[1]))
	}

	err := run(os.Args[1:])

	// When ssh itself fails it has already printed its own message: zzh only
	// propagates the exit code, so that scripts see the remote result.
	var exitStatus connect.ExitStatus
	if errors.As(err, &exitStatus) {
		os.Exit(exitStatus.Code)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "zzh:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	options, args, err := parseGlobalOptions(args)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		// Later this will open an interactive host picker; for now the
		// help is the most useful thing to show.
		return runHelp(options, nil)
	}

	if handler, found := subcommands[args[0]]; found {
		if options.database != "" && !usesDatabaseFlag[args[0]] {
			return fmt.Errorf("--db cannot be used with \"zzh %s\"", args[0])
		}
		return handler(options, args[1:])
	}

	return runConnect(options, args)
}

// parseGlobalOptions consumes the zzh options at the start of the command
// line ("--db work" or "--db=work") and returns the remaining arguments.
//
// Only leading options are considered: anything after the subcommand or host
// name belongs to that command or to ssh. Long options are safe here because
// ssh has none, so "--db" can never be mistaken for an ssh option.
func parseGlobalOptions(args []string) (globalOptions, []string, error) {
	var options globalOptions

	for len(args) > 0 {
		arg := args[0]

		switch {
		case arg == "--db":
			if len(args) < 2 {
				return options, nil, errors.New("--db requires a database name")
			}
			options.database = args[1]
			args = args[2:]

		case strings.HasPrefix(arg, "--db="):
			options.database = strings.TrimPrefix(arg, "--db=")
			if options.database == "" {
				return options, nil, errors.New("--db requires a database name")
			}
			args = args[1:]

		default:
			return options, args, nil
		}
	}

	return options, args, nil
}

func runConnect(options globalOptions, args []string) error {
	// The command line is validated before asking for the master password,
	// so that a typo does not cost the user a password prompt.
	invocation, err := sshcli.ParseInvocation(args)
	if err != nil {
		return fmt.Errorf("%w (see \"zzh help\")", err)
	}

	loaded, err := config.LoadDefault()
	if err != nil {
		return err
	}

	v, err := session.OpenVault(loaded.Config, options.database)
	if err != nil {
		return err
	}

	host, found, err := connect.ResolveTarget(v, invocation.Target)
	if err != nil {
		return err
	}

	// "--key <name>" picks a KeePass key for this connection only, instead
	// of the one associated with the host, and also works for hosts that
	// are not in KeePass (D-037). It is read now, while the vault is open.
	var chosenKey []byte
	if invocation.KeyName != "" {
		chosenKey, err = v.PrivateKey(invocation.KeyName)
		if err != nil {
			return err
		}
	}

	// Not in KeePass: behave like ssh, with the arguments as typed and the
	// system ssh configuration (D-030).
	if !found {
		if !loaded.Config.SSH.PassthroughUnknownHosts {
			return fmt.Errorf("host %q is not in KeePass (neither a name nor an address)", invocation.Target)
		}
		fmt.Fprintf(os.Stderr, "zzh: %q is not in KeePass, running plain ssh (save it with \"zzh add\")\n", invocation.Target)
		return connect.Passthrough(loaded.Config.SSH.Binary, invocation, args, chosenKey, invocation.KeyName)
	}

	// The private key is read now, while the vault is open; the connection
	// serves it to ssh from memory (D-029).
	var privateKey []byte
	switch {
	case chosenKey != nil:
		privateKey = chosenKey
		host.KeyName = invocation.KeyName
	case host.KeyName != "":
		privateKey, err = v.PrivateKey(host.KeyName)
		if err != nil {
			return err
		}
	case host.KeyID != "":
		fmt.Fprintf(os.Stderr, "zzh: the key associated with %q no longer exists in KeePass, connecting without it\n", host.Name)
	}

	// ssh will run this same executable as askpass program.
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine the zzh executable path: %w", err)
	}

	return connect.Connect(connect.Request{
		SSH:        loaded.Config.SSH,
		Host:       host,
		Invocation: invocation,
		PrivateKey: privateKey,
		Executable: executable,
		AskHidden:  prompt.Password,
		AskVisible: prompt.Line,
	})
}

// isAskpassInvocation recognizes the case where ssh runs zzh as its askpass
// program: the askpass variables set by zzh are in the environment (only
// ssh started by zzh, and its children, have them) and the only argument is
// the question, which is never a zzh subcommand.
func isAskpassInvocation(args []string) bool {
	if !askpass.Requested() || len(args) != 1 {
		return false
	}
	_, isSubcommand := subcommands[args[0]]
	return !isSubcommand
}

// runAskpass answers one ssh question: it forwards it to the zzh process
// that started ssh and prints the answer on stdout, where ssh reads it. A
// non-zero exit tells ssh that the question was cancelled.
func runAskpass(question string) int {
	answer, err := askpass.Ask(question)
	if err != nil {
		if !errors.Is(err, askpass.ErrRefused) {
			fmt.Fprintln(os.Stderr, "zzh askpass:", err)
		}
		return 1
	}
	fmt.Println(answer)
	return 0
}

func runList(options globalOptions, args []string) error {
	loaded, err := config.LoadDefault()
	if err != nil {
		return err
	}

	v, err := session.OpenVault(loaded.Config, options.database)
	if err != nil {
		return err
	}

	hostList, err := v.Hosts()
	if err != nil {
		return err
	}

	return hosts.Run(args, hostList, os.Stdout)
}

func runAdd(options globalOptions, args []string) error {
	loaded, err := config.LoadDefault()
	if err != nil {
		return err
	}

	modify := func(change func(v *vault.Vault) error) error {
		return session.Modify(loaded.Config, options.database, change)
	}
	return addhost.Run(args, reservedNames(), modify, prompt.NewPassword, os.Stdout)
}

func runKey(options globalOptions, args []string) error {
	loaded, err := config.LoadDefault()
	if err != nil {
		return err
	}

	store := keys.Store{
		Open: func() (*vault.Vault, error) {
			return session.OpenVault(loaded.Config, options.database)
		},
		Modify: func(change func(v *vault.Vault) error) error {
			return session.Modify(loaded.Config, options.database, change)
		},
	}
	return keys.Run(args, store, loaded.Config.Keys, os.Stdin, prompt.Password, prompt.Confirm, os.Stdout)
}

func runRemove(options globalOptions, args []string) error {
	loaded, err := config.LoadDefault()
	if err != nil {
		return err
	}

	modify := func(change func(v *vault.Vault) error) error {
		return session.Modify(loaded.Config, options.database, change)
	}
	return removehost.Run(args, modify, prompt.Confirm, os.Stdout)
}

func runEdit(options globalOptions, args []string) error {
	loaded, err := config.LoadDefault()
	if err != nil {
		return err
	}

	modify := func(change func(v *vault.Vault) error) error {
		return session.Modify(loaded.Config, options.database, change)
	}
	return edithost.Run(args, reservedNames(), modify, prompt.NewPassword, os.Stdout)
}

func runAlias(options globalOptions, args []string) error {
	loaded, err := config.LoadDefault()
	if err != nil {
		return err
	}

	modify := func(change func(v *vault.Vault) error) error {
		return session.Modify(loaded.Config, options.database, change)
	}
	return aliases.Run(args, reservedNames(), modify, os.Stdout)
}

// daemonOperations connects the daemon commands to the session package.
func daemonOperations(cfg config.Config) daemoncmd.Operations {
	return daemoncmd.Operations{
		Unlock: func(databaseName string) (config.ResolvedDatabase, error) {
			return session.Unlock(cfg, databaseName)
		},
		Lock: func(databaseName string, all bool) error {
			return session.Lock(cfg, databaseName, all)
		},
		Status: session.Status,
		Stop:   session.StopDaemon,
		Run: func() error {
			dir, err := daemon.DefaultDir()
			if err != nil {
				return err
			}
			return daemon.Run(dir, os.Getpid())
		},
	}
}

func runUnlock(options globalOptions, args []string) error {
	loaded, err := config.LoadDefault()
	if err != nil {
		return err
	}
	return daemoncmd.Unlock(args, options.database, loaded.Config, daemonOperations(loaded.Config), os.Stdout)
}

func runLock(options globalOptions, args []string) error {
	loaded, err := config.LoadDefault()
	if err != nil {
		return err
	}
	return daemoncmd.Lock(args, options.database, daemonOperations(loaded.Config), os.Stdout)
}

func runStatus(_ globalOptions, args []string) error {
	loaded, err := config.LoadDefault()
	if err != nil {
		return err
	}
	return daemoncmd.Status(args, loaded.Config, daemonOperations(loaded.Config), time.Now(), os.Stdout)
}

// runDaemon does not need a valid config file: the daemon only keeps
// passwords, and "zzh daemon stop" must work even when the file is broken.
func runDaemon(_ globalOptions, args []string) error {
	return daemoncmd.Daemon(args, daemonOperations(config.Defaults()), os.Stdout)
}

// runSync handles "zzh sync out". The direction is an explicit word so that
// "zzh sync in" (import from ~/.ssh) can be added later without ambiguity.
func runSync(options globalOptions, args []string) error {
	if len(args) == 0 || args[0] != "out" {
		return errors.New("usage: zzh sync out [--dir path] [--dry-run]")
	}

	loaded, err := config.LoadDefault()
	if err != nil {
		return err
	}
	defaultDir, err := syncout.DefaultDir()
	if err != nil {
		return err
	}

	open := func() (*vault.Vault, error) {
		return session.OpenVault(loaded.Config, options.database)
	}
	return syncout.Run(args[1:], open, defaultDir, os.Stdout)
}

// reservedNames returns the subcommand names, which cannot be host names.
func reservedNames() []string {
	names := make([]string, 0, len(subcommands))
	for name := range subcommands {
		names = append(names, name)
	}
	return names
}

func runInit(_ globalOptions, args []string) error {
	loaded, err := config.LoadDefault()
	return initdb.Run(args, loaded, err, prompt.NewPassword, os.Stdout)
}

func runDatabases(_ globalOptions, args []string) error {
	loaded, err := config.LoadDefault()
	return dbcmd.Run(args, loaded, err, os.Stdout)
}

func runDoctor(_ globalOptions, _ []string) error {
	loaded, err := config.LoadDefault()
	return doctor.Run(loaded, err, os.Stdout)
}

func runConfig(_ globalOptions, args []string) error {
	loaded, err := config.LoadDefault()
	return configcmd.Run(args, loaded, err, os.Stdout, os.Stderr)
}

func runVersion(_ globalOptions, _ []string) error {
	fmt.Println("zzh", fullVersion(), "- Massimiliano Dal Cero, DRIFT Linux")
	return nil
}

func runHelp(_ globalOptions, _ []string) error {
	fmt.Printf("zzh %s - SSH connections driven by a KeePass database\n", fullVersion())
	fmt.Print(usage)
	return nil
}
