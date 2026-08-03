//go:build unix

package speech

import (
	"os/exec"
	"syscall"
)

// group põe o processo num grupo próprio, para que matar a fala mate o
// motor e o player de uma vez.
func group(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// kill mata o grupo inteiro. Tem que ser o grupo: matar só o piper deixaria o
// aplay tocando até drenar o que já está no buffer, o que é justamente o
// contrário de parar.
func kill(c *exec.Cmd) {
	if c.Process == nil {
		return
	}
	if err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL); err != nil {
		_ = c.Process.Kill()
	}
}
