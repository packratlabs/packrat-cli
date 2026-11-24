package main

import (
	"fmt"
	"os"
	"os/exec"
)

// becomeProgram runs the program with packrat's terminal until it exits, because Windows cannot replace a process.
func becomeProgram(env []string, name string, args ...string) error {
	command := exec.Command(name, args...) // #nosec G204 -- claude, with arguments written here
	command.Env = append(os.Environ(), env...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("running %s: %w", name, err)
	}
	return nil
}
