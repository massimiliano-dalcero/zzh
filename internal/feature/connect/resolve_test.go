package connect

import (
	"testing"

	"zzh/internal/core/vault"
	"zzh/internal/core/vault/vaulttest"
)

func TestResolveTarget(t *testing.T) {
	path := vaulttest.NewDatabase(t)
	err := vaulttest.Modify(path, func(v *vault.Vault) error {
		hosts := []vault.NewHost{
			{Name: "dd.lan", Address: "192.168.3.3", User: "max"},
			{Name: "twin-a", Address: "10.0.0.9"},
			{Name: "twin-b", Address: "10.0.0.9", Port: 2222},
		}
		for _, host := range hosts {
			if err := v.AddHost(host); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("cannot prepare database: %v", err)
	}
	v, err := vaulttest.Open(path)
	if err != nil {
		t.Fatalf("cannot open database: %v", err)
	}

	cases := []struct {
		target  string
		name    string
		found   bool
		failure bool
	}{
		{"dd.lan", "dd.lan", true, false},
		{"192.168.3.3", "dd.lan", true, false},
		{"unknown.lan", "", false, false},
		// Two entries share the address: refusing is safer than guessing.
		{"10.0.0.9", "", false, true},
	}

	for _, tc := range cases {
		host, found, err := ResolveTarget(v, tc.target)
		if (err != nil) != tc.failure || found != tc.found || host.Name != tc.name {
			t.Errorf("%s: got host=%q found=%v err=%v", tc.target, host.Name, found, err)
		}
	}
}
