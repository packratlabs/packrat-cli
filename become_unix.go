//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// becomeProgram replaces packrat with the program, so the program has the terminal to itself.
func becomeProgram(env []string, name string, args ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("finding %s: %w", name, err)
	}
	if err := syscall.Exec(path, append([]string{name}, args...), append(os.Environ(), env...)); err != nil { // #nosec G204 -- claude, with arguments written here
		return fmt.Errorf("starting %s: %w", name, err)
	}
	return nil
}
