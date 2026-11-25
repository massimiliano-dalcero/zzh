package localipc

import (
	"bufio"
	"strings"
	"testing"
	"time"
)

// An endpoint must carry data both ways and refuse connections once closed.
func TestListenDialClose(t *testing.T) {
	endpoint, err := Listen("test")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	go func() {
		conn, err := endpoint.Listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, _ := bufio.NewReader(conn).ReadString('\n')
		conn.Write([]byte(strings.ToUpper(line)))
	}()

	conn, err := Dial(endpoint.Path, 2*time.Second)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	if _, err := conn.Write([]byte("hello\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if err != nil || reply != "HELLO\n" {
		t.Fatalf("unexpected reply %q (%v)", reply, err)
	}

	if err := endpoint.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if conn, err := Dial(endpoint.Path, 200*time.Millisecond); err == nil {
		conn.Close()
		t.Fatalf("a closed endpoint must refuse connections")
	}
}

// Two endpoints for the same purpose must never collide.
func TestEndpointsAreUnique(t *testing.T) {
	first, err := Listen("agent")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer first.Close()

	second, err := Listen("agent")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer second.Close()

	if first.Path == second.Path {
		t.Fatalf("endpoint paths must be random, got %s twice", first.Path)
	}
}
