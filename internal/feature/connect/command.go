// Package connect implements "zzh <name> [ssh options] [command]": it turns
// a KeePass host entry plus the user's command line into an ssh invocation
// and runs it.
package connect

import (
	"strconv"
	"strings"

	"zzh/internal/core/config"
	"zzh/internal/core/sshcli"
	"zzh/internal/core/vault"
)

// equivalentLetters maps ssh_config keywords to the command line switch that
// sets the same thing, so that "-p 2222" typed by the user also prevents zzh
// from adding "-o Port=...". Keys are lower case, as ssh keywords are
// case-insensitive.
var equivalentLetters = map[string]byte{
	"user":         'l',
	"port":         'p',
	"proxyjump":    'J',
	"identityfile": 'i',
}

// commandBuilder accumulates the generated "-o Key=Value" options, skipping
// those the user already set and those already generated with a higher
// priority.
type commandBuilder struct {
	invocation *sshcli.Invocation
	generated  map[string]bool
	args       []string
}

func (b *commandBuilder) addOption(key, value string) {
	lowerKey := strings.ToLower(key)

	if b.invocation.HasConfigKey(lowerKey) || b.generated[lowerKey] {
		return
	}
	if letter, found := equivalentLetters[lowerKey]; found && b.invocation.HasOption(letter) {
		return
	}

	b.generated[lowerKey] = true
	b.args = append(b.args, "-o", key+"="+value)
}

// addRawOption adds a "Key=Value" option coming from KeePass or from the
// config file. Values were validated when loaded, so the "=" is present.
func (b *commandBuilder) addRawOption(option string) {
	key, value, _ := strings.Cut(option, "=")
	b.addOption(strings.TrimSpace(key), strings.TrimSpace(value))
}

// BuildArgs returns the ssh arguments (without the binary name) for
// connecting to host.
//
// useAgent tells that zzh serves the host's KeePass key through an agent
// whose endpoint is in SSH_AUTH_SOCK (see Connect).
//
// Priority, from highest to lowest (decision D-009):
//  1. options typed by the user on the command line;
//  2. the fields of the KeePass entry (address, user, port, key agent);
//  3. the "zzh.options" attribute of the entry;
//  4. "ssh.extra_options" from the config file;
//  5. the options excluding system keys ("ssh.use_system_keys" false).
//
// ssh keeps the first value it receives for each setting, so the user's
// options are placed first; on top of that zzh does not generate a setting
// that has already been given, so that "ssh -v" output stays readable.
//
// The entry name is used as the ssh destination while the real address is
// passed as HostName: ssh messages then mention the name the user typed,
// and when "use_system_config" is enabled "Host web01" blocks in
// ~/.ssh/config still match.
func BuildArgs(sshConfig config.SSHConfig, host vault.Host, invocation *sshcli.Invocation, useAgent bool) []string {
	b := &commandBuilder{
		invocation: invocation,
		generated:  map[string]bool{},
	}

	b.args = append(b.args, invocation.Options...)

	// "-F none" makes the KeePass entry the only source of truth (D-010).
	if !sshConfig.UseSystemConfig && !invocation.HasOption('F') {
		b.args = append(b.args, "-F", "none")
	}

	b.addOption("HostName", host.Address)

	// "user@name" typed on the command line beats the stored user.
	if invocation.User != "" {
		b.addOption("User", invocation.User)
	}
	if host.User != "" {
		b.addOption("User", host.User)
	}

	if host.Port != 0 {
		b.addOption("Port", strconv.Itoa(host.Port))
	}

	// The special value SSH_AUTH_SOCK makes ssh read the agent endpoint from
	// that environment variable. Passing the Windows pipe path directly in
	// "-o IdentityAgent=\\.\pipe\..." does not work: ssh's option parser
	// treats backslashes as escapes and alters the path (D-029).
	if useAgent {
		b.addOption("IdentityAgent", "SSH_AUTH_SOCK")
	}

	for _, option := range host.Options {
		b.addRawOption(option)
	}
	for _, option := range sshConfig.ExtraOptions {
		b.addRawOption(option)
	}

	// Keep ssh away from the keys zzh does not manage (D-023): the default
	// ~/.ssh/id_* files and the system ssh-agent. They come last, so a
	// per-host "zzh.options" value or a "-i"/"-o IdentityAgent=..." typed by
	// the user still wins for a single host or connection.
	if !sshConfig.UseSystemKeys {
		b.addOption("IdentityFile", "none")
		b.addOption("IdentityAgent", "none")
	}

	// "--" stops ssh's own option parsing: without it a remote command such
	// as "-weird" (typed after "--" on the zzh command line) would be taken
	// by ssh as one of its options.
	b.args = append(b.args, "--", host.Name)
	b.args = append(b.args, invocation.Command...)

	return b.args
}
