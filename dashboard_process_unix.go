//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

func detachDashboardProcess(command *exec.Cmd) {
	// Process.Release only releases Go's process handle. A new session also
	// separates the server from the launcher's terminal and process group.
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
