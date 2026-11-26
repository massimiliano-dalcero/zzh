// Package config loads the user's zzh settings from a JSON file stored in
// the user profile.
//
// The file only contains non-secret preferences (where the KeePass database
// lives, lock timeouts, which ssh binary to drive, ...). Secrets never go
// here: everything sensitive lives inside the .kdbx database.
//
// A missing file is not an error: zzh works with built-in defaults, and the
// file only needs to list the values the user wants to change.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	appDirName = "zzh"
	fileName   = "config.json"

	// EnvConfigPath lets the user point zzh to a different config file,
	// mainly useful for tests and for keeping several profiles side by side.
	EnvConfigPath = "ZZH_CONFIG"
)

// Config is the full set of user preferences.
type Config struct {
	// Databases are the bookmarked .kdbx files, by name. The user picks one
	// with "--db <name>"; without the flag DefaultDatabase is used.
	Databases map[string]Database `json:"databases"`

	// DefaultDatabase is the bookmark used when "--db" is not given. It may
	// be empty when exactly one database is bookmarked.
	DefaultDatabase string `json:"default_database"`

	// RootGroup is the KeePass group under which zzh looks for its
	// "Hosts" and "Keys" subgroups. It allows sharing the database with
	// other, unrelated entries. A bookmark can override it.
	RootGroup string `json:"root_group"`

	// BackupOnWrite keeps the previous version of a database next to it
	// ("<file>.bak", still encrypted) every time zzh modifies it.
	BackupOnWrite bool `json:"backup_on_write"`

	// WarnEnvPassword prints a notice on stderr whenever the master password is
	// taken from the ZZH_PASSWORD environment variable, as a reminder that it
	// is set (D-028).
	WarnEnvPassword bool `json:"warn_env_password"`

	Daemon DaemonConfig `json:"daemon"`
	Lock   LockConfig   `json:"lock"`
	SSH    SSHConfig    `json:"ssh"`
	Keys   KeysConfig   `json:"keys"`
	Output OutputConfig `json:"output"`
}

// OutputConfig controls how zzh presents its output on a terminal. Output
// sent to a pipe or a file is never colored, whatever these say, so that
// scripts and git always get plain text.
type OutputConfig struct {
	// Color is "auto" (colors on a terminal, unless the NO_COLOR environment
	// variable is set), "always" or "never".
	Color string `json:"color"`

	// Stripes gives alternate table rows a subtly different background, to
	// follow long rows across the screen. Users of light terminal themes
	// may prefer to turn it off.
	Stripes bool `json:"stripes"`
}

// LockConfig controls when the daemon forgets the unlocked database.
// A zero duration disables the corresponding limit.
type LockConfig struct {
	// IdleTimeout locks the database when no command used it for this long.
	IdleTimeout Duration `json:"idle_timeout"`

	// MaxLifetime locks the database this long after it was unlocked,
	// regardless of activity, so that a forgotten session does not stay
	// open for days.
	MaxLifetime Duration `json:"max_lifetime"`
}

// DaemonConfig controls the background process that keeps master passwords
// in memory between commands (D-033).
type DaemonConfig struct {
	// Autostart lets the first command run from a terminal ask the master
	// password and start the daemon. When false, a locked database must be
	// unlocked explicitly with "zzh unlock" and other commands refuse to run.
	Autostart bool `json:"autostart"`

	// KeepOpen makes the daemon keep the database decrypted, reloading it
	// when the file changes, so that reading commands get it instantly
	// instead of paying the key derivation each time (D-034). Writes always
	// work on the file.
	KeepOpen bool `json:"keep_open"`
}

// SSHConfig describes how the system OpenSSH client is driven.
type SSHConfig struct {
	// Binary is the ssh executable: a bare name is searched in PATH.
	Binary string `json:"binary"`

	// UseSystemConfig, when false, makes zzh run ssh with "-F none" so that
	// ~/.ssh/config and the system-wide ssh_config are ignored: the KeePass
	// entry is then the only source of truth for the connection.
	UseSystemConfig bool `json:"use_system_config"`

	// UseSystemKeys, when false, stops ssh from trying the keys zzh does not
	// manage: the default ~/.ssh/id_* files ("IdentityFile=none") and the
	// keys loaded in the system ssh-agent ("IdentityAgent=none"). Only the
	// key stored in KeePass, or the password, is then used to log in.
	UseSystemKeys bool `json:"use_system_keys"`

	// UseAskpass lets zzh answer ssh's questions: it types the password stored
	// in KeePass and relays the other questions (host key confirmations,
	// one-time codes) to the terminal. When false ssh asks them itself.
	UseAskpass bool `json:"use_askpass"`

	// PassthroughUnknownHosts makes "zzh <target>" run the plain ssh, with the
	// arguments exactly as typed, when target is neither the name nor the
	// address of a KeePass host, so that zzh can replace ssh for every host.
	PassthroughUnknownHosts bool `json:"passthrough_unknown_hosts"`

	// ExtraOptions are "Key=Value" pairs passed as "-o" to every connection.
	ExtraOptions []string `json:"extra_options"`
}

// KeysConfig holds the defaults used by "zzh key gen".
type KeysConfig struct {
	// DefaultType is one of "ed25519", "ecdsa" or "rsa".
	DefaultType string `json:"default_type"`

	// RSABits is the key size used when the type is "rsa".
	RSABits int `json:"rsa_bits"`
}

// Defaults returns the configuration used when no file exists, and the base
// on top of which a partial file is applied.
func Defaults() Config {
	return Config{
		RootGroup:       "SSH",
		BackupOnWrite:   true,
		Daemon:          DaemonConfig{Autostart: true, KeepOpen: true},
		WarnEnvPassword: true,
		Lock: LockConfig{
			IdleTimeout: Duration(15 * time.Minute),
			MaxLifetime: Duration(8 * time.Hour),
		},
		SSH: SSHConfig{
			Binary:                  "ssh",
			UseSystemConfig:         false,
			UseSystemKeys:           false,
			UseAskpass:              true,
			PassthroughUnknownHosts: true,
		},
		Keys: KeysConfig{
			DefaultType: "ed25519",
			RSABits:     4096,
		},
		Output: OutputConfig{
			Color:   "auto",
			Stripes: true,
		},
	}
}

// DefaultPath returns where the config file is expected: the ZZH_CONFIG
// environment variable when set, otherwise the per-user configuration
// directory of the operating system:
//
//	Windows: %APPDATA%\zzh\config.json
//	Linux:   $XDG_CONFIG_HOME/zzh/config.json (usually ~/.config/zzh)
//	macOS:   ~/Library/Application Support/zzh/config.json
func DefaultPath() (string, error) {
	if override := os.Getenv(EnvConfigPath); override != "" {
		return override, nil
	}

	baseDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine the user configuration directory: %w", err)
	}

	return filepath.Join(baseDir, appDirName, fileName), nil
}

// Load reads the config file at path and applies it on top of Defaults().
//
// The returned bool reports whether the file actually exists; when it does
// not, the defaults are returned without error.
//
// Unknown JSON fields are rejected on purpose: a typo such as "idle_timout"
// would otherwise be silently ignored and the user would believe a setting
// is active when it is not.
func Load(path string) (Config, bool, error) {
	cfg := Defaults()

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, fmt.Errorf("cannot read config file %s: %w", path, err)
	}

	// Decoding into the pre-filled struct means that every field missing
	// from the file keeps its default value.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&cfg); err != nil {
		return cfg, true, fmt.Errorf("invalid config file %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return cfg, true, fmt.Errorf("invalid config file %s: %w", path, err)
	}

	return cfg, true, nil
}

// Loaded is the outcome of LoadDefault: besides the configuration it keeps
// where it came from, which several commands show to the user.
type Loaded struct {
	Config Config
	Path   string
	Exists bool
}

// LoadDefault resolves the default path and loads the configuration from it.
// It is the single entry point used by every command, so that path
// resolution and error messages stay consistent. On error the returned
// Loaded still contains the path (when known) and the defaults, so that
// diagnostic commands can keep working.
func LoadDefault() (Loaded, error) {
	path, err := DefaultPath()
	if err != nil {
		return Loaded{Config: Defaults()}, err
	}

	cfg, exists, err := Load(path)
	return Loaded{Config: cfg, Path: path, Exists: exists}, err
}

// Validate checks the values that would otherwise cause confusing failures
// much later (for example when generating a key or starting the daemon).
func (c Config) Validate() error {
	if c.Lock.IdleTimeout < 0 {
		return errors.New("lock.idle_timeout cannot be negative")
	}
	if c.Lock.MaxLifetime < 0 {
		return errors.New("lock.max_lifetime cannot be negative")
	}

	if err := c.validateDatabases(); err != nil {
		return err
	}

	if strings.TrimSpace(c.SSH.Binary) == "" {
		return errors.New("ssh.binary cannot be empty")
	}

	// Extra options are passed as "-o Key=Value": a value without "=" would
	// be misread by ssh, so it is better to fail here with a clear message.
	for _, option := range c.SSH.ExtraOptions {
		if !strings.Contains(option, "=") {
			return fmt.Errorf("ssh.extra_options: %q must be in the form Key=Value", option)
		}
	}

	switch c.Output.Color {
	case "auto", "always", "never":
	default:
		return fmt.Errorf("output.color %q is not valid (use auto, always or never)", c.Output.Color)
	}

	switch c.Keys.DefaultType {
	case "ed25519", "ecdsa", "rsa":
	default:
		return fmt.Errorf("keys.default_type %q is not supported (use ed25519, ecdsa or rsa)", c.Keys.DefaultType)
	}

	// Below 2048 bits RSA is considered broken; above 16384 key generation
	// becomes impractically slow and many servers refuse the key anyway.
	if c.Keys.RSABits < 2048 || c.Keys.RSABits > 16384 {
		return fmt.Errorf("keys.rsa_bits must be between 2048 and 16384, got %d", c.Keys.RSABits)
	}

	return nil
}

// Save writes the configuration as indented JSON, creating the parent
// directory if needed. It is used by "zzh config init" to give the user a
// complete file to edit.
func Save(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode configuration: %w", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("cannot write config file %s: %w", path, err)
	}

	return nil
}

// ExpandPath resolves a leading "~" to the user home and expands environment
// variables, so that the same config file can be written portably
// (e.g. "~/vault/ssh.kdbx" or "$HOME/vault/ssh.kdbx").
func ExpandPath(path string) string {
	if path == "" {
		return path
	}

	path = os.ExpandEnv(path)

	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[1:])
		}
		// If the home directory cannot be determined the path is left
		// untouched: the later attempt to open the database will report
		// a clear "file not found" error pointing at the literal value.
	}

	return filepath.Clean(path)
}
