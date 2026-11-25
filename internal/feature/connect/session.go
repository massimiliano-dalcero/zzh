package connect

import (
	"errors"
	"strings"
	"sync"

	"zzh/internal/core/askpass"
	"zzh/internal/core/config"
	"zzh/internal/core/keyagent"
	"zzh/internal/core/localipc"
	"zzh/internal/core/sshcli"
	"zzh/internal/core/sshkey"
	"zzh/internal/core/vault"
)

// Request is everything needed to connect to a host.
type Request struct {
	SSH        config.SSHConfig
	Host       vault.Host
	Invocation *sshcli.Invocation

	// PrivateKey is the host's key from KeePass (OpenSSH PEM), nil when the
	// host has no key.
	PrivateKey []byte

	// Executable is the path of the zzh binary, used as SSH_ASKPASS.
	Executable string

	// AskHidden and AskVisible relay a question to the user (normally
	// prompt.Password and prompt.Line); parameters for the tests.
	AskHidden  func(message string) (string, error)
	AskVisible func(message string) (string, error)
}

// Connect runs ssh for one connection, serving the KeePass key through a
// private agent and answering ssh's questions through askpass (D-006,
// D-029). Both exist only while ssh runs: they are closed when it exits,
// and the key never leaves this process.
func Connect(request Request) error {
	var env []string
	useAgent := false

	if request.PrivateKey != nil {
		endpoint, err := startAgent(request.PrivateKey, request.Host.KeyName)
		if err != nil {
			return err
		}
		defer endpoint.Close()

		// SSH_AUTH_SOCK is set only in ssh's environment; BuildArgs points
		// IdentityAgent at it.
		env = append(env, "SSH_AUTH_SOCK="+endpoint.Path)
		useAgent = true
	}

	if request.SSH.UseAskpass {
		answerer := &answerer{
			password:   request.Host.Password,
			askHidden:  request.AskHidden,
			askVisible: request.AskVisible,
		}
		server, err := askpass.Start(answerer.answer)
		if err != nil {
			return err
		}
		defer server.Close()
		env = append(env, server.Environment(request.Executable)...)
	}

	args := BuildArgs(request.SSH, request.Host, request.Invocation, useAgent)
	return Run(request.SSH.Binary, args, env)
}

// startAgent serves one private key on a new private endpoint.
func startAgent(privatePEM []byte, comment string) (*localipc.Endpoint, error) {
	privateKey, err := sshkey.ParsePrivateKey(privatePEM)
	if err != nil {
		return nil, err
	}

	sshAgent, err := keyagent.New([]keyagent.Key{{PrivateKey: privateKey, Comment: comment}})
	if err != nil {
		return nil, err
	}

	endpoint, err := localipc.Listen("agent")
	if err != nil {
		return nil, err
	}
	go keyagent.Serve(endpoint.Listener, sshAgent)
	return endpoint, nil
}

// answerer decides how to answer each question ssh asks.
type answerer struct {
	mutex sync.Mutex

	// password is the one stored in KeePass, typed automatically once.
	password     string
	passwordUsed bool

	askHidden  func(message string) (string, error)
	askVisible func(message string) (string, error)
}

// answer implements askpass.Handler.
//
//   - A password question gets the KeePass password, but only the first
//     time: if ssh asks again the stored password was rejected, and sending
//     it again would only count as more failed attempts on the server (and
//     possibly get the user banned). From the second time the user is asked.
//   - Yes/no questions (unknown or changed host key) are shown to the user
//     with visible typing: they are decisions zzh must not take silently.
//   - Anything else (one-time codes, key passphrases) is asked to the user
//     without echo, since it may be secret.
func (a *answerer) answer(prompt string) (string, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	lower := strings.ToLower(prompt)

	var reply string
	var err error

	switch {
	case strings.Contains(lower, "password") && a.password != "" && !a.passwordUsed:
		a.passwordUsed = true
		return a.password, nil

	case strings.Contains(lower, "(yes/no"):
		reply, err = a.askVisible(prompt)

	default:
		reply, err = a.askHidden(prompt)
	}

	// Whatever went wrong while asking (Ctrl+C, closed terminal), for ssh
	// it is a cancelled question.
	if err != nil {
		return "", errors.Join(askpass.ErrRefused, err)
	}
	return reply, nil
}
