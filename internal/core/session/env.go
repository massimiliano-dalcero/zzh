package session

import (
	"os"
)

// EnvPassword is the environment variable that can hold the master
// password, for scripts or for users who accept the risk (D-028).
const EnvPassword = "ZZH_PASSWORD"

// environmentPassword is the value taken from EnvPassword at startup; empty
// when the variable was not set.
var environmentPassword string

// TakePasswordFromEnvironment reads the master password from ZZH_PASSWORD
// and removes the variable from this process's environment.
//
// It must be called once, as early as possible in main. Removing the
// variable is what keeps the password from reaching every program zzh
// starts (ssh, icacls, ...): child processes inherit the environment, and
// ssh could even forward variables to the server if configured with
// SendEnv. The value stays only in this package, in memory.
func TakePasswordFromEnvironment() error {
	value, present := os.LookupEnv(EnvPassword)
	if !present {
		return nil
	}

	environmentPassword = value
	return os.Unsetenv(EnvPassword)
}
