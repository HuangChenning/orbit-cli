package engine

import (
	"os/exec"
	"strconv"
)

func configureProcess(cmd *exec.Cmd) func() {
	cmd.Cancel = func() error {
		// taskkill terminates Git's SSH/credential children as well as Git itself.
		if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	return func() {}
}
