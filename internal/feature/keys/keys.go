// Package keys implements "zzh key gen|import|ls|pub|use": SSH key pairs
// stored in KeePass and their association with hosts.
package keys

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"zzh/internal/core/cliflags"
	"zzh/internal/core/config"
	"zzh/internal/core/sshkey"
	"zzh/internal/core/style"
	"zzh/internal/core/table"
	"zzh/internal/core/vault"
)

const usage = `usage:
  zzh key gen <name> [--type ed25519|ecdsa|rsa] [--bits n] [--comment text] [--folder path]
  zzh key import <name> <file|-> [--comment text] [--folder path]
  zzh key ls [--grep]
  zzh key rm <name> [--force] [--yes]
  zzh key pub <name>
  zzh key use <host> <key>
  zzh key use <host> --none`

// Store gives the command access to the database. Open is used for read-only
// subcommands, Modify for the ones that save (see session.Modify). They are
// parameters so that tests can work on a temporary database.
type Store struct {
	Open   func() (*vault.Vault, error)
	Modify func(change func(v *vault.Vault) error) error
}

// SecretReader asks the user for a secret without echo (prompt.Password).
type SecretReader func(message string) (string, error)

// Confirmer asks a yes/no question (prompt.Confirm).
type Confirmer func(message string) (bool, error)

// Run dispatches the key subcommands.
//
// in is where "zzh key import <name> -" reads a pasted key from, askSecret
// asks for the passphrase of an encrypted key and confirm asks before a
// removal; they are parameters so that tests can run without a terminal.
func Run(args []string, store Store, keysConfig config.KeysConfig, in io.Reader, askSecret SecretReader, confirm Confirmer, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}

	switch args[0] {
	case "gen":
		return generate(args[1:], store, keysConfig, out)
	case "import":
		return importKey(args[1:], store, in, askSecret, out)
	case "rm":
		return removeKey(args[1:], store, confirm, out)
	case "ls":
		return list(args[1:], store, out)
	case "pub":
		return printPublic(args[1:], store, out)
	case "use":
		return use(args[1:], store, out)
	default:
		return errors.New(usage)
	}
}

// generate creates a key pair and stores it in a new entry under Keys.
//
// The key is generated before the database is opened: session.Modify may
// apply the change more than once (if KeePassXC saves meanwhile), and every
// attempt must store the same key.
func generate(args []string, store Store, keysConfig config.KeysConfig, out io.Writer) error {
	parsed, err := cliflags.Parse(args, cliflags.Spec{Values: []string{"type", "bits", "comment", "folder"}})
	if err != nil {
		return err
	}
	if len(parsed.Positional) != 1 {
		return errors.New(usage)
	}
	name := parsed.Positional[0]

	if err := vault.ValidateEntryName(name); err != nil {
		return err
	}

	keyType := parsed.Value("type")
	if keyType == "" {
		keyType = keysConfig.DefaultType
	}

	bits := 0
	if text := parsed.Value("bits"); text != "" {
		bits, err = strconv.Atoi(text)
		if err != nil {
			return fmt.Errorf("invalid --bits %q", text)
		}
	} else if keyType == sshkey.TypeRSA {
		bits = keysConfig.RSABits
	}

	// The comment ends up in authorized_keys on every host the key is
	// installed on; the key name is the most useful default to recognize it.
	comment := parsed.Value("comment")
	if comment == "" {
		comment = name
	}

	pair, err := sshkey.Generate(keyType, bits, comment)
	if err != nil {
		return err
	}

	err = store.Modify(func(v *vault.Vault) error {
		return v.AddKey(vault.NewKey{
			Name:        name,
			Folder:      parsed.Value("folder"),
			Type:        pair.Type,
			PublicKey:   pair.PublicLine,
			Fingerprint: pair.Fingerprint,
			PrivatePEM:  pair.PrivatePEM,
		})
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Key %q created (%s, %s)\n", name, pair.Type, pair.Fingerprint)
	fmt.Fprintln(out, pair.PublicLine)
	fmt.Fprintf(out, "Associate it with a host: zzh key use <host> %s\n", name)
	return nil
}

// list shows the keys and, for each one, the hosts configured to use it:
// a table on a terminal, tab-separated lines with --grep or in a pipe.
func list(args []string, store Store, out io.Writer) error {
	grep, err := table.ParseGrepOnly(args, "usage: zzh key ls [--grep]")
	if err != nil {
		return err
	}

	v, err := store.Open()
	if err != nil {
		return err
	}
	keyList, err := v.Keys()
	if err != nil {
		return err
	}
	hostList, err := v.Hosts()
	if err != nil {
		return err
	}

	usedBy := map[string][]string{}
	for _, host := range hostList {
		if host.KeyID != "" {
			usedBy[host.KeyID] = append(usedBy[host.KeyID], host.Name)
		}
	}

	return keyTable(keyList, usedBy, style.For(out)).Render(out, grep)
}

// keyTable builds the key listing: names in bold, fingerprints dimmed (long
// and rarely read), unused keys flagged in yellow as candidates for removal.
func keyTable(keyList []vault.Key, usedBy map[string][]string, s style.Styler) table.Table {
	t := table.Table{Headers: []string{"NAME", "TYPE", "FINGERPRINT", "HOSTS"}}
	for _, key := range keyList {
		hosts := table.Plain(strings.Join(usedBy[key.ID], ","))
		if len(usedBy[key.ID]) == 0 {
			hosts = table.Cell{Text: "unused", Paint: s.Warning}
		}
		t.Add(
			table.Cell{Text: key.Name, Paint: s.Name},
			table.Plain(key.Type),
			table.Cell{Text: key.Fingerprint, Paint: s.Dim},
			hosts,
		)
	}
	return t
}

// printPublic prints only the public key line, so that the output can be
// used directly: "zzh key pub laptop >> ~/.ssh/authorized_keys".
func printPublic(args []string, store Store, out io.Writer) error {
	if len(args) != 1 {
		return errors.New(usage)
	}

	v, err := store.Open()
	if err != nil {
		return err
	}
	key, err := v.FindKey(args[0])
	if err != nil {
		return err
	}
	if key.PublicKey == "" {
		return fmt.Errorf("key %q has no public key stored", args[0])
	}

	fmt.Fprintln(out, key.PublicKey)
	return nil
}

// use associates a key with a host, or removes the association with --none.
// It only changes KeePass: installing the key on the remote host is a
// separate step ("zzh key push", or by hand with "zzh key pub").
func use(args []string, store Store, out io.Writer) error {
	parsed, err := cliflags.Parse(args, cliflags.Spec{Switches: []string{"none"}})
	if err != nil {
		return err
	}

	var hostName, keyName string
	switch {
	case parsed.Switch("none") && len(parsed.Positional) == 1:
		hostName = parsed.Positional[0]
	case !parsed.Switch("none") && len(parsed.Positional) == 2:
		hostName, keyName = parsed.Positional[0], parsed.Positional[1]
	default:
		return errors.New(usage)
	}

	err = store.Modify(func(v *vault.Vault) error {
		return v.SetHostKey(hostName, keyName)
	})
	if err != nil {
		return err
	}

	if keyName == "" {
		fmt.Fprintf(out, "Host %q no longer uses a key\n", hostName)
	} else {
		fmt.Fprintf(out, "Host %q now uses the key %q\n", hostName, keyName)
	}
	return nil
}
