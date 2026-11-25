package keys

import (
	"errors"
	"fmt"
	"io"
	"os"

	"zzh/internal/core/cliflags"
	"zzh/internal/core/sshkey"
	"zzh/internal/core/vault"
)

// maxKeyFileSize bounds what is read as a private key. Real keys are a few
// KB at most (an RSA 16384 key is about 12 KB); the limit protects against
// pointing the command at a large file by mistake.
const maxKeyFileSize = 64 * 1024

// importKey stores an existing private key (for example one handed over by
// a colleague or a customer) as a zzh key entry (D-025).
//
// The key is normalized like a generated one: OpenSSH format, no
// passphrase. The comment comes from, in order: --comment, the ".pub" file
// next to the private key (only if it belongs to the same key), the key
// name. Importing a key already stored under another name is refused, so
// that one key is never split over two entries.
func importKey(args []string, store Store, in io.Reader, askSecret SecretReader, out io.Writer) error {
	parsed, err := cliflags.Parse(args, cliflags.Spec{Values: []string{"comment", "folder"}})
	if err != nil {
		return err
	}
	if len(parsed.Positional) != 2 {
		return errors.New(usage)
	}
	name, source := parsed.Positional[0], parsed.Positional[1]

	if err := vault.ValidateEntryName(name); err != nil {
		return err
	}

	data, err := readKeySource(source, in, out)
	if err != nil {
		return err
	}

	askPassphrase := func() (string, error) {
		return askSecret("Passphrase of the key: ")
	}
	pair, err := sshkey.Import(data, name, askPassphrase)
	if err != nil {
		return err
	}

	comment := chooseComment(parsed.Value("comment"), source, pair.Fingerprint)
	if comment != "" && comment != name {
		pair, err = sshkey.WithComment(pair, comment)
		if err != nil {
			return err
		}
	}

	err = store.Modify(func(v *vault.Vault) error {
		existing, err := v.Keys()
		if err != nil {
			return err
		}
		for _, key := range existing {
			if key.Fingerprint == pair.Fingerprint {
				return fmt.Errorf("this key is already stored as %q", key.Name)
			}
		}

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

	fmt.Fprintf(out, "Key %q imported (%s, %s)\n", name, pair.Type, pair.Fingerprint)
	fmt.Fprintln(out, pair.PublicLine)
	fmt.Fprintf(out, "Associate it with a host: zzh key use <host> %s\n", name)

	// zzh never deletes files it did not create, but the point of storing
	// the key in KeePass is to not have it lying around on disk.
	if source != "-" {
		fmt.Fprintf(out, "The key is now in KeePass: %s (and its .pub) can be deleted.\n", source)
	}
	return nil
}

// readKeySource reads the private key from a file, or from in when source
// is "-", so that a key received by mail or chat can be pasted without ever
// being saved to disk.
func readKeySource(source string, in io.Reader, out io.Writer) ([]byte, error) {
	if source == "-" {
		fmt.Fprintln(out, "Paste the private key, then press Ctrl+Z and Enter (Windows) or Ctrl+D (Linux/macOS):")
		data, err := io.ReadAll(io.LimitReader(in, maxKeyFileSize+1))
		if err != nil {
			return nil, fmt.Errorf("cannot read the key: %w", err)
		}
		if len(data) > maxKeyFileSize {
			return nil, errors.New("the input is too large to be a private key")
		}
		return data, nil
	}

	info, err := os.Stat(source)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", source, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", source)
	}
	if info.Size() > maxKeyFileSize {
		return nil, fmt.Errorf("%s is too large to be a private key", source)
	}

	data, err := os.ReadFile(source)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", source, err)
	}
	return data, nil
}

// chooseComment picks the comment for the imported key (see importKey).
// An empty result means "keep the key name".
func chooseComment(explicit, source, fingerprint string) string {
	if explicit != "" {
		return explicit
	}
	if source == "-" {
		return ""
	}

	// A missing or unreadable .pub is normal (often only the private key is
	// handed over), so errors here simply mean "no comment from the file".
	publicLine, err := os.ReadFile(source + ".pub")
	if err != nil {
		return ""
	}
	comment, matches := sshkey.PublicKeyComment(publicLine, fingerprint)
	if !matches {
		return ""
	}
	return comment
}
