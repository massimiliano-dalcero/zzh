# zzh

**SSH connections driven by a KeePass database.**

`zzh` keeps your SSH hosts, keys and passwords in a KeePass (`.kdbx`)
database — the same kind of file you open with KeePassXC — and drives the
OpenSSH client installed on your system with them. You type `zzh web01`;
zzh finds `web01` in KeePass, hands its key and password to `ssh`, and gets
out of the way.

```console
$ zzh ls
┌─────────┬──────────┬────────┬────────┬───────────────┬────────┬──────────┬──────────┐
│ NAME    │ ALIASES  │ FOLDER │ USER   │ ADDRESS       │ KEY    │ PASSWORD │ TAGS     │
├─────────┼──────────┼────────┼────────┼───────────────┼────────┼──────────┼──────────┤
│ bastion │          │ prod   │ jump   │ 10.0.0.1:2222 │ laptop │          │ prod     │
│ web01   │ w1,front │ prod   │ deploy │ 10.0.0.10     │ laptop │ stored   │ prod,web │
│ github  │          │        │ git    │ github.com    │ github │          │          │
└─────────┴──────────┴────────┴────────┴───────────────┴────────┴──────────┴──────────┘

$ zzh web01                     # interactive shell, key from KeePass
$ zzh w1 -L 8080:localhost:80   # same host through an alias, with a tunnel
$ zzh web01 uptime              # remote command
```

- No private key, password or `known_hosts` entry needs to live in plain
  files: everything stays in the encrypted database.
- Every classic `ssh` option keeps working (`-L`, `-R`, `-D`, `-J`, `-t`,
  `-v`, `-o` …), because the real `ssh` does the connection.
- Optionally zzh can **take the place of `ssh`** for every program
  (editors, scripts, git, scp): see [shims](#zzh-in-place-of-ssh-shims).
- Works on Windows, Linux and macOS.

---

## Contents

- [Why zzh](#why-zzh)
- [How it works](#how-it-works)
- [Requirements](#requirements)
- [Installation](#installation)
- [Quick start](#quick-start)
- [The KeePass database](#the-keepass-database)
- [Connecting](#connecting)
- [Managing hosts](#managing-hosts)
- [Managing keys](#managing-keys)
- [Output: tables, colors and --grep](#output-tables-colors-and---grep)
- [The master password and the daemon](#the-master-password-and-the-daemon)
- [Several databases](#several-databases)
- [Using zzh with git](#using-zzh-with-git)
- [zzh in place of ssh (shims)](#zzh-in-place-of-ssh-shims)
- [Exporting to ~/.ssh for the plain ssh](#exporting-to-ssh-for-the-plain-ssh)
- [Configuration reference](#configuration-reference)
- [Command reference](#command-reference)
- [Security notes](#security-notes)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [Author](#author)
- [License](#license)

---

## Why zzh

A typical SSH setup spreads secrets over the disk: private keys in
`~/.ssh`, hosts in `~/.ssh/config`, passwords in your head or in a password
manager you copy from. Moving to a new machine, rotating a key or sharing a
set of hosts means copying files around and keeping them in sync.

zzh puts all of it in one encrypted KeePass database:

- **one source of truth** — hosts, users, ports, keys, passwords and options
  live in KeePass entries you can also see and edit in KeePassXC;
- **nothing secret on disk** — keys are served to `ssh` from memory, never
  written to files (unless you explicitly export them);
- **the real OpenSSH** — zzh does not reimplement SSH; it prepares the
  command line and supplies secrets through the standard OpenSSH extension
  points (ssh-agent protocol, `SSH_ASKPASS`);
- **type it once** — a small background daemon remembers the master
  password, with timeouts you choose.

---

## How it works

```
 zzh web01 -L 8080:localhost:80
   │
   ├─ parses the command line with ssh's own rules
   ├─ gets the master password: ZZH_PASSWORD, the zzh daemon, or the terminal
   ├─ finds "web01" in KeePass (by name, alias or address)
   ├─ starts, for this connection only, two private endpoints:
   │     • an ssh-agent holding only web01's key (read-only)
   │     • an askpass endpoint that answers ssh's questions
   └─ runs the system ssh:
        ssh -L 8080:localhost:80 -F none -o HostName=10.0.0.10 -o User=deploy
            -o IdentityAgent=SSH_AUTH_SOCK -o IdentityFile=none -- web01
        env SSH_AUTH_SOCK=<zzh agent>  SSH_ASKPASS=zzh  SSH_ASKPASS_REQUIRE=force
```

- **Keys** are offered to `ssh` through a temporary ssh-agent (a named pipe
  on Windows, a Unix socket elsewhere) reachable only by your user. `ssh`
  only receives signatures, never the key. The agent refuses any request to
  add, remove or export keys, and disappears when `ssh` exits.
- **Passwords** are typed by zzh: `ssh` runs zzh as its askpass program,
  and that process fetches the answer from the zzh that started `ssh`,
  through a private endpoint protected by a one-time token. The stored
  password is sent **only to the first password question**: if the server
  asks again, the password was wrong and you are asked instead, so a stale
  password never piles up failed logins.
- **Host key confirmations** (`Are you sure you want to continue connecting
  (yes/no)?`) are always shown to you; zzh never answers them on its own.
- **`~/.ssh/config` is ignored** by default (`-F none`) and so are your
  default key files and the system agent: the KeePass entry is the only
  source of truth for the connection. Both can be re-enabled.
- **Hosts not in KeePass** are handed to the plain `ssh` exactly as you
  typed them, so `zzh` can replace `ssh` for everything.
- **Other programs** can go through zzh too: with the optional
  [shims](#zzh-in-place-of-ssh-shims), a program that runs `ssh`, `scp` or
  `sftp` actually runs zzh, which behaves as described here for hosts in
  KeePass and as the plain ssh for everything else.

---

## Requirements

| | |
|---|---|
| **OpenSSH client** | 8.5 or newer (`SSH_ASKPASS_REQUIRE` from 8.4, `KnownHostsCommand` from 8.5). `zzh doctor` checks it. |
| **Windows** | The **native** Windows OpenSSH (`C:\Windows\System32\OpenSSH\ssh.exe`, or the newer `Microsoft.OpenSSH.Preview` from winget). zzh picks it automatically even when the `ssh.exe` of Git, MSYS2 or Cygwin comes first in `PATH`: see [why](#why-the-native-openssh-on-windows). |
| **KeePass database** | KDBX 4 with **Argon2d** or AES-KDF. Argon2id is not supported yet (zzh says so clearly). Databases are written only in KDBX 4. |
| **To build** | Go 1.26 or newer. |

KeePassXC is not required to use zzh, but it is the natural companion to
look at and edit the same database.

> zzh is developed and used daily on Windows. Linux and macOS are supported
> by design (Unix sockets, `setsid`, file modes) and the code builds and is
> vetted for them, but they have been exercised much less.

### Why the native OpenSSH on Windows

During a key login the server asks the client to *sign* some data with the
private key. zzh keeps the key in its own memory and lets `ssh` ask for the
signatures through the standard ssh-agent protocol; `ssh` finds the agent
through the `SSH_AUTH_SOCK` variable. What that variable points to depends
on the platform:

| Platform | Agent channel | `SSH_AUTH_SOCK` example |
|---|---|---|
| Linux, macOS | Unix socket (a special file) | `/tmp/zzh-1a2b/agent.sock` |
| Windows, native OpenSSH | named pipe (a kernel object, not a file on disk, with an ACL limited to your user) | `\\.\pipe\zzh-agent-3f9c…` |

The `ssh.exe` shipped with Git for Windows (and the ones of MSYS2 and
Cygwin) is the Unix OpenSSH built for a POSIX emulation layer. It expects a
Unix socket, and on Windows that layer *emulates* Unix sockets with a small
file holding a TCP port on `localhost` and a secret. Given a named pipe, it
cannot connect: it sees no agent, therefore no key, and the server answers
`Permission denied (publickey)`.

This matters mostly under git: git for Windows runs `core.sshCommand`
through its own shell, which puts its folders (with its `ssh.exe`) first in
`PATH`. That is why zzh, when `ssh.binary` is the default `ssh`, resolves it
itself on Windows: it skips every `ssh.exe` sitting next to `msys-2.0.dll`
or `cygwin1.dll` and falls back to `C:\Windows\System32\OpenSSH\ssh.exe`.
An explicit `ssh.binary` path is always used as given.

zzh deliberately does not imitate the MSYS/Cygwin socket emulation: its
format is an internal detail of those runtimes that may change, and it
would replace a pipe reachable only by your user with a TCP port on
`localhost` reachable by any local process, protected only by a secret
written to a file. The native OpenSSH client is part of Windows 10 (1809
and later) and Windows 11, so there is no need for it.

---

## Installation

### Prebuilt binaries

Every [release](https://github.com/massimiliano-dalcero/zzh/releases)
provides a single static executable per platform — nothing else to
install:

| Platform | File |
|---|---|
| Windows x64 / ARM64 | `zzh-windows-amd64.exe` / `zzh-windows-arm64.exe` |
| Linux x64 / ARM64 | `zzh-linux-amd64` / `zzh-linux-arm64` |
| macOS Intel / Apple Silicon | `zzh-darwin-amd64` / `zzh-darwin-arm64` |

Rename it to `zzh` (`zzh.exe` on Windows) and install it with
[`zzh install`](#installing-zzh-install). `SHA256SUMS.txt` in the release
lets you verify the download:

```sh
sha256sum -c SHA256SUMS.txt --ignore-missing            # Linux
shasum -a 256 -c SHA256SUMS.txt --ignore-missing        # macOS
```

```powershell
(Get-FileHash zzh-windows-amd64.exe -Algorithm SHA256).Hash.ToLower()   # compare with SHA256SUMS.txt
```

On Linux and macOS make it executable with `chmod +x zzh`. The binaries
are not code-signed: Windows SmartScreen and macOS Gatekeeper may warn on
the first run (on macOS: `xattr -d com.apple.quarantine zzh`).

### Installing: zzh install

zzh installs itself: run the downloaded (or built) binary with `install`.
It copies itself to a fixed place and puts that place in your PATH.

```sh
./zzh install                  # for you only, no privileges needed
sudo ./zzh install --system    # for every user of the machine
```

```powershell
.\zzh.exe install              # for you only
.\zzh.exe install --system     # for every user: from a terminal opened with "Run as administrator"
```

> In PowerShell a program in the current folder must be called as
> `.\zzh.exe`; once installed, plain `zzh` works everywhere.

| | for you (`zzh install`) | for everybody (`--system`) |
|---|---|---|
| Windows | `%LOCALAPPDATA%\Programs\zzh\zzh.exe`, folder added to your user PATH | `C:\Program Files\zzh\zzh.exe`, folder added to the system PATH |
| Linux / macOS | `~/.local/bin/zzh` | `/usr/local/bin/zzh` (already in PATH) |

- **PATH on Linux and macOS**: zzh does not edit shell profiles, because
  which file is read depends on your shell and on how it starts. If
  `~/.local/bin` is missing from your PATH, `install` prints the line to add
  (to `~/.profile` for bash, `~/.zprofile` for zsh).
- **PATH on Windows**: the change is written in the registry and announced
  to running programs, but terminals and editors **already open keep the
  old PATH**: close and reopen them.
- **Configuration and databases** live in each user's profile:
  `install` and `uninstall` never touch them.
- Add `--shim` to also make `ssh`, `scp` and `sftp` run through zzh: see
  [zzh in place of ssh](#zzh-in-place-of-ssh-shims).

### Upgrading and uninstalling

To **upgrade**, run `install` again from the new binary, with the same
options used the first time (`--system`, `--shim`). The installed zzh is
replaced; on Windows, where a running `.exe` cannot be overwritten, the old
one is moved aside as `zzh.exe.old` and removed at the next upgrade.

To **uninstall** (add `--system` for a system-wide installation):

```sh
zzh uninstall --shim          # remove only the shims, keep zzh
zzh uninstall --shim --all    # remove the shims and zzh
zzh uninstall --all           # remove zzh (refused while its shims are installed)
```

zzh is never removed while its shims are installed: `ssh` would point to a
missing file and stop working. On Windows the running `zzh.exe` is deleted
a moment after the command ends. Your configuration file and your
databases are left where they are.

### Checking the environment

After installing:

```console
$ zzh doctor
[ok  ] config: /home/max/.config/zzh/config.json not found, using defaults
[FAIL] database: none bookmarked (use "zzh init <name> <path>" or "zzh db add <name> <path>")
[ok  ] ssh: /usr/bin/ssh
[ok  ] ssh version: OpenSSH 9.6
[ok  ]   SSH_ASKPASS_REQUIRE=force (needs 8.4): passwords are supplied by zzh instead of being typed
[ok  ]   KnownHostsCommand (needs 8.5): host keys are read from KeePass instead of known_hosts
```

On a terminal the marks are colored: `ok` green, `FAIL` red, with the
commands to run highlighted.

### Building from source

```sh
git clone git@github.com:massimiliano-dalcero/zzh.git
cd zzh

# Linux / macOS
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o zzh ./cmd/zzh

# Windows (PowerShell)
$env:CGO_ENABLED = "0"; go build -trimpath -ldflags "-s -w" -o zzh.exe ./cmd/zzh
```

`-trimpath -ldflags "-s -w"` produce a smaller, reproducible binary;
`CGO_ENABLED=0` makes it fully static. Then install the result with
`./zzh install` as above.

The version is defined in one place, `releaseVersion` in
`cmd/zzh/version.go`. A binary built from a git checkout also shows the
commit it comes from (`zzh v0.9.1+4a0cc78`, `.dirty` with uncommitted
changes); add `-buildvcs=false` to get exactly the release version, as the
published binaries do.

---

## Quick start

```sh
# 1. Create a database (or bookmark an existing one: zzh db add personal ~/vault/ssh.kdbx)
zzh init personal ~/vault/ssh.kdbx

# 2. Create a key pair inside KeePass and print its public key
zzh key gen laptop --comment "me@laptop"
zzh key pub laptop            # put this line in the server's ~/.ssh/authorized_keys

# 3. Add a host that uses the key
zzh add web01 deploy@10.0.0.10 --key laptop --folder prod --tag prod

# 4. Connect
zzh web01
```

The first command that needs the database asks for the master password and
starts the zzh daemon, which remembers it (15 minutes of inactivity, at most
8 hours, by default). See [the daemon](#the-master-password-and-the-daemon).

---

## The KeePass database

### Layout

zzh only looks inside one group (default `SSH`) directly under the
database root, so the database can hold other entries too.

```
Root
└── SSH                     root_group, configurable
    ├── Hosts               one entry per host; any sub-folder structure
    │   ├── bastion
    │   └── prod
    │       └── web01
    └── Keys                one entry per key pair
        └── laptop
```

`zzh init` creates this layout. For an existing database, create the `SSH`,
`SSH/Hosts` and `SSH/Keys` groups in KeePassXC.

### Host entries

| KeePass field | Meaning |
|---|---|
| Title | the host name you type: `zzh <title>`; unique among names and aliases |
| Username | remote user |
| Password | optional login password, typed by zzh at the first password question |
| URL | `ssh://address[:port]`; also accepted: `address`, `address:port`, `user@address`, bare IPv6 |
| Tags | KeePass tags, used by `zzh ls --tag` |
| `zzh.aliases` | other names for the host, one per line |
| `zzh.key` | UUID of the key entry used to log in (set by `zzh key use`) |
| `zzh.options` | extra ssh options for this host only, one `Key=Value` per line; `#` starts a comment |

Attributes starting with `zzh.` are custom attributes, in the *Advanced*
tab of the KeePassXC entry editor.

### Key entries

| KeePass field | Meaning |
|---|---|
| Title | key name used in `zzh key …` |
| Attachment `id_<type>` | the private key, OpenSSH format, without passphrase (the database protects it) |
| `zzh.type` | `ed25519`, `ecdsa` or `rsa` |
| `zzh.public` | public key line, as in `authorized_keys`, with its comment |
| `zzh.fingerprint` | SHA256 fingerprint, as shown by `ssh-keygen -l` |

### Editing the database in KeePassXC

You can keep the database open in KeePassXC while using zzh:

- zzh **reads** the file at every command (or the daemon follows it, see
  `keep_open`), so changes saved in KeePassXC are seen immediately;
- when zzh **writes** (add, edit, key gen…), it first checks that the file
  was not changed since it read it; if KeePassXC saved meanwhile, zzh reads
  the new version and applies its change again, so nothing is lost. Enable
  *Automatically reload the database when modified externally* in KeePassXC
  so that it picks up zzh's changes;
- every change made by zzh keeps the previous version of the entry in its
  KeePass history, so it can be undone from KeePassXC;
- removed entries go to the KeePass recycle bin when the database uses one
  (KeePassXC: *Database settings → General → Use recycle bin*; `zzh init`
  enables it).

---

## Connecting

```sh
zzh [--db name] <target> [--key name] [ssh options] [command]
```

### Finding the target

`<target>` is looked up, in order:

1. the **name** or an **alias** of a host entry;
2. the **address** of a host entry (`zzh 192.168.3.1` uses the entry whose
   URL is that address), when exactly one entry has it;
3. otherwise the plain `ssh` is run with the arguments **exactly as typed**,
   with your normal ssh configuration, after a notice. Disable this with
   `zzh config set ssh.passthrough_unknown_hosts false`.

A name or address shared by several entries is an error listing them:
zzh never guesses.

### ssh options

Options can go before or after the target, as with `ssh`:

```sh
zzh web01 -L 8080:localhost:80 -N      # local forward
zzh -D 1080 bastion                    # SOCKS proxy
zzh root@web01                         # other user, for this connection only
zzh web01 -p 2222 -v                   # other port, verbose
zzh web01 --key spare                  # another KeePass key, for this connection only
zzh root@10.0.0.99 --key spare         # a host not in KeePass, with a KeePass key
zzh web01 -- ls -la /tmp               # everything after "--" is the remote command
```

`--key <name>` is zzh's own option: it uses that KeePass key instead of the
one associated with the host, without changing the entry, and also works
for hosts that are not in KeePass (the plain `ssh` then gets the key from
the zzh agent, so it still never touches the disk). Like every zzh option it
goes before the remote command: in `zzh web01 mysql --key x`, `--key x`
belongs to `mysql`. To change the key for good, use `zzh key use`.

Priority, from highest to lowest:

1. options typed on the command line;
2. the fields of the KeePass entry (address, user, port, key);
3. the entry's `zzh.options`;
4. `ssh.extra_options` from the configuration;
5. zzh's defaults (ignore `~/.ssh/config`, default key files, system agent).

### Per-host options

```sh
zzh edit github --option RequestTTY=no
zzh edit db01 --option ServerAliveInterval=30 --option Compression=yes
```

`--option` replaces the whole list; `--option=` clears it.

### Keys, passwords and host keys

- A host with a key (`zzh key use`) logs in with it through the zzh agent.
- A host with a password gets it typed automatically at the first password
  question. With both, `ssh` tries the key first and falls back to the
  password.
- Your `~/.ssh/id_*` files and the system ssh-agent are **not** used unless
  you enable them: `zzh config set ssh.use_system_keys true` (or, for one
  connection, `-i <file>` / `-o IdentityAgent=SSH_AUTH_SOCK`).
- Host keys are checked against your `known_hosts` file as usual; new ones
  are confirmed by you.

### Exit code

zzh exits with the exit code of `ssh` (and therefore of the remote command),
so `zzh web01 test -f /etc/foo && echo present` works in scripts.

---

## Managing hosts

```sh
zzh ls [--tag prod]... [--grep]                # list (filters combine)

zzh add <name> <[user@]address[:port]>         # create
        [--user u] [--port n] [--folder path] [--key name]
        [--alias a]... [--tag t]... [--option Key=Value]...
        [--password]                           # asked twice, never echoed

zzh edit <host>                                # change only what you pass
        [--rename name] [--address a] [--port n] [--user u]
        [--password | --clear-password] [--folder path]
        [--tag t]... [--option Key=Value]...
                                               # an empty value clears: --user= --port= --tag=

zzh alias add <host> <alias>...                # more names for the same entry
zzh alias rm <alias>...

zzh rm <host> [--yes]                          # to the recycle bin, after confirmation
```

`zzh ls` draws a table on a terminal and prints tab-separated lines for
`grep` in a pipe: see [Output](#output-tables-colors-and---grep).

Examples:

```sh
zzh add centauri max@192.168.3.1 --alias c --alias proxmox --password
zzh edit centauri --user root
zzh edit c --address 192.168.3.10 --port 2222
zzh edit centauri --folder lab
zzh rm centauri
```

Names and aliases cannot contain spaces or `@`, cannot start with `-`, and
cannot be a zzh command (`ls`, `add`, `key`…). `zzh ls` shows whether a
password is stored, never the password itself.

---

## Managing keys

```sh
zzh key gen <name> [--type ed25519|ecdsa|rsa] [--bits n] [--comment text] [--folder path]
zzh key import <name> <file|-> [--comment text] [--folder path]
zzh key ls                                    # keys and the hosts using them
zzh key pub <name>                            # the public key line, nothing else
zzh key use <host> <key>                      # log in to <host> with <key>
zzh key use <host> --none                     # remove the association
zzh key rm <name> [--force] [--yes]           # refused while in use, unless --force
```

- **Generated** keys never touch the disk: they are created in memory and
  stored in KeePass. The default type is ed25519 (`keys.default_type`), RSA
  uses `keys.rsa_bits` (4096) unless `--bits`. The comment defaults to the
  key name and ends up in `authorized_keys`, which helps recognizing the key
  on servers.
- **Imported** keys can be OpenSSH or PEM (RSA, EC, PKCS#8). An encrypted
  key is decrypted once (the passphrase is asked) and stored without
  passphrase, like generated ones. The comment is taken from the `.pub` file
  next to the private key when it matches. `-` reads the key from the
  terminal, so a key received by mail or chat never needs to be saved to a
  file. PuTTY `.ppk` files must be converted first (PuTTYgen → *Conversions →
  Export OpenSSH key*). zzh never deletes the source files.
- **Installing** a key on a server is up to you for now:
  `zzh key pub <name>` prints the line to append to `~/.ssh/authorized_keys`.

```sh
zzh key pub laptop | ssh deploy@10.0.0.10 'cat >> ~/.ssh/authorized_keys'
```

---

## Output: tables, colors and --grep

zzh adapts its output to where it goes.

**On a terminal**, listings are tables with borders, a bold header and
alternate rows on a slightly different background, and colors carry a
meaning:

| Color | Meaning | Examples |
|---|---|---|
| red | errors, failed checks, broken entries | `zzh:` before an error, `[FAIL]`, `INVALID`, `MISSING KEY`, `MISSING` |
| yellow | warnings, things to notice | `zzh:` before a notice, `unused` keys |
| green | success, passed checks | `[ok]`, the default database |
| cyan | commands to type, tags | `"zzh unlock"` in a message, host tags |
| **magenta** | a stored login password | **`stored`** in `zzh ls` (the password itself is never shown) |
| bold | names | hosts, keys, databases, table headers |
| dim | secondary details | folders, paths, fingerprints, borders |

**In a pipe or a file**, or with `--grep`, listings become one line per
row, fields separated by a tab, `-` for empty fields (so that `awk` and
`cut` never shift columns), no header and no colors:

```sh
zzh ls | grep stored                 # hosts with a stored password
zzh ls --grep | cut -f1,5            # name and address
zzh key ls | grep unused             # keys no host uses
```

| Listing | Columns | Highlights |
|---|---|---|
| `zzh ls [--tag t]... [--grep]` | name, aliases, folder, user, address, key, password, tags | `stored`, broken entries |
| `zzh key ls [--grep]` | name, type, fingerprint, hosts | `unused` |
| `zzh db ls [--grep]` | default (`*`), name, path, status | default, `MISSING` files |
| `zzh status [--grep]` | database, path, unlocked at, locks in, kept | unreadable file |

Colors are **never** written to pipes or files, so scripts and git always
get plain text, nor on output meant to be copied (`zzh key pub`). They can
be turned off with the standard `NO_COLOR` environment variable, or with
`zzh config set output.color never` (`always` forces them, `auto` is the
default). On a light terminal theme the striped rows may read badly:
`zzh config set output.stripes false`. On Windows the classic console is
switched to color mode automatically; if that is not possible, zzh prints
plain text.

---

## The master password and the daemon

### Where the password comes from

For every command, in order:

1. the `ZZH_PASSWORD` environment variable, when set;
2. the **zzh daemon**, if it holds the password;
3. the terminal: you are asked, and the password is handed to the daemon
   (started automatically) so you are not asked again.

The question is asked on the terminal you are using even when the standard
input and output of zzh are redirected, as when git runs it: like OpenSSH,
zzh falls back to the controlling terminal (`/dev/tty`, or the console on
Windows). So `git push` typed in a terminal asks the password there.

Connections started by programs with no terminal anyone can see — git
started by an editor in the background, scheduled tasks — ask the password
in the [confirmation dialog](#when-another-program-runs-ssh-the-confirmation-dialog)
when there is a graphical session. Without one (cron, services, servers)
they need an unlocked database or `ZZH_PASSWORD`; otherwise they stop with
a message asking for `zzh unlock`. On Windows a console counts as visible when it has a visible
window or is a pseudo console (Windows Terminal, the VS Code terminal, an
ssh session); a hidden console never receives questions, so a background
program is never left waiting for an answer nobody can give.

Through the [ssh shim](#zzh-in-place-of-ssh-shims) the rules differ in one
point: a database that is not unlocked (Enter pressed at the question,
wrong password, nobody to ask) leads to the plain ssh, not to an error.

### The daemon

```sh
zzh unlock          # ask the password now and keep it (e.g. before using git)
zzh status          # which databases are unlocked and when they lock
zzh lock            # forget every password now (zzh --db work lock: only one)
zzh daemon stop     # stop the daemon
```

```console
$ zzh status
┌──────────┬──────────────────────────┬──────────┬──────────┬──────┐
│ DATABASE │ PATH                     │ UNLOCKED │ LOCKS    │ KEPT │
├──────────┼──────────────────────────┼──────────┼──────────┼──────┤
│ personal │ /home/max/vault/ssh.kdbx │ 10:42:13 │ in 14m3s │ open │
└──────────┴──────────────────────────┴──────────┴──────────┴──────┘
```

- It is a `zzh daemon run` process started in the background, detached from
  the terminal: closing the terminal does not stop it, and it shows no
  window. It exits by itself when it holds nothing any more.
- It listens on a private endpoint with a random name, published in a file
  only you can read (`%LOCALAPPDATA%\zzh\daemon.json`,
  `~/.cache/zzh/daemon.json`, `~/Library/Caches/zzh/daemon.json`).
- **Timeouts**: `lock.idle_timeout` (15 minutes without use) and
  `lock.max_lifetime` (8 hours in any case). `0` disables a limit. Changes
  apply immediately, also to an already unlocked database:

  ```sh
  zzh config set lock.idle_timeout 2h
  zzh config set lock.max_lifetime 0
  ```

- **Automatic start**: with `daemon.autostart` set to `false`, a locked
  database is never unlocked implicitly; commands ask you to run
  `zzh unlock`.
- If the master password is changed in KeePassXC, the daemon forgets the old
  one and asks for `zzh unlock`.

### keep_open: instant commands

Opening a KeePass file takes about a second on purpose: the key derivation
(Argon2) is what makes guessing the master password expensive. With
`daemon.keep_open` (default on) the daemon keeps the database open and
hands each reading command a ready-to-use copy, so `zzh ls`, `zzh key ls`
and connections start instantly.

- The daemon follows the file like KeePassXC does: before every answer, and
  every 5 seconds, it compares the file's SHA-256 with the version it holds
  and reloads it when it changed (saved by KeePassXC, synced from another
  computer, written by zzh).
- If the file cannot be decrypted for a moment (a sync client writing it),
  the last good version keeps being served, with a warning. The password is
  forgotten only if the same content keeps refusing it in 3 checks.
- Writes never go through the daemon: they always open the real file, with
  the safe write described in [Security notes](#security-notes).
- Turn it off with `zzh config set daemon.keep_open false`: the daemon then
  keeps only the password, and every command decrypts the file itself.

### ZZH_PASSWORD

For scripts, or by choice, the master password can come from `ZZH_PASSWORD`.
zzh reads it at startup and **removes it from its environment**, so no
program it starts (ssh included) inherits it. A notice is printed at every
use (`zzh config set warn_env_password false` to silence it).

Be aware that environment variables can be read by other programs running as
your user, and a variable set permanently is stored in clear text (registry
or shell profile). Prefer setting it for the current session only, without
leaving it in the shell history:

```powershell
$env:ZZH_PASSWORD = [Net.NetworkCredential]::new('', (Read-Host -AsSecureString "KeePass")).Password
```

```sh
read -rs ZZH_PASSWORD && export ZZH_PASSWORD
```

---

## Several databases

Databases are bookmarked by name in the configuration file.

```sh
zzh init work ~/work/servers.kdbx      # create and bookmark
zzh db add personal ~/vault/ssh.kdbx   # bookmark an existing file
zzh db ls                              # * marks the default
zzh db default work
zzh db rm personal                     # forgets the bookmark, keeps the file

zzh --db personal ls                   # use another database for one command
zzh --db personal web01
```

`--db` must come right after `zzh`. The first bookmark becomes the default;
with a single bookmark and no default, that one is used.

`zzh init` creates the database with the zzh groups, never overwrites an
existing file, and tunes the key derivation like KeePassXC: Argon2d with
64 MiB of memory and as many iterations as fit in about one second on your
machine.

---

## Using zzh with git

git can use zzh as its SSH transport, so pushes and pulls authenticate with
keys stored in KeePass:

```sh
# 1. Store the key and the host
zzh key gen github --comment "me@zzh"
zzh key pub github          # add it in GitHub: Settings → SSH and GPG keys
zzh add github git@github.com --key github
zzh edit github --option RequestTTY=no

# 2. Test (accept GitHub's host key after checking its fingerprint)
zzh github                  # "Hi <user>! You've successfully authenticated..."

# 3. Tell git to use zzh, for one repository...
git config core.sshCommand "/path/to/zzh"
git config ssh.variant ssh
# ...or for all of them: git config --global ...

# 4. Use git: from a terminal the password is asked there if needed
git push
# Tools that run git in the background (an editor's Sync button) show the
# zzh confirmation dialog, which also asks the password when needed
```

On Windows use forward slashes in the path, e.g.
`C:/Users/me/AppData/Local/Programs/zzh/zzh.exe`.

With the [shims](#zzh-in-place-of-ssh-shims) installed, step 3 is not
needed: git runs `ssh`, which is zzh (on Windows through `GIT_SSH`).

### How git ends up running zzh

For a remote in SSH form (`git@github.com:user/repo.git`), git needs an ssh
connection. Instead of running `ssh`, it runs the program set in
`core.sshCommand`, with the arguments it would give to ssh:

```
zzh -o SendEnv=GIT_PROTOCOL git@github.com "git-receive-pack 'user/repo.git'"
```

zzh reads them exactly as ssh would:

1. `-o SendEnv=GIT_PROTOCOL` is an ssh option: passed on unchanged;
2. `git@github.com` is the destination: user `git`, target `github.com`;
3. the rest is the remote command that git runs on the server.

`github.com` is not the name of an entry, but it is the address of the
`github` entry, so zzh uses that one, with its key served by the zzh agent.
It then runs the real `ssh`, and the git protocol flows through standard
input and output as with plain ssh.

`ssh.variant ssh` tells git that the program understands OpenSSH options
(`-p`, `-o` …). Without it git does not recognize the name `zzh` and probes
it with `zzh -G` before every operation to find out what kind of client it
is.

### Where the setting applies

git chooses the ssh program in this order: the `GIT_SSH_COMMAND` environment
variable, then `core.sshCommand`, then `GIT_SSH`, then plain `ssh`.

| Scope | How | Notes |
|---|---|---|
| One repository | `git config core.sshCommand …` (inside the repository) | stored in its `.git/config`; other repositories keep using `ssh` |
| All repositories | `git config --global core.sshCommand …` | hosts not in KeePass still work: zzh hands them to the plain `ssh`. Put zzh in a stable path, since every repository depends on it |
| One command | `GIT_SSH_COMMAND=zzh git fetch` | nothing is stored |

Check what a repository uses with:

```sh
git config --show-origin --get-regexp "core.sshcommand|ssh.variant"
```

In PowerShell, for one command:

```powershell
$env:GIT_SSH_COMMAND = "C:/path/to/zzh.exe"; git fetch; Remove-Item Env:GIT_SSH_COMMAND
```

---

## zzh in place of ssh (shims)

### What a shim is

Typing `zzh web01` works for you, but other programs know nothing about
zzh: an editor, a deployment script, Ansible, `scp`, git all run `ssh`. A
**shim** is a stand-in placed where those programs look for `ssh`: a file
named `ssh` (`ssh.exe` on Windows) that is actually a link to zzh, put in a
folder that comes **before** the real OpenSSH in PATH. Every program that
runs `ssh` then runs zzh, without being configured, and zzh hands the
connection to the real ssh.

zzh installs three shims: `ssh`, `scp` and `sftp`.

### How it works

```
 program runs "ssh web01 uptime"
   │
   ├─ the system finds the first "ssh" in PATH: the shim, a link to zzh
   ├─ zzh sees it was started as "ssh" and behaves as ssh:
   │     web01 is in KeePass  → address, user, port, options, key and
   │                            password come from KeePass, as "zzh web01"
   │     anything else        → the real ssh, arguments unchanged, silently
   └─ zzh finds the real ssh further down PATH (skipping itself) and runs it
```

- **The name decides**: zzh checks the name it was started with. As `ssh`
  it reads the command line exactly as ssh does, and zzh's own subcommands
  are not recognized: `ssh ls` is the host `ls`, as it always was.
- **Invisible when it has nothing to do**: unknown hosts, `ssh -V`, users
  who never set up zzh (with `--system` the shim serves every user of the
  machine, root included) all get the real ssh with no message at all.
- **scp and sftp** do not look for ssh in PATH (the Windows ones run the
  `ssh.exe` of their own folder), so their shims run the real `scp` or
  `sftp` with `-S <zzh>`: the copy is done by OpenSSH, the connection by
  zzh. A `-S` you pass yourself is respected.
- **Links, not copies**: the shims are symbolic links, so after an upgrade
  they run the new zzh at once.
- **Loops are impossible**: zzh never picks itself, nor the shim of
  another zzh installation (a link to a file named `zzh`), as "the real
  ssh", and a counter stops it if a plain copy of zzh is found in PATH.

### Installing the shims

```sh
zzh install --shim                    # Linux / macOS, for you
sudo zzh install --system --shim      # Linux / macOS, for every user
```

```powershell
zzh install --system --shim           # Windows, from a terminal opened as administrator
```

| | Where the shims go | How they come first |
|---|---|---|
| Linux / macOS | `ssh`, `scp`, `sftp` next to zzh, in `~/.local/bin` or `/usr/local/bin` | `/usr/local/bin` already comes before `/usr/bin`; for `~/.local/bin` zzh tells you if your PATH needs fixing |
| Windows | `C:\Program Files\zzh\shim\ssh.exe`, `scp.exe`, `sftp.exe` | the folder is inserted in the system PATH right before `C:\Windows\System32\OpenSSH`; the system variable `GIT_SSH` is set to the ssh shim |

On **Windows the shims need `--system`**. Windows builds the PATH of every
program as *system PATH + user PATH*, and the system part already contains
`System32\OpenSSH`: a shim in a user folder would always come second.
`zzh install --shim` without `--system` therefore stops and explains this.

**Git for Windows** puts its own folders first in PATH and would run its own
`ssh.exe`; `GIT_SSH` makes it use the shim instead. A `GIT_SSH` you already
set to something else is left alone. Note that git gives `core.sshCommand`
priority over `GIT_SSH`: a repository configured with
`core.sshCommand` keeps using what it says.

Programs started **before** the installation keep the old PATH: restart
them (terminals, editors, Windows Terminal).

zzh never replaces a file that is not a link to zzh: if an `ssh` you put
there yourself already exists where a shim should go, the installation
stops before changing anything and tells you.

Check that it works, in a **new** terminal:

```sh
where.exe ssh        # Windows: the shim must be listed first
command -v ssh       # Linux / macOS
ssh -G web01         # a KeePass host: prints the configuration taken from KeePass
```

### Using it, and its options

Use `ssh`, `scp` and `sftp` as usual; KeePass hosts just work:

```sh
ssh web01                          # key and password from KeePass
ssh -L 8080:localhost:80 w1        # aliases and every ssh option
scp backup.tar web01:/tmp/         # copies through zzh too
```

Two prefixes, valid only as the **first** argument of `ssh` (they can never
collide with ssh: it has no long options, and a lone `-` could only be a
host named `-`):

| You type | It runs |
|---|---|
| `ssh --zzh <zzh arguments>` | zzh itself: `ssh --zzh ls` is `zzh ls`, `ssh --zzh --db work key ls` is `zzh --db work key ls` |
| `ssh - <ssh arguments>` | the real ssh at once, without zzh: `ssh - web01` |

Turning the shims off, without uninstalling them; zzh then runs the real
program at once, without opening the database and without messages:

| Scope | How |
|---|---|
| one command | `ssh - …` (above) |
| a terminal session or a script, and every program it starts | `ZZH_SHIM=off` (also `0`, `false`, `no`), for ssh, scp and sftp |
| always, for your user | `zzh config set shim.enabled false` (with `--system`, each user can opt out alone) |

```powershell
$env:ZZH_SHIM = "off"     # PowerShell, this session
```

```sh
ZZH_SHIM=off ./deploy.sh  # a script that must use the plain ssh
```

**The master password through the shim.** When the database is locked and
there is a terminal, zzh asks for the master password, and the question
offers a way out:

```
KeePass password for "personal" (/home/max/vault/ssh.kdbx) [Enter: plain ssh]:
```

Pressing Enter without typing skips zzh: the real ssh runs at once. A
wrong password, a damaged database or a database nobody can unlock now
also end with one notice and the plain ssh, instead of an error: through
the shim, zzh never stops a connection the plain ssh could make.

### When another program runs ssh: the confirmation dialog

A program that runs `ssh` in the background — the git of an editor, a
scheduled task, a sync tool — has no terminal you can see. Before giving
it the keys and passwords stored in KeePass, zzh asks **you**, with a
dialog:

```
┌ zzh ──────────────────────────────────────────────────────────────┐
│ git.exe (started by Code.exe) wants to connect to "github" with   │
│ the keys and passwords stored in KeePass.                         │
│                                                                   │
│ Program: C:\Program Files\Git\cmd\git.exe                         │
│                                                                   │
│   [ Allow once ]   [ Allow for 15 minutes ]   [ Deny ]            │
└───────────────────────────────────────────────────────────────────┘
```

- **Who is asking**: zzh walks up the chain of programs that led to the
  connection, skipping zzh, ssh, scp and sftp themselves, and names the
  first real program and the one that started it. The full path lets you
  tell a genuine program from a look-alike in another folder.
- **Allow once**: this connection only.
- **Allow for 15 minutes**: that program may connect to **that host**
  without asking again for the given time (`confirm.remember_for`). Another
  host, or another program, is asked again. `zzh lock` forgets every such
  authorization, together with the passwords.
- **Deny**, closing the dialog, or no answer within **60 seconds**: the
  KeePass data is not used; through the shim the plain ssh runs instead,
  as `zzh <host>` the command fails.

**When the database is locked**, the same dialog also has a field for the
master password: one dialog unlocks and allows. The password is then kept
by the daemon as if you had typed it in a terminal.

When the dialog appears is set by `confirm.mode`:

| `confirm.mode` | Asks |
|---|---|
| `background` (default) | only programs without a visible terminal; what you type yourself in a terminal is never asked |
| `always` | every connection that uses KeePass data; in a terminal the question is asked there (`[y/N]`) |
| `never` | nobody, and no zzh dialog at all (not even for the password): background programs use KeePass data whenever the database is unlocked |

Connections that do not use KeePass data (hosts not in KeePass, without
`--key`) never ask anything.

The dialog is drawn by the system's own tools: a WinForms window through
the Windows PowerShell on Windows, `osascript` on macOS, `zenity` or
`kdialog` on Linux. It is never shown where nobody could see it: not in a
Windows service (session 0), not in an ssh session into a Mac, not on Linux
without a display or without zenity/kdialog. In those cases, with
`confirm.mode background`, a program without a terminal **does not** get
the KeePass data (through the shim it runs the plain ssh, with a notice);
servers that run unattended jobs with an unlocked database can use
`confirm.mode never`.

`zzh doctor` shows the mode and whether the dialog is available in the
current session.

### Removing the shims

```sh
zzh uninstall --shim                 # for you
zzh uninstall --system --shim        # system-wide (sudo / administrator)
zzh uninstall --system --shim --all  # the shims and zzh itself
```

On Windows this also removes the shim folder from the system PATH and
`GIT_SSH` (only if it still points to the shim). zzh only removes links to
zzh, never other files, and cannot be removed while its shims are
installed.

---

## Exporting to ~/.ssh for the plain ssh

Sometimes you want the plain `ssh` — or tools that only know it, like VS
Code Remote-SSH — to see your KeePass hosts too:

```sh
zzh sync out --dry-run     # show what would change
zzh sync out               # write it
ssh web01                  # now works without zzh
```

What it writes, in `~/.ssh` (or `--dir <folder>`):

- for every key, `<name>` (private) and `<name>.pub`;
- `zzh_config`, owned by zzh, with one `Host` block per host (aliases on the
  same `Host` line, `IdentityFile` + `IdentitiesOnly` for keys, and the
  per-host options);
- one line, `Include zzh_config`, added once at the top of your
  `~/.ssh/config`. Everything else in your config is kept; for a host
  defined in both places the KeePass values win.

A file that already exists with different content is renamed to
`<file>_<n>.bkp` before being replaced; identical files are left alone.
Private files are made readable by you only (mode 0600; an explicit
owner-only ACL on Windows, which the Windows OpenSSH client requires).

> **This is the one command that writes private keys to disk**, in clear
> text, protected only by file permissions. Passwords cannot be exported:
> OpenSSH has no configuration option for them.

---

## Configuration reference

The configuration is a JSON file, optional (defaults apply when it is
missing):

| OS | Path |
|---|---|
| Windows | `%APPDATA%\zzh\config.json` |
| Linux | `~/.config/zzh/config.json` |
| macOS | `~/Library/Application Support/zzh/config.json` |

`ZZH_CONFIG` points to another file. `zzh config path` prints the one in
use, `zzh config show` the effective values, `zzh config init` writes a
complete file to edit.

Every setting can be changed without opening the file; values are checked
exactly as in the file:

```sh
zzh config get lock.idle_timeout
zzh config set lock.idle_timeout 30m
zzh config set ssh.extra_options ServerAliveInterval=30 Compression=yes   # a list: replaces it
zzh config unset lock.idle_timeout                                        # back to the default
```

| Setting | Default | Meaning |
|---|---|---|
| `databases` | — | bookmarked databases (managed with `zzh init` / `zzh db`) |
| `default_database` | — | bookmark used without `--db` (`zzh db default`) |
| `root_group` | `SSH` | group containing `Hosts` and `Keys`; a bookmark can override it |
| `backup_on_write` | `true` | keep the previous database file as `<file>.bak` at every write |
| `warn_env_password` | `true` | notice when the password comes from `ZZH_PASSWORD` |
| `daemon.autostart` | `true` | a password typed on the terminal starts the daemon |
| `daemon.keep_open` | `true` | the daemon keeps the database open (instant reads) |
| `lock.idle_timeout` | `15m` | forget the password after this time without use (`0`: never) |
| `lock.max_lifetime` | `8h` | forget it this long after unlocking, in any case (`0`: never) |
| `ssh.binary` | `ssh` | the OpenSSH client to drive |
| `ssh.use_system_config` | `false` | read `~/.ssh/config` too (otherwise `-F none`) |
| `ssh.use_system_keys` | `false` | allow `~/.ssh/id_*` and the system ssh-agent |
| `ssh.use_askpass` | `true` | let zzh type passwords and relay ssh's questions |
| `ssh.passthrough_unknown_hosts` | `true` | run the plain ssh for targets not in KeePass |
| `ssh.extra_options` | `[]` | `Key=Value` options added to every connection |
| `keys.default_type` | `ed25519` | type for `zzh key gen` |
| `keys.rsa_bits` | `4096` | RSA size for `zzh key gen --type rsa` |
| `output.color` | `auto` | colors on a terminal (`auto`, also off when `NO_COLOR` is set), `always` or `never`; pipes and files never get colors |
| `output.stripes` | `true` | alternate background for table rows (turn off on light themes if it reads badly) |
| `shim.enabled` | `true` | `false`: the ssh, scp and sftp shims run the real programs for you, as if zzh were not there |
| `confirm.mode` | `background` | when a program must be confirmed before using KeePass data: `background` (no visible terminal), `always`, `never` |
| `confirm.remember_for` | `15m` | how long "Allow for a while" lasts, per program and host |

Durations are written as `30s`, `15m`, `8h`, `1h30m`.

Example:

```json
{
  "databases": {
    "personal": { "path": "~/vault/ssh.kdbx" },
    "work": { "path": "C:/work/servers.kdbx", "root_group": "Servers" }
  },
  "default_database": "personal",
  "lock": { "idle_timeout": "1h", "max_lifetime": "0s" },
  "ssh": { "extra_options": ["ServerAliveInterval=30"] },
  "output": { "color": "auto", "stripes": false },
  "shim": { "enabled": true },
  "confirm": { "mode": "background", "remember_for": "30m" }
}
```

Environment variables:

| Variable | Effect |
|---|---|
| `ZZH_CONFIG` | use another configuration file |
| `ZZH_PASSWORD` | master password, see [ZZH_PASSWORD](#zzh_password) |
| `ZZH_SHIM` | `off` (also `0`, `false`, `no`): the shims run the real ssh, scp, sftp |
| `NO_COLOR` | any value: no colors ([no-color.org](https://no-color.org)) |
| `ZZH_DAEMON_DIR` | where the daemon publishes its endpoint (tests, special setups) |

`ZZH_AS_SSH` and `ZZH_SHIM_DEPTH` are used by zzh internally between its
own processes (scp and sftp through the shims, loop protection): do not
set them.

---

## Command reference

| Command | |
|---|---|
| `zzh <target> [--key k] [ssh options] [command]` | connect (name, alias, address; otherwise plain ssh); `--key` picks a KeePass key for this connection |
| `zzh ls [--tag t]... [--grep]` | list hosts |
| `zzh add …` / `zzh edit …` / `zzh rm <host>` | create, change, remove hosts |
| `zzh alias add <host> <alias>...` / `zzh alias rm <alias>...` | host aliases |
| `zzh key gen\|import\|pub\|use\|rm …` | keys |
| `zzh key ls [--grep]` | list keys and the hosts using them |
| `zzh sync out [--dir path] [--dry-run]` | export to `~/.ssh` |
| `zzh unlock` / `zzh lock` / `zzh daemon stop` | master password and daemon |
| `zzh status [--grep]` | unlocked databases and when they lock |
| `zzh init <name> <path>` | create a database |
| `zzh db add\|rm\|default …` | bookmarked databases |
| `zzh db ls [--grep]` | list bookmarked databases |
| `zzh config path\|show\|init\|get\|set\|unset …` | configuration |
| `zzh install [--system] [--shim]` | install or upgrade zzh; `--shim`: ssh, scp and sftp run through zzh |
| `zzh uninstall [--system] --shim [--all]` / `zzh uninstall [--system] --all` | remove the shims and/or zzh |
| `zzh doctor` | check ssh, configuration and databases |
| `zzh version` / `zzh help` | |

`--db <name>`, right after `zzh`, selects a database for the commands that
use one. `--grep` prints one tab-separated line per row instead of a table
(automatic in a pipe). `zzh help` shows every option.

With the shims installed:

| Command | |
|---|---|
| `ssh`, `scp`, `sftp` … | as usual; KeePass hosts get their data from KeePass |
| `ssh --zzh <zzh arguments>` | run zzh (`ssh --zzh ls`) |
| `ssh - <ssh arguments>` | run the real ssh, skipping zzh |

---

## Security notes

What zzh protects, and how:

- **Private keys** stay in the encrypted database and in the memory of zzh
  processes. During a connection they are used through an ssh-agent that
  only signs; `ssh` never sees them. The only exception is
  `zzh sync out`, which you run on purpose.
- **Local endpoints** (agent, askpass, daemon) are named pipes with an ACL
  limited to your user SID on Windows, Unix sockets in `0700` directories
  elsewhere, always with random names. Askpass requests also carry a
  one-time token.
- **Safe writes**: zzh never edits the database in place. It encodes the new
  version in memory, decrypts it again to verify it, checks that nobody
  changed the file meanwhile (otherwise it re-reads and re-applies), keeps
  the previous file as `.bak`, writes to a temporary file and renames it
  over the database, so the file is always either the old or the new
  version.
- **Fresh randomness at every save**: master seed, encryption IV, KDF salt
  and inner stream key are regenerated each time, as KeePass does. (The
  underlying KDBX library would otherwise reuse them, encrypting different
  contents with the same key and nonce.)
- **Strong key derivation for new databases**: Argon2d, 64 MiB, calibrated
  to about one second (the library's own defaults are far weaker).
- **The daemon** keeps master passwords (and, with `keep_open`, the
  decrypted database) in memory only, and forgets them after the configured
  timeouts. The copy it hands to commands is re-encrypted with a random
  256-bit one-time key and never written to disk.

- **Questions only where you can see them**: passwords and confirmations
  are asked on the terminal you are using (also when git redirects zzh's
  input and output), never on a hidden console, and never read from a pipe.
- **The shims** change what `ssh` means for every program, and with
  `--system` for every user of the machine: they only ever add KeePass data
  for hosts you stored; for everything else, and for users without zzh,
  the real ssh runs unchanged. `install` never replaces, and `uninstall`
  never deletes, a file that is not a link to zzh. On Windows the registry
  PATH keeps its type (`REG_EXPAND_SZ`), so entries like `%SystemRoot%`
  keep working.

- **Programs in the background** are confirmed by you before they use the
  KeePass data (`confirm.mode`), in a dialog that names the program and its
  path; authorizations are per program and host, limited in time, and
  cleared by `zzh lock`. The dialog's texts reach the helper program
  through its environment, not its command line, and the password comes
  back through a private pipe. The confirmation protects from programs
  acting behind your back, not from malware running as your user, which
  could fake the program's name or click the dialog itself.

What zzh does **not** protect against: code already running as your user
can read process memory and reach your private endpoints, exactly as with
ssh-agent, KeePassXC or any password manager. Lock the database
(`zzh lock`) when you leave the machine unattended.

---

## Troubleshooting

**`zzh doctor`** is the first thing to run: it checks the configuration,
the bookmarked databases and the ssh client.

| Symptom | Cause and fix |
|---|---|
| `... is not the native Windows OpenSSH` | `ssh.binary` points explicitly to an MSYS/Cygwin build (e.g. Git's). Use `zzh config unset ssh.binary` to let zzh find the native one, or set it to `C:/Windows/System32/OpenSSH/ssh.exe` |
| `Permission denied (publickey)` only under git | git runs zzh with its own folders first in `PATH`. zzh skips Git's `ssh.exe` there, unless `ssh.binary` points to it explicitly: check with `zzh doctor`. |
| `the database uses the Argon2id key derivation` | Not supported yet. In KeePassXC: *Database settings → Security → Encryption settings*, switch to Argon2d. |
| `the database is locked ... there is no terminal` | A command with no visible terminal (a script, a scheduled task, git run by an editor). Run `zzh unlock` first, or set `ZZH_PASSWORD`. |
| `cannot decrypt the database: wrong password or corrupted file` | Wrong master password. If it says it came from `ZZH_PASSWORD`, the variable is stale or belongs to another database. |
| `host "x" not found` / plain ssh runs | `x` is neither a name, an alias nor an address in KeePass: check `zzh ls`. |
| `... is used by more than one entry` | Two entries share a name or an alias (possibly created in KeePassXC): rename one. |
| `the database file was modified by another program` | Someone kept saving the file while zzh was writing; run the command again. |
| ssh asks for a password although a key is set | The public key is not in the server's `authorized_keys` (`zzh key pub <key>`), or the server refuses it. |
| `zzh` "not recognized" in PowerShell | Use `.\zzh.exe` in the current folder, or install it with `.\zzh.exe install` and open a new terminal. |
| The daemon keeps running | It stops by itself when nothing is unlocked; `zzh daemon stop` stops it now. |
| `--system needs administrator rights` / `needs root` | Run it from a terminal opened with *Run as administrator* (Windows) or with `sudo`. |
| `on Windows the ssh shim can only be installed with --system` | Per-user shims cannot come before the system OpenSSH on Windows: use `zzh install --system --shim`. |
| `cannot install the shims: … is not a link to zzh` | A program of yours already sits where a shim goes; zzh does not replace it. Move it, or keep it and do without the shims. |
| `ssh` does not go through zzh after installing the shims | The program was started before the installation and has the old PATH: restart it. Check with `where.exe ssh` / `command -v ssh` in a new terminal. In a repository with `core.sshCommand` git uses that setting instead. |
| `ssh ls`: `Could not resolve hostname ls` | Through the shim `ssh` stays ssh: `ls` is a host name. Use `zzh ls`, or `ssh --zzh ls`. |
| `zzh started itself … times as ssh` | The "real" ssh found in PATH is zzh again (a copy instead of a link, or `ssh.binary` pointing to a shim). Check with `zzh doctor` and `zzh config get ssh.binary`. |
| `access denied in the zzh confirmation` | You pressed Deny, or nobody answered the dialog within 60 seconds. |
| `a confirmation is required (confirm.mode) but there is no terminal or graphical session to ask it` | A program without a terminal, where no dialog can be shown (a service, a server without display). Run it where you can answer, or `zzh config set confirm.mode never` for unattended use. |
| A background program asks every time | Choose *Allow for 15 minutes*, or set a longer `confirm.remember_for`; with `confirm.mode never` it is never asked. |
| Strange characters like `←[1m` instead of colors | An old console without color support: `zzh config set output.color never`, or set `NO_COLOR`. |
| Striped table rows hard to read | Light terminal theme: `zzh config set output.stripes false`. |

---

## Development

```
cmd/zzh/              entry point: dispatches to the feature modules;
                      integration tests with the real ssh client
internal/core/        building blocks shared by the commands
  askpass/            answers ssh's questions through a private endpoint
  cliflags/           --long options of zzh's commands
  config/             configuration file, bookmarks, get/set/unset
  daemon/             background process keeping passwords and open databases
  keyagent/           read-only ssh-agent serving KeePass keys
  localipc/           private temporary named pipes / Unix sockets
  prompt/             terminal questions (hidden passwords, confirmations)
  session/            where the master password comes from; open and modify
  shim/               zzh started as ssh, scp or sftp; its own real path
  caller/             which program asked for the connection (process tree)
  gui/                the confirmation dialog, drawn by the system's tools
  approval/           "Allow for a while" authorizations, per program and host
  sshcli/             ssh command line parsing, OpenSSH version detection
  sshkey/             key generation and import
  style/              meaningful colors on terminals (NO_COLOR, output.color)
  table/              boxed tables on terminals, tab-separated lines for grep
  vault/              the KeePass database: hosts, keys, safe writes, snapshots
    vaulttest/        throw-away databases for tests
internal/feature/     one self-contained module per command
  addhost/ aliases/ configcmd/ connect/ daemoncmd/ dbcmd/ doctor/
  edithost/ hosts/ initdb/ install/ keys/ removehost/ syncout/
```

Core modules never import feature modules, and feature modules never import
each other.

```sh
go vet ./cmd/... ./internal/...
go test ./cmd/... ./internal/...           # includes integration tests
go test -short ./cmd/... ./internal/...    # skips them
```

The integration tests build the zzh binary and use the real OpenSSH client
against a small SSH server written in Go inside the test, so they never
touch a real server or your accounts; they are skipped when no suitable
OpenSSH client is installed.

Dependencies: [gokeepasslib](https://github.com/tobischo/gokeepasslib)
(KDBX), [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto)
(SSH keys and agent), [golang.org/x/term](https://pkg.go.dev/golang.org/x/term)
(terminal), [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys)
(Windows registry, console modes, elevation check),
[go-winio](https://github.com/microsoft/go-winio) (Windows named pipes).

---

## Author

Developed by **Massimiliano Dal Cero** — DRIFT Linux
· [LinkedIn](https://www.linkedin.com/in/dalcero/)

---

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
