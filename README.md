# zzh

**SSH connections driven by a KeePass database.**

`zzh` keeps your SSH hosts, keys and passwords in a KeePass (`.kdbx`)
database — the same kind of file you open with KeePassXC — and drives the
OpenSSH client installed on your system with them. You type `zzh web01`;
zzh finds `web01` in KeePass, hands its key and password to `ssh`, and gets
out of the way.

```console
$ zzh ls
NAME      ALIASES        FOLDER  USER    ADDRESS          KEY     PASSWORD  TAGS
bastion                  prod    jump    10.0.0.1:2222    laptop            prod
web01     w1,front       prod    deploy  10.0.0.10        laptop  stored    prod,web
github                           git     github.com       github

$ zzh web01                     # interactive shell, key from KeePass
$ zzh w1 -L 8080:localhost:80   # same host through an alias, with a tunnel
$ zzh web01 uptime              # remote command
```

- No private key, password or `known_hosts` entry needs to live in plain
  files: everything stays in the encrypted database.
- Every classic `ssh` option keeps working (`-L`, `-R`, `-D`, `-J`, `-t`,
  `-v`, `-o` …), because the real `ssh` does the connection.
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
- [The master password and the daemon](#the-master-password-and-the-daemon)
- [Several databases](#several-databases)
- [Using zzh with git](#using-zzh-with-git)
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

Rename it to `zzh` (`zzh.exe` on Windows) and put it in your `PATH` (see
below). `SHA256SUMS.txt` in the release lets you verify the download:

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
`CGO_ENABLED=0` makes it fully static.

The version is defined in one place, `releaseVersion` in
`cmd/zzh/version.go`. A binary built from a git checkout also shows the
commit it comes from (`zzh v0.9.1+4a0cc78`, `.dirty` with uncommitted
changes); add `-buildvcs=false` to get exactly the release version, as the
published binaries do.

### Putting zzh in the PATH

On Windows, for the current user:

```powershell
$dir = "$env:LOCALAPPDATA\Programs\zzh"
New-Item -ItemType Directory -Force $dir | Out-Null
Copy-Item zzh.exe $dir
[Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$dir", "User")
# open a new terminal
```

> In PowerShell a program in the current folder must be called as
> `.\zzh.exe`; once it is in the `PATH`, plain `zzh` works everywhere.

On Linux and macOS:

```sh
mkdir -p ~/.local/bin
install -m 755 zzh ~/.local/bin/zzh     # make sure ~/.local/bin is in your PATH
```

Then check the environment:

```console
$ zzh doctor
[ok  ] config: /home/max/.config/zzh/config.json not found, using defaults
[FAIL] database: none bookmarked (use "zzh init <name> <path>" or "zzh db add <name> <path>")
[ok  ] ssh: /usr/bin/ssh
[ok  ] ssh version: OpenSSH 9.6
[ok  ]   SSH_ASKPASS_REQUIRE=force (needs 8.4): passwords are supplied by zzh instead of being typed
[ok  ]   KnownHostsCommand (needs 8.5): host keys are read from KeePass instead of known_hosts
```

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
zzh ls [--tag prod]...                         # list (filters combine)

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

Commands with no terminal anyone can see — scripts, cron, scheduled tasks,
git started by an editor in the background — need either an unlocked
database or `ZZH_PASSWORD`; otherwise they stop with a message asking for
`zzh unlock`. On Windows a console counts as visible when it has a visible
window or is a pseudo console (Windows Terminal, the VS Code terminal, an
ssh session); a hidden console never receives questions, so a background
program is never left waiting for an answer nobody can give.

### The daemon

```sh
zzh unlock          # ask the password now and keep it (e.g. before using git)
zzh status          # which databases are unlocked and when they lock
zzh lock            # forget every password now (zzh --db work lock: only one)
zzh daemon stop     # stop the daemon
```

```console
$ zzh status
DATABASE  PATH                    UNLOCKED  LOCKS     KEPT
personal  /home/max/vault/ssh.kdbx  10:42:13  in 14m3s  open
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
# Tools that run git in the background (editors, scheduled jobs) have no
# terminal to ask on: unlock first
zzh unlock
```

On Windows use forward slashes in the path, e.g.
`C:/Users/me/AppData/Local/Programs/zzh/zzh.exe`.

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
  "ssh": { "extra_options": ["ServerAliveInterval=30"] }
}
```

Other environment variables: `ZZH_PASSWORD` (master password, see above),
`ZZH_DAEMON_DIR` (where the daemon publishes its endpoint).

---

## Command reference

| Command | |
|---|---|
| `zzh <target> [--key k] [ssh options] [command]` | connect (name, alias, address; otherwise plain ssh); `--key` picks a KeePass key for this connection |
| `zzh ls [--tag t]...` | list hosts |
| `zzh add …` / `zzh edit …` / `zzh rm <host>` | create, change, remove hosts |
| `zzh alias add <host> <alias>...` / `zzh alias rm <alias>...` | host aliases |
| `zzh key gen\|import\|ls\|pub\|use\|rm …` | keys |
| `zzh sync out [--dir path] [--dry-run]` | export to `~/.ssh` |
| `zzh unlock` / `zzh lock` / `zzh status` / `zzh daemon stop` | master password and daemon |
| `zzh init <name> <path>` | create a database |
| `zzh db ls\|add\|rm\|default …` | bookmarked databases |
| `zzh config path\|show\|init\|get\|set\|unset …` | configuration |
| `zzh doctor` | check ssh, configuration and databases |
| `zzh version` / `zzh help` | |

`--db <name>`, right after `zzh`, selects a database for the commands that
use one. `zzh help` shows every option.

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
| `zzh` "not recognized" in PowerShell | Use `.\zzh.exe` in the current folder, or put zzh in your `PATH`. |
| The daemon keeps running | It stops by itself when nothing is unlocked; `zzh daemon stop` stops it now. |

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
  sshcli/             ssh command line parsing, OpenSSH version detection
  sshkey/             key generation and import
  vault/              the KeePass database: hosts, keys, safe writes, snapshots
    vaulttest/        throw-away databases for tests
internal/feature/     one self-contained module per command
  addhost/ aliases/ configcmd/ connect/ daemoncmd/ dbcmd/ doctor/
  edithost/ hosts/ initdb/ keys/ removehost/ syncout/
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
(terminal), [go-winio](https://github.com/microsoft/go-winio) (Windows
named pipes).

---

## Author

Developed by **Massimiliano Dal Cero** — DRIFT Linux
· [LinkedIn](https://www.linkedin.com/in/dalcero/)

---

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
