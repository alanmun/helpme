// hooks.go — the shell wrappers are embedded into the binary so the installer
// can emit them with `helpme-bin --print-hook <shell>`. This guarantees the
// hook always matches the binary version and lets the curl|sh installer stay
// self-contained (no repo checkout needed).
package main

import (
	"embed"
	"fmt"
	"os"
)

//go:embed hooks/helpme.zsh hooks/helpme.bash
var hookFS embed.FS

func printHook(shell string) {
	var name string
	switch shell {
	case "zsh":
		name = "hooks/helpme.zsh"
	case "bash":
		name = "hooks/helpme.bash"
	default:
		fmt.Fprintf(os.Stderr, "helpme: unknown shell %q (use zsh or bash)\n", shell)
		os.Exit(2)
	}
	b, err := hookFS.ReadFile(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "helpme:", err)
		os.Exit(1)
	}
	os.Stdout.Write(b)
}
