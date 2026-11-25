// Package cliflags parses the "--name value" options of zzh's own commands
// (zzh add, zzh key gen, ...).
//
// The standard flag package stops at the first positional argument, so
// "zzh add web02 10.0.0.2 --user deploy" would not work; here options and
// positional arguments can be mixed freely, which is how people naturally
// type these commands. Only long options exist, to never be confused with
// the single-letter ssh options handled by sshcli.
package cliflags

import (
	"fmt"
	"strings"
)

// Spec declares the options a command accepts.
type Spec struct {
	// Values are options followed by a value ("--user deploy" or
	// "--user=deploy"). They can be repeated; all values are kept in order.
	Values []string

	// Switches are options without a value ("--password").
	Switches []string
}

// Result holds the parsed command line.
type Result struct {
	Positional []string
	values     map[string][]string
	switches   map[string]bool
}

// Value returns the last value given for an option, or "" when absent.
// Using the last one mirrors common CLI behaviour: a later option overrides
// an earlier one.
func (r Result) Value(name string) string {
	values := r.values[name]
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

// Has reports whether a value option was given at all, even with an empty
// value: "--user=" means "clear the user", which differs from not passing
// --user ("leave it as it is").
func (r Result) Has(name string) bool {
	_, given := r.values[name]
	return given
}

// Values returns every value given for a repeatable option.
func (r Result) Values(name string) []string {
	return r.values[name]
}

// Switch reports whether a switch was given.
func (r Result) Switch(name string) bool {
	return r.switches[name]
}

// Parse splits args according to spec. "--" ends option parsing, so that a
// positional argument starting with "--" can still be passed.
func Parse(args []string, spec Spec) (Result, error) {
	result := Result{values: map[string][]string{}, switches: map[string]bool{}}

	isValue := map[string]bool{}
	for _, name := range spec.Values {
		isValue[name] = true
	}
	isSwitch := map[string]bool{}
	for _, name := range spec.Switches {
		isSwitch[name] = true
	}

	for index := 0; index < len(args); index++ {
		arg := args[index]

		if arg == "--" {
			result.Positional = append(result.Positional, args[index+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "--") {
			result.Positional = append(result.Positional, arg)
			continue
		}

		name, value, hasInlineValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")

		switch {
		case isSwitch[name]:
			if hasInlineValue {
				return result, fmt.Errorf("--%s does not take a value", name)
			}
			result.switches[name] = true

		case isValue[name]:
			if !hasInlineValue {
				if index+1 >= len(args) {
					return result, fmt.Errorf("--%s requires a value", name)
				}
				index++
				value = args[index]
			}
			result.values[name] = append(result.values[name], value)

		default:
			return result, fmt.Errorf("unknown option --%s", name)
		}
	}

	return result, nil
}
