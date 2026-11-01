//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestDashboardDetachedProcessHasOwnGroup(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestDashboardDetachedProcessHelper$")
	command.Env = append(os.Environ(), "ENGLISH_GUARD_DETACHED_TEST_CHILD=1")
	detachDashboardProcess(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	group, err := syscall.Getpgid(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if group != command.Process.Pid || group == syscall.Getpgrp() {
		t.Fatalf("dashboard child remains in launcher's process group: pid=%d, child group=%d, parent group=%d", command.Process.Pid, group, syscall.Getpgrp())
	}
}

func TestDashboardDetachedProcessHelper(t *testing.T) {
	if os.Getenv("ENGLISH_GUARD_DETACHED_TEST_CHILD") != "1" {
		return
	}
	time.Sleep(10 * time.Second)
}
