//go:build unix

package ocv2

import (
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func signalGroup(cmd *exec.Cmd, kill bool) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	return syscall.Kill(-cmd.Process.Pid, sig)
}
