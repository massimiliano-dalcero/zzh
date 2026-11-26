package connect

import (
	"testing"

	"zzh/internal/core/shim"
)

func TestHasProgramOption(t *testing.T) {
	cases := []struct {
		name    string
		program string
		args    []string
		found   bool
	}{
		{"plain copy", shim.ProgramSCP, []string{"file", "web01:/tmp"}, false},
		{"separate -S", shim.ProgramSCP, []string{"-S", "prog", "file", "web01:"}, true},
		{"attached -S", shim.ProgramSCP, []string{"-Sprog", "file", "web01:"}, true},
		{"grouped -rS", shim.ProgramSCP, []string{"-rS", "prog", "dir", "web01:"}, true},
		{"S inside -o value", shim.ProgramSCP, []string{"-oStrictHostKeyChecking=no", "f", "web01:"}, false},
		{"S as value of -o", shim.ProgramSCP, []string{"-o", "S", "f", "web01:"}, false},
		{"-S after the operands", shim.ProgramSCP, []string{"f", "web01:", "-S"}, false},
		{"-S after --", shim.ProgramSCP, []string{"--", "-S", "web01:"}, false},
		{"scp -s is a switch", shim.ProgramSCP, []string{"-sS", "prog", "f", "web01:"}, true},
		{"sftp -s takes a value", shim.ProgramSFTP, []string{"-sS", "web01"}, false},
		{"sftp -S", shim.ProgramSFTP, []string{"-P", "22", "-S", "prog", "web01"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasProgramOption(tc.program, tc.args); got != tc.found {
				t.Fatalf("hasProgramOption(%s, %q) = %v, expected %v", tc.program, tc.args, got, tc.found)
			}
		})
	}
}
