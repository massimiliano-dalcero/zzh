package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSetTypedValues(t *testing.T) {
	cfg := Defaults()
	var err error

	cfg, err = Set(cfg, "ssh.use_system_keys", []string{"yes"})
	if err != nil || !cfg.SSH.UseSystemKeys {
		t.Fatalf("bool not set: %v", err)
	}

	cfg, err = Set(cfg, "lock.idle_timeout", []string{"30m"})
	if err != nil || cfg.Lock.IdleTimeout.Std() != 30*time.Minute {
		t.Fatalf("duration not set: %v", err)
	}

	cfg, err = Set(cfg, "keys.rsa_bits", []string{"3072"})
	if err != nil || cfg.Keys.RSABits != 3072 {
		t.Fatalf("number not set: %v", err)
	}

	cfg, err = Set(cfg, "ssh.extra_options", []string{"ServerAliveInterval=30", "Compression=yes"})
	if err != nil || !reflect.DeepEqual(cfg.SSH.ExtraOptions, []string{"ServerAliveInterval=30", "Compression=yes"}) {
		t.Fatalf("list not set: %v (%q)", err, cfg.SSH.ExtraOptions)
	}

	// No words empties a list.
	cfg, err = Set(cfg, "ssh.extra_options", nil)
	if err != nil || len(cfg.SSH.ExtraOptions) != 0 {
		t.Fatalf("list not emptied: %v", err)
	}
}

// Values are validated exactly like the config file.
func TestSetRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		path   string
		values []string
	}{
		{"ssh.use_system_keys", []string{"maybe"}},
		{"lock.idle_timeout", []string{"soon"}},
		{"keys.rsa_bits", []string{"1024"}},
		{"keys.rsa_bits", []string{"big"}},
		{"keys.default_type", []string{"dsa"}},
		{"ssh.extra_options", []string{"NoEquals"}},
		{"ssh.binary", []string{"a", "b"}},
		{"ssh", []string{"x"}},
		{"ssh.nope", []string{"x"}},
		{"nope", []string{"x"}},
		{"ssh.binary.deeper", []string{"x"}},
		{"databases", []string{"x"}},
		{"default_database", []string{"x"}},
	}

	for _, tc := range cases {
		if _, err := Set(Defaults(), tc.path, tc.values); err == nil {
			t.Errorf("%s %q: expected an error", tc.path, tc.values)
		}
	}
}

func TestUnsetRestoresDefault(t *testing.T) {
	cfg := Defaults()
	cfg.Lock.IdleTimeout = Duration(time.Minute)
	cfg.SSH.ExtraOptions = []string{"A=1"}

	cfg, err := Unset(cfg, "lock.idle_timeout")
	if err != nil || cfg.Lock.IdleTimeout != Defaults().Lock.IdleTimeout {
		t.Fatalf("not restored: %v", err)
	}
	// Other settings are untouched.
	if len(cfg.SSH.ExtraOptions) != 1 {
		t.Fatalf("unrelated setting changed")
	}

	if _, err := Unset(cfg, "databases"); err == nil {
		t.Fatalf("databases must be refused")
	}
}

func TestGet(t *testing.T) {
	value, err := Get(Defaults(), "lock.idle_timeout")
	if err != nil || value != `"15m0s"` {
		t.Fatalf("unexpected value %q (%v)", value, err)
	}

	section, err := Get(Defaults(), "ssh")
	if err != nil || !strings.Contains(section, `"use_system_keys": false`) {
		t.Fatalf("section not returned: %q (%v)", section, err)
	}

	_, err = Get(Defaults(), "ssh.nope")
	if err == nil || !strings.Contains(err.Error(), "use_system_keys") {
		t.Fatalf("unknown key must list the valid ones, got %v", err)
	}
}
