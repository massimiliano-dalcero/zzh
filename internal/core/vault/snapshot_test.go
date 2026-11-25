package vault

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"
)

func TestSnapshotRoundTrip(t *testing.T) {
	path := standardDatabase(t)
	fileBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read database: %v", err)
	}

	v, err := Open(path, testPassword, "SSH")
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	data, key, err := v.ExportSnapshot()
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	start := time.Now()
	snapshot, err := OpenSnapshot(data, key, path, "SSH")
	if err != nil {
		t.Fatalf("cannot open snapshot: %v", err)
	}
	// The whole point: no expensive key derivation.
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("opening a snapshot took %v, it must be almost instant", elapsed)
	}

	// Same content, including protected values.
	host, err := snapshot.FindHost("web01")
	if err != nil || host.Password != "s3cret" || host.Address != "10.0.0.1" {
		t.Fatalf("snapshot content differs: %+v (%v)", host, err)
	}
	hosts, _ := snapshot.Hosts()
	if len(hosts) != 6 {
		t.Fatalf("expected 6 hosts, got %d", len(hosts))
	}

	// The snapshot can never be saved over the real file.
	if err := snapshot.Save(false); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("saving a snapshot must fail with ErrReadOnly, got %v", err)
	}
	fileAfter, _ := os.ReadFile(path)
	if !bytes.Equal(fileBefore, fileAfter) {
		t.Fatalf("exporting a snapshot must not touch the file")
	}

	// The snapshot key is the only way in.
	if _, err := OpenSnapshot(data, testPassword, path, "SSH"); err == nil {
		t.Fatalf("the master password must not open a snapshot")
	}
}

func TestSnapshotKeysAreUnique(t *testing.T) {
	path := standardDatabase(t)
	first, _ := Open(path, testPassword, "SSH")
	second, _ := Open(path, testPassword, "SSH")

	_, firstKey, err := first.ExportSnapshot()
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	_, secondKey, err := second.ExportSnapshot()
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if firstKey == secondKey || len(firstKey) != 64 {
		t.Fatalf("each snapshot needs its own random 256-bit key")
	}
}
