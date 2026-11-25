package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The functions in this file let "zzh config get|set|unset" address any
// setting by its dotted JSON path ("ssh.use_system_keys", "lock.idle_timeout")
// without a hand-written list of keys.
//
// They work on the JSON form of the configuration: the key names and types
// are the ones of the JSON tags, and a modified configuration is decoded
// back with the same strict decoder and Validate used by Load. A value that
// would be rejected in the file is therefore rejected here too, with the
// same message.

// managedElsewhere are settings with a dedicated command, which keeps their
// own consistency rules (unique names, existing default...).
var managedElsewhere = map[string]string{
	"databases":        "zzh db add|rm",
	"default_database": "zzh db default",
}

// Get returns the value at path, formatted as JSON (strings with quotes,
// lists as arrays), or the whole section when path names one.
func Get(cfg Config, path string) (string, error) {
	tree, err := toTree(cfg)
	if err != nil {
		return "", err
	}

	value, err := lookup(tree, path)
	if err != nil {
		return "", err
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", fmt.Errorf("cannot encode %s: %w", path, err)
	}
	return string(data), nil
}

// Set returns cfg with the setting at path changed.
//
// values are the command line words after the key. Their meaning follows
// the type of the setting:
//   - booleans: true/false (also yes/no, on/off, 1/0);
//   - numbers: an integer;
//   - text and durations: a single word ("30m", "C:/tools/ssh.exe");
//   - lists: every word is an element, replacing the whole list.
func Set(cfg Config, path string, values []string) (Config, error) {
	if command, found := managedElsewhere[path]; found {
		return cfg, fmt.Errorf("%s is managed with \"%s\"", path, command)
	}

	tree, err := toTree(cfg)
	if err != nil {
		return cfg, err
	}

	current, err := lookup(tree, path)
	if err != nil {
		return cfg, err
	}

	newValue, err := convert(path, current, values)
	if err != nil {
		return cfg, err
	}

	if err := assign(tree, path, newValue); err != nil {
		return cfg, err
	}
	return fromTree(tree)
}

// Unset returns cfg with the setting at path restored to its default value.
func Unset(cfg Config, path string) (Config, error) {
	if command, found := managedElsewhere[path]; found {
		return cfg, fmt.Errorf("%s is managed with \"%s\"", path, command)
	}

	defaults, err := toTree(Defaults())
	if err != nil {
		return cfg, err
	}
	defaultValue, err := lookup(defaults, path)
	if err != nil {
		return cfg, err
	}

	tree, err := toTree(cfg)
	if err != nil {
		return cfg, err
	}
	if err := assign(tree, path, defaultValue); err != nil {
		return cfg, err
	}
	return fromTree(tree)
}

// toTree converts the configuration to generic JSON values (maps, slices,
// strings, float64, bool), the same shape the config file has.
func toTree(cfg Config) (map[string]any, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot encode configuration: %w", err)
	}

	var tree map[string]any
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("cannot decode configuration: %w", err)
	}
	return tree, nil
}

// fromTree decodes the generic form back into a Config, applying exactly the
// checks Load applies to the file.
func fromTree(tree map[string]any) (Config, error) {
	data, err := json.Marshal(tree)
	if err != nil {
		return Config{}, fmt.Errorf("cannot encode configuration: %w", err)
	}

	cfg := Defaults()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// lookup follows a dotted path through nested objects.
func lookup(tree map[string]any, path string) (any, error) {
	if path == "" {
		return nil, errors.New("missing setting name (see \"zzh config show\")")
	}

	var current any = tree
	for _, part := range strings.Split(path, ".") {
		object, isObject := current.(map[string]any)
		if !isObject {
			return nil, fmt.Errorf("unknown setting %q", path)
		}
		next, found := object[part]
		if !found {
			return nil, fmt.Errorf("unknown setting %q (known here: %s)", path, strings.Join(sortedKeys(object), ", "))
		}
		current = next
	}
	return current, nil
}

// assign replaces the value at a path that lookup has already validated.
func assign(tree map[string]any, path string, value any) error {
	parts := strings.Split(path, ".")
	object := tree
	for _, part := range parts[:len(parts)-1] {
		next, isObject := object[part].(map[string]any)
		if !isObject {
			return fmt.Errorf("unknown setting %q", path)
		}
		object = next
	}
	object[parts[len(parts)-1]] = value
	return nil
}

// convert parses the command line words according to the type of the
// current value. A whole section cannot be set at once: it would be too
// easy to wipe settings by mistake.
func convert(path string, current any, values []string) (any, error) {
	switch current.(type) {
	case map[string]any:
		return nil, fmt.Errorf("%s is a section: set one of its settings (%s.<name>)", path, path)

	case []any, nil:
		// A list (nil is how an empty list is encoded) takes every word.
		list := make([]any, 0, len(values))
		for _, value := range values {
			list = append(list, value)
		}
		return list, nil
	}

	if len(values) != 1 {
		return nil, fmt.Errorf("%s takes exactly one value", path)
	}
	value := values[0]

	switch current.(type) {
	case bool:
		switch strings.ToLower(value) {
		case "true", "yes", "on", "1":
			return true, nil
		case "false", "no", "off", "0":
			return false, nil
		}
		return nil, fmt.Errorf("%s must be true or false, got %q", path, value)

	case float64:
		number, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("%s must be an integer, got %q", path, value)
		}
		return number, nil

	default:
		return value, nil
	}
}

func sortedKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
