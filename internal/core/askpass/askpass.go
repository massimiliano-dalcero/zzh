// Package askpass lets zzh answer the questions ssh asks during a
// connection (passwords, host key confirmations) instead of ssh reading
// them from the keyboard (D-006).
//
// The mechanism has two sides:
//
//   - The zzh process that starts ssh runs a Server on a private local
//     endpoint, and starts ssh with SSH_ASKPASS pointing to the zzh
//     executable and SSH_ASKPASS_REQUIRE=force, so that ssh always uses it.
//   - For every question, ssh runs "zzh <question>". That second zzh
//     process finds the endpoint and a one-time token in its environment
//     (inherited from ssh), sends the question with Ask, prints the answer
//     on stdout for ssh, and exits.
//
// The answers never travel through the environment or the command line:
// only the endpoint path and the token do. The token proves that the
// question comes from the ssh started for this connection, not from
// another program that found the endpoint.
package askpass

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"zzh/internal/core/localipc"
)

// Environment variables passed to ssh, and through it to the askpass
// process.
const (
	EnvEndpoint = "ZZH_ASKPASS_ENDPOINT"
	EnvToken    = "ZZH_ASKPASS_TOKEN"
)

// ErrRefused is returned by a handler (and then by Ask) when the question is
// not answered, for example because the user cancelled. ssh treats a failing
// askpass program as a cancelled prompt.
var ErrRefused = errors.New("question not answered")

// Handler answers one question.
type Handler func(prompt string) (string, error)

type request struct {
	Token  string `json:"token"`
	Prompt string `json:"prompt"`
}

type response struct {
	Answer  string `json:"answer"`
	Refused bool   `json:"refused"`
}

// Server answers the questions of one ssh connection.
type Server struct {
	endpoint *localipc.Endpoint
	token    string
}

// Start opens the endpoint and answers questions with handler until Close.
// Questions are handled one at a time: ssh asks them sequentially, and the
// handler may need the terminal, which must not be shared.
func Start(handler Handler) (*Server, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("cannot generate the askpass token: %w", err)
	}

	endpoint, err := localipc.Listen("askpass")
	if err != nil {
		return nil, err
	}

	server := &Server{endpoint: endpoint, token: hex.EncodeToString(tokenBytes)}
	go server.serve(handler)
	return server, nil
}

// Environment returns the variables to add to ssh's environment.
func (s *Server) Environment(executable string) []string {
	return []string{
		"SSH_ASKPASS=" + executable,
		"SSH_ASKPASS_REQUIRE=force",
		EnvEndpoint + "=" + s.endpoint.Path,
		EnvToken + "=" + s.token,
	}
}

// Close stops answering.
func (s *Server) Close() error {
	return s.endpoint.Close()
}

func (s *Server) serve(handler Handler) {
	for {
		conn, err := s.endpoint.Listener.Accept()
		if err != nil {
			// The endpoint was closed: the connection is over.
			return
		}
		s.answer(conn, handler)
	}
}

func (s *Server) answer(conn net.Conn, handler Handler) {
	defer conn.Close()

	var question request
	if err := json.NewDecoder(conn).Decode(&question); err != nil {
		return
	}

	// Constant-time comparison: the time taken must not reveal how much of
	// a guessed token was right.
	if subtle.ConstantTimeCompare([]byte(question.Token), []byte(s.token)) != 1 {
		// No answer at all: the client sees a refusal.
		return
	}

	answer, err := handler(question.Prompt)
	reply := response{Answer: answer, Refused: err != nil}

	// If the askpass process is already gone there is nobody to tell.
	_ = json.NewEncoder(conn).Encode(reply)
}

// Requested reports whether this process was started by ssh as askpass
// program, i.e. whether the endpoint variable is present.
func Requested() bool {
	return os.Getenv(EnvEndpoint) != ""
}

// Ask sends prompt to the Server named in the environment and returns the
// answer. It is used by the "zzh <question>" process that ssh starts.
func Ask(prompt string) (string, error) {
	endpointPath := os.Getenv(EnvEndpoint)
	token := os.Getenv(EnvToken)
	if endpointPath == "" || token == "" {
		return "", errors.New("not started by zzh: askpass variables missing")
	}

	// The user may take a while to answer a question shown on the
	// terminal, so only connecting has a short timeout.
	conn, err := localipc.Dial(endpointPath, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("cannot reach zzh: %w", err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(request{Token: token, Prompt: prompt}); err != nil {
		return "", fmt.Errorf("cannot send the question: %w", err)
	}

	var reply response
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return "", ErrRefused
	}
	if reply.Refused {
		return "", ErrRefused
	}
	return reply.Answer, nil
}
