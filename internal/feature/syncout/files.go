package syncout

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// apply writes the planned files following the user's rule: an existing
// file with different content is first renamed to "<file>_<n>.bkp" (the
// first free number), never overwritten or deleted. A file that already has
// exactly the planned content is left alone, so that repeated syncs do not
// pile up identical backups.
//
// With dryRun the same decisions are reported but nothing is changed.
func apply(files []plannedFile, dir string, dryRun bool, out io.Writer) error {
	if dryRun {
		fmt.Fprintln(out, "Dry run: nothing will be changed.")
	} else if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}

	written, unchanged := 0, 0

	for _, file := range files {
		existing, err := os.ReadFile(file.path)
		exists := err == nil

		switch {
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("cannot read %s: %w", file.path, err)

		case exists && bytes.Equal(existing, file.content):
			unchanged++
			continue

		case exists:
			backup, err := backupName(file.path)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "backup  %s -> %s\n", file.path, filepath.Base(backup))
			if !dryRun {
				if err := os.Rename(file.path, backup); err != nil {
					return fmt.Errorf("cannot back up %s: %w", file.path, err)
				}
			}
		}

		fmt.Fprintf(out, "write   %s\n", file.path)
		if !dryRun {
			if err := writeFile(file); err != nil {
				return err
			}
		}
		written++
	}

	fmt.Fprintf(out, "%d written, %d unchanged\n", written, unchanged)
	return nil
}

// backupName returns the first "<path>_<n>.bkp" that does not exist yet.
func backupName(path string) (string, error) {
	for n := 1; ; n++ {
		candidate := path + "_" + strconv.Itoa(n) + ".bkp"
		_, err := os.Lstat(candidate)
		if errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("cannot check %s: %w", candidate, err)
		}
	}
}

// writeFile creates the file with restrictive permissions from the start
// (O_EXCL: the previous file has just been moved away, so finding one here
// means something else is writing in the directory at the same time).
//
// Private files are restricted to the owner before any content is written,
// so a private key is never readable by others, not even for a moment:
// mode 0600 on Linux and macOS, an explicit owner-only ACL on Windows (see
// restrictToOwner), where the mode is ignored.
func writeFile(file plannedFile) error {
	mode := os.FileMode(0o644)
	if file.private {
		mode = 0o600
	}

	handle, err := os.OpenFile(file.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("cannot create %s: %w", file.path, err)
	}

	if file.private {
		// The file is still empty here. The ACL is changed by path, which
		// works on Windows while this handle is open.
		if err := restrictToOwner(file.path); err != nil {
			handle.Close()
			// Best effort: the file is empty, removing it only avoids
			// leaving an unusable placeholder behind.
			_ = os.Remove(file.path)
			return err
		}
	}

	_, writeErr := handle.Write(file.content)
	closeErr := handle.Close()
	if writeErr != nil {
		return fmt.Errorf("cannot write %s: %w", file.path, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("cannot write %s: %w", file.path, closeErr)
	}
	return nil
}
