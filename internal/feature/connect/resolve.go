package connect

import (
	"errors"

	"zzh/internal/core/vault"
)

// ResolveTarget finds the KeePass host for what the user typed after "zzh"
// (D-030):
//
//  1. an entry whose name is target;
//  2. otherwise the single valid entry whose address is target, so that
//     "zzh 192.168.3.3" uses the entry stored as "dd.lan" for that IP.
//
// found is false when neither exists: the caller then decides whether to
// run the plain ssh. Any other problem (ambiguous name or address, invalid
// entry, unreadable database) is returned as an error, because silently
// falling back to ssh would hide it.
func ResolveTarget(v *vault.Vault, target string) (host vault.Host, found bool, err error) {
	host, err = v.FindHost(target)
	if err == nil {
		return host, true, nil
	}
	if !errors.Is(err, vault.ErrNotFound) {
		return vault.Host{}, false, err
	}

	host, err = v.FindHostByAddress(target)
	if err == nil {
		return host, true, nil
	}
	if errors.Is(err, vault.ErrNotFound) {
		return vault.Host{}, false, nil
	}
	return vault.Host{}, false, err
}
