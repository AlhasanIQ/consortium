//go:build windows

package benchloop

import (
	"log"
	"os"
	"os/exec"
	"time"

	"golang.org/x/sys/windows"
)

func configureAgentProcess(cmd *exec.Cmd) {
	// Windows does not support POSIX process groups. Keep cancellation best-effort
	// for v0.1 benchloop, which remains documented as experimental/operator-only.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 3 * time.Second
}

func killStaleAgentProcess(pid int) {
	log.Printf("Killing stale agent process (pid %d)", pid)
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Kill()
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(handle)
	return true
}
