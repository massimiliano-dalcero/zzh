//go:build windows

package prompt

import "testing"

func TestAcceptConsole(t *testing.T) {
	cases := []struct {
		name      string
		visible   bool
		className string
		accepted  bool
	}{
		{"classic visible console", true, "ConsoleWindowClass", true},
		{"pseudo console (Windows Terminal, VS Code, sshd)", false, pseudoConsoleClass, true},
		{"hidden console of a background program", false, "ConsoleWindowClass", false},
		{"class name not readable", false, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := acceptConsole(tc.visible, tc.className); got != tc.accepted {
				t.Fatalf("acceptConsole(%v, %q) = %v, expected %v", tc.visible, tc.className, got, tc.accepted)
			}
		})
	}
}
