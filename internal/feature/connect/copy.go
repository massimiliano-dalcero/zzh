package connect

import (
	"strings"

	"zzh/internal/core/shim"
)

// valueOptions lists, for scp and sftp, the option letters followed by a
// value (from their getopt strings in OpenSSH 9.x). They are needed to read
// a command line correctly: in "-oStrictHostKeyChecking=no" the "S" belongs
// to the value of -o, while in "-rS prog" it is the -S option. -s takes a
// value for sftp (subsystem) but is a plain switch for scp.
var valueOptions = map[string]string{
	shim.ProgramSCP:  "cDFiJloPSX",
	shim.ProgramSFTP: "BbcDFiJloPRSsX",
}

// RunCopy runs the real scp or sftp so that its connection goes through zzh:
// both accept "-S program" to use another program instead of ssh, and zzh
// passes itself, with shim.EnvAsSSH telling that zzh to act as ssh. The
// files are copied by the real programs; zzh only provides host data, keys
// and passwords, exactly as for an interactive connection.
//
// This is needed because scp and sftp do not look for ssh in PATH: the
// Windows ones run the ssh.exe of their own folder (verified, see D-055), so
// an ssh shim alone would never see their connections.
//
// When the user already chose a program with -S, that choice is respected
// and the command runs unchanged.
func RunCopy(program string, args []string, zzhExecutable string) error {
	var extraEnv []string
	if !hasProgramOption(program, args) {
		args = append([]string{"-S", zzhExecutable}, args...)
		extraEnv = []string{shim.EnvAsSSH + "=1"}
	}
	return Run(program, args, extraEnv)
}

// hasProgramOption reports whether -S appears among the options of an scp
// or sftp command line, following getopt rules: options end at "--" or at
// the first argument not starting with "-"; switches can be grouped
// ("-rS prog"); an option taking a value consumes the rest of its argument
// or, when nothing follows the letter, the next argument.
func hasProgramOption(program string, args []string) bool {
	takesValue := valueOptions[program]

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" || !strings.HasPrefix(arg, "-") || arg == "-" {
			return false
		}

		letters := arg[1:]
		for j := 0; j < len(letters); j++ {
			letter := letters[j]
			if letter == 'S' {
				return true
			}
			if strings.IndexByte(takesValue, letter) >= 0 {
				// The value is attached ("-P22") or is the next argument.
				if j == len(letters)-1 {
					i++
				}
				break
			}
		}
	}
	return false
}
