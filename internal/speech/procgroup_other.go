//go:build !unix

package speech

import "os/exec"

// Fora do Unix não há grupo de processos a montar. Detect já recusa o Windows,
// então isto existe para o pacote compilar, não para funcionar.
func group(*exec.Cmd) {}

func kill(c *exec.Cmd) {
	if c.Process != nil {
		_ = c.Process.Kill()
	}
}
