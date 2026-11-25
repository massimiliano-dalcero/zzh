// Package keyagent serves private keys to ssh through the ssh-agent
// protocol, so that ssh can authenticate with a key stored in KeePass
// without the key ever being written to disk or handed to ssh: ssh only
// receives signatures (D-006).
package keyagent

import (
	"crypto"
	"errors"
	"fmt"
	"net"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// errReadOnly is returned to clients trying to change the agent: it only
// exists to sign with the keys it was started with.
var errReadOnly = errors.New("zzh agent: read-only")

// Key is a private key to serve.
type Key struct {
	PrivateKey crypto.PrivateKey
	Comment    string
}

// readOnlyAgent exposes the signing operations of a keyring and refuses all
// the operations that would modify it. ssh only lists keys and asks for
// signatures; refusing the rest means that another local program reaching
// the endpoint cannot add, remove or lock keys during the session.
type readOnlyAgent struct {
	keyring agent.Agent
}

func (a readOnlyAgent) List() ([]*agent.Key, error) {
	return a.keyring.List()
}

func (a readOnlyAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	return a.keyring.Sign(key, data)
}

// SignWithFlags is needed for RSA keys: modern servers require the
// rsa-sha2-256/512 signature algorithms, requested through flags.
func (a readOnlyAgent) SignWithFlags(key ssh.PublicKey, data []byte, flags agent.SignatureFlags) (*ssh.Signature, error) {
	extended, ok := a.keyring.(agent.ExtendedAgent)
	if !ok {
		return nil, errors.New("zzh agent: signature flags not supported")
	}
	return extended.SignWithFlags(key, data, flags)
}

func (a readOnlyAgent) Signers() ([]ssh.Signer, error) {
	return nil, errReadOnly
}

func (a readOnlyAgent) Add(agent.AddedKey) error   { return errReadOnly }
func (a readOnlyAgent) Remove(ssh.PublicKey) error { return errReadOnly }
func (a readOnlyAgent) RemoveAll() error           { return errReadOnly }
func (a readOnlyAgent) Lock([]byte) error          { return errReadOnly }
func (a readOnlyAgent) Unlock([]byte) error        { return errReadOnly }
func (a readOnlyAgent) Extension(string, []byte) ([]byte, error) {
	return nil, agent.ErrExtensionUnsupported
}

// New builds a read-only agent holding the given keys.
func New(keys []Key) (agent.ExtendedAgent, error) {
	keyring := agent.NewKeyring()
	for _, key := range keys {
		if err := keyring.Add(agent.AddedKey{PrivateKey: key.PrivateKey, Comment: key.Comment}); err != nil {
			return nil, fmt.Errorf("cannot load key %q into the agent: %w", key.Comment, err)
		}
	}
	return readOnlyAgent{keyring: keyring}, nil
}

// Serve answers agent requests on every connection accepted by listener,
// until the listener is closed. Each ssh process opens its own connection;
// they are served concurrently (the keyring is safe for concurrent use).
func Serve(listener net.Listener, sshAgent agent.Agent) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			// The listener was closed: the session is over.
			return
		}
		go func() {
			defer conn.Close()
			// ServeAgent returns when ssh closes the connection; its error
			// (normally io.EOF) carries no information worth reporting.
			_ = agent.ServeAgent(sshAgent, conn)
		}()
	}
}
