// Package daemoncmd implements the commands around the zzh daemon:
// "zzh unlock", "zzh lock", "zzh status" and "zzh daemon run|stop" (D-033).
package daemoncmd

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"zzh/internal/core/config"
	"zzh/internal/core/daemon"
)

// Operations groups what the commands need from the session package, as
// parameters so that the tests can replace them.
type Operations struct {
	Unlock func(databaseName string) (config.ResolvedDatabase, error)
	Lock   func(databaseName string, all bool) error
	Status func() ([]daemon.Unlocked, error)
	Stop   func() error
	Run    func() error
}

// Unlock handles "zzh [--db name] unlock".
func Unlock(args []string, databaseName string, cfg config.Config, ops Operations, out io.Writer) error {
	if len(args) != 0 {
		return errors.New("usage: zzh [--db name] unlock")
	}

	database, err := ops.Unlock(databaseName)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "%q unlocked (%s)\n", database.Name, describePolicy(cfg.Lock))
	return nil
}

// Lock handles "zzh lock" (every database) and "zzh --db name lock" (one).
func Lock(args []string, databaseName string, ops Operations, out io.Writer) error {
	if len(args) != 0 {
		return errors.New("usage: zzh [--db name] lock")
	}

	all := databaseName == ""
	if err := ops.Lock(databaseName, all); err != nil {
		return err
	}

	if all {
		fmt.Fprintln(out, "All databases locked")
	} else {
		fmt.Fprintf(out, "%q locked\n", databaseName)
	}
	return nil
}

// Status handles "zzh status": which databases are unlocked and until when.
func Status(args []string, cfg config.Config, ops Operations, now time.Time, out io.Writer) error {
	if len(args) != 0 {
		return errors.New("usage: zzh status")
	}

	unlocked, err := ops.Status()
	if errors.Is(err, daemon.ErrNotRunning) || (err == nil && len(unlocked) == 0) {
		fmt.Fprintln(out, "No database is unlocked")
		return nil
	}
	if err != nil {
		return err
	}

	// The daemon only knows paths: show the bookmark names the user types.
	names := map[string]string{}
	for _, name := range cfg.DatabaseNames() {
		if resolved, err := cfg.Resolve(name); err == nil {
			names[resolved.Path] = name
		}
	}

	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "DATABASE\tPATH\tUNLOCKED\tLOCKS\tKEPT")
	var problems []string
	for _, entry := range unlocked {
		locks := "never"
		if expiry, expires := entry.ExpiresAt(); expires {
			locks = "in " + expiry.Sub(now).Round(time.Second).String()
		}

		// "open": decrypted copy ready (keep_open); "password": only the
		// master password, the file is decrypted by each command.
		kept := "password"
		if entry.Open {
			kept = "open"
		}
		if entry.Stale != "" {
			kept += " (file unreadable)"
			problems = append(problems, entry.Database+": "+entry.Stale)
		}

		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", names[entry.Database], entry.Database,
			entry.UnlockedAt.Local().Format("15:04:05"), locks, kept)
	}
	if err := table.Flush(); err != nil {
		return err
	}

	for _, problem := range problems {
		fmt.Fprintln(out, "warning:", problem)
	}
	return nil
}

// Daemon handles "zzh daemon run" (the background process itself, started
// automatically) and "zzh daemon stop".
func Daemon(args []string, ops Operations, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: zzh daemon run|stop")
	}

	switch args[0] {
	case "run":
		return ops.Run()
	case "stop":
		if err := ops.Stop(); err != nil {
			return err
		}
		fmt.Fprintln(out, "Daemon stopped, all databases locked")
		return nil
	default:
		return errors.New("usage: zzh daemon run|stop")
	}
}

// describePolicy explains when the password will be forgotten.
func describePolicy(lock config.LockConfig) string {
	idle, max := lock.IdleTimeout.Std(), lock.MaxLifetime.Std()
	switch {
	case idle == 0 && max == 0:
		return "until \"zzh lock\": no timeout configured"
	case max == 0:
		return fmt.Sprintf("locks after %s without use", idle)
	case idle == 0:
		return fmt.Sprintf("locks after %s", max)
	default:
		return fmt.Sprintf("locks after %s without use, at most after %s", idle, max)
	}
}
