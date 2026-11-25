package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
)

// Database is a bookmarked .kdbx file.
type Database struct {
	// Path is stored as written: "~" and environment variables are expanded
	// only when the database is used (see Resolve), so that a hand-written
	// "~/vault/ssh.kdbx" survives when zzh rewrites the config file.
	Path string `json:"path"`

	// RootGroup overrides Config.RootGroup for this database only, for
	// databases organized differently. Empty means "use the global value".
	RootGroup string `json:"root_group,omitempty"`
}

// ResolvedDatabase is a bookmark ready to be opened: name selected, path
// expanded and root group decided.
type ResolvedDatabase struct {
	Name      string
	Path      string
	RootGroup string
}

// Bookmark names are typed on the command line ("--db work") and shown in
// tables, so they are limited to characters that need no quoting.
var databaseNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidateDatabaseName checks a bookmark name chosen by the user.
func ValidateDatabaseName(name string) error {
	if !databaseNamePattern.MatchString(name) {
		return fmt.Errorf("invalid database name %q: use letters, digits, '.', '_' or '-'", name)
	}
	return nil
}

// DatabaseNames returns the bookmark names in alphabetical order, so that
// listings are stable.
func (c Config) DatabaseNames() []string {
	names := make([]string, 0, len(c.Databases))
	for name := range c.Databases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Resolve selects the database to use.
//
// requested is the value of "--db" (empty when not given). Without it the
// default bookmark is used; when no default is set but exactly one database
// is bookmarked, that one is used, because there is nothing to choose.
func (c Config) Resolve(requested string) (ResolvedDatabase, error) {
	name := requested
	if name == "" {
		name = c.DefaultDatabase
	}

	if name == "" {
		switch len(c.Databases) {
		case 0:
			return ResolvedDatabase{}, errors.New(
				`no database configured: create one with "zzh init <name> <path>" or bookmark an existing one with "zzh db add <name> <path>"`)
		case 1:
			name = c.DatabaseNames()[0]
		default:
			return ResolvedDatabase{}, errors.New(
				`several databases are bookmarked and none is the default: use "--db <name>" or "zzh db default <name>"`)
		}
	}

	bookmark, found := c.Databases[name]
	if !found {
		return ResolvedDatabase{}, fmt.Errorf("database %q is not bookmarked (see \"zzh db ls\")", name)
	}

	rootGroup := bookmark.RootGroup
	if rootGroup == "" {
		rootGroup = c.RootGroup
	}

	return ResolvedDatabase{
		Name:      name,
		Path:      ExpandPath(bookmark.Path),
		RootGroup: rootGroup,
	}, nil
}

// AddDatabase bookmarks a database. The path is made absolute because a
// relative path would depend on the directory zzh is run from later.
//
// The first database ever added becomes the default, so that a new user
// does not need a separate command.
func (c *Config) AddDatabase(name, path string) error {
	if err := ValidateDatabaseName(name); err != nil {
		return err
	}
	if _, exists := c.Databases[name]; exists {
		return fmt.Errorf("database %q is already bookmarked", name)
	}

	absolute, err := filepath.Abs(ExpandPath(path))
	if err != nil {
		return fmt.Errorf("invalid path %q: %w", path, err)
	}

	if c.Databases == nil {
		c.Databases = map[string]Database{}
	}
	c.Databases[name] = Database{Path: absolute}

	if c.DefaultDatabase == "" && len(c.Databases) == 1 {
		c.DefaultDatabase = name
	}
	return nil
}

// RemoveDatabase forgets a bookmark (the file itself is never touched).
// Removing the default clears it rather than picking another one silently.
func (c *Config) RemoveDatabase(name string) error {
	if _, exists := c.Databases[name]; !exists {
		return fmt.Errorf("database %q is not bookmarked", name)
	}

	delete(c.Databases, name)
	if c.DefaultDatabase == name {
		c.DefaultDatabase = ""
	}
	return nil
}

// SetDefaultDatabase makes an existing bookmark the default.
func (c *Config) SetDefaultDatabase(name string) error {
	if _, exists := c.Databases[name]; !exists {
		return fmt.Errorf("database %q is not bookmarked", name)
	}
	c.DefaultDatabase = name
	return nil
}

// validateDatabases checks the bookmarks read from the config file.
func (c Config) validateDatabases() error {
	for name, bookmark := range c.Databases {
		if err := ValidateDatabaseName(name); err != nil {
			return fmt.Errorf("databases: %w", err)
		}
		if bookmark.Path == "" {
			return fmt.Errorf("databases.%s: path cannot be empty", name)
		}
	}

	// A default pointing nowhere would make every command fail with a
	// confusing message, so it is caught when the file is loaded.
	if c.DefaultDatabase != "" {
		if _, found := c.Databases[c.DefaultDatabase]; !found {
			return fmt.Errorf("default_database %q is not among the bookmarked databases", c.DefaultDatabase)
		}
	}

	return nil
}
