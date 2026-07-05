//go:build !windows

package benchloop

import (
	"log"
	"os/exec"
	"syscall"
	"time"
)

func configureAgentProcess(cmd *exec.Cmd) {
	// Process group isolation: agent runs in its own group so we can kill the
	// entire tree on context cancellation or crash cleanup.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 3 * time.Second
}

func killStaleAgentProcess(pid int) {
	// Guard against PID reuse: our agents run with Setpgid (PGID == PID).
	// If the PID was recycled by an unrelated process, its PGID will differ.
	pgid, err := syscall.Getpgid(pid)
	if err != nil || pgid != pid {
		log.Printf("Stale agent PID %d has PGID %d (expected %d) - PID was reused, skipping kill", pid, pgid, pid)
		return
	}

	log.Printf("Killing stale agent process group (pid %d)", pid)
	_ = syscall.Kill(-pid, syscall.SIGKILL) // Kill entire process group.
	_ = syscall.Kill(pid, syscall.SIGKILL)  // Fallback: kill process directly.
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}
