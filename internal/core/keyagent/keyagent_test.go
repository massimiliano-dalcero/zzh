package keyagent

import (
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"zzh/internal/core/localipc"
	"zzh/internal/core/sshkey"
)

// startAgent serves one freshly generated key on a real local endpoint and
// returns a client connected to it, as ssh would be.
func startAgent(t *testing.T, keyType string, bits int) (agent.ExtendedAgent, sshkey.Pair) {
	t.Helper()

	pair, err := sshkey.Generate(keyType, bits, "test-key")
	if err != nil {
		t.Fatalf("cannot generate key: %v", err)
	}
	privateKey, err := sshkey.ParsePrivateKey(pair.PrivatePEM)
	if err != nil {
		t.Fatalf("cannot parse key: %v", err)
	}

	sshAgent, err := New([]Key{{PrivateKey: privateKey, Comment: "test-key"}})
	if err != nil {
		t.Fatalf("cannot create agent: %v", err)
	}

	endpoint, err := localipc.Listen("agent-test")
	if err != nil {
		t.Fatalf("cannot listen: %v", err)
	}
	t.Cleanup(func() { endpoint.Close() })
	go Serve(endpoint.Listener, sshAgent)

	conn, err := localipc.Dial(endpoint.Path, 2*time.Second)
	if err != nil {
		t.Fatalf("cannot connect to the agent: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return agent.NewClient(conn), pair
}

func TestAgentListsAndSigns(t *testing.T) {
	for _, tc := range []struct {
		keyType string
		bits    int
	}{{sshkey.TypeEd25519, 0}, {sshkey.TypeRSA, 2048}} {
		t.Run(tc.keyType, func(t *testing.T) {
			client, pair := startAgent(t, tc.keyType, tc.bits)

			keys, err := client.List()
			if err != nil || len(keys) != 1 {
				t.Fatalf("expected one key, got %v (%v)", keys, err)
			}
			if ssh.FingerprintSHA256(keys[0]) != pair.Fingerprint || keys[0].Comment != "test-key" {
				t.Fatalf("unexpected key: %s %q", ssh.FingerprintSHA256(keys[0]), keys[0].Comment)
			}

			// The signature must verify with the public key, as the server
			// would do during authentication.
			data := []byte("session identifier")
			flags := agent.SignatureFlags(0)
			if tc.keyType == sshkey.TypeRSA {
				flags = agent.SignatureFlagRsaSha256
			}
			signature, err := client.SignWithFlags(keys[0], data, flags)
			if err != nil {
				t.Fatalf("sign failed: %v", err)
			}
			if err := keys[0].Verify(data, signature); err != nil {
				t.Fatalf("signature does not verify: %v", err)
			}
			if tc.keyType == sshkey.TypeRSA && signature.Format != ssh.KeyAlgoRSASHA256 {
				t.Fatalf("expected an rsa-sha2-256 signature, got %s", signature.Format)
			}
		})
	}
}

func TestAgentIsReadOnly(t *testing.T) {
	client, _ := startAgent(t, sshkey.TypeEd25519, 0)

	other, err := sshkey.Generate(sshkey.TypeEd25519, 0, "intruder")
	if err != nil {
		t.Fatalf("cannot generate key: %v", err)
	}
	otherKey, err := sshkey.ParsePrivateKey(other.PrivatePEM)
	if err != nil {
		t.Fatalf("cannot parse key: %v", err)
	}

	if err := client.Add(agent.AddedKey{PrivateKey: otherKey}); err == nil {
		t.Errorf("adding keys must be refused")
	}
	if err := client.RemoveAll(); err == nil {
		t.Errorf("removing keys must be refused")
	}
	if err := client.Lock([]byte("x")); err == nil {
		t.Errorf("locking must be refused")
	}

	keys, err := client.List()
	if err != nil || len(keys) != 1 {
		t.Fatalf("the agent must still hold exactly its key: %v (%v)", keys, err)
	}
}
