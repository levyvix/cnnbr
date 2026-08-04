package speech

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// O piper é um pacote Python com wheel `cp39-abi3` publicado no PyPI para
// manylinux, macOS e Windows, então instalá-lo não pede compilador nem casa com
// uma versão de Python específica. Só `onnxruntime` e `pathvalidate` vêm de
// carona — o `torch` está atrás do extra `train`, que não pedimos.
const piperPackage = "piper-tts"

// venvBin é onde um ambiente Python põe os executáveis. Detect recusa o Windows
// antes de chegar aqui, então não tratamos o `Scripts` de lá.
const venvBin = "bin"

// Install põe o piper num ambiente isolado que só o cnnbr usa.
//
// Um venv próprio, e não um `pip install` no Python do sistema, por três
// motivos: distro nenhuma deixa (PEP 668 marca o Python do sistema como
// externally-managed), não sujamos ambiente que não é nosso, e — o que mais
// importa aqui — o binário passa a ficar num caminho que nós escolhemos. A issue
// que desenhou a fala evitava o baixador do próprio piper justamente porque
// descobrir *qual* python é o do piper é frágil quando ele veio de pipx, de um
// venv ou do pacote da distro. Sendo o venv nosso, a pergunta não existe.
func Install(ctx context.Context, dir string, onStep func(string)) error {
	step := func(s string) {
		if onStep != nil {
			onStep(s)
		}
	}

	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}

	// O uv na frente: é a ferramenta que este projeto prefere para CLIs Python, e
	// resolve o ambiente em segundos. Sem ele, a biblioteca padrão dá conta.
	if uv, err := exec.LookPath("uv"); err == nil {
		step("criando o ambiente")
		if err := run(ctx, uv, "venv", dir); err != nil {
			return err
		}
		step("baixando o piper")
		return run(ctx, uv, "pip", "install", "--python", dir, piperPackage)
	}

	python, err := exec.LookPath("python3")
	if err != nil {
		return errors.New("instale uv ou python3 para o cnnbr baixar a voz neural sozinho")
	}

	step("criando o ambiente")
	if err := run(ctx, python, "-m", "venv", dir); err != nil {
		return err
	}
	step("baixando o piper")
	return run(ctx, filepath.Join(dir, venvBin, "pip"), "install", piperPackage)
}

// run executa e, quando falha, devolve a saída junto: um `pip install` que morre
// explica o motivo no stderr, e engolir isso deixaria o leitor com "falhou" e
// nada mais.
func run(ctx context.Context, bin string, args ...string) error {
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err == nil {
		return nil
	}
	if trimmed := lastLine(string(out)); trimmed != "" {
		return fmt.Errorf("%s: %s", filepath.Base(bin), trimmed)
	}
	return fmt.Errorf("%s: %w", filepath.Base(bin), err)
}

// lastLine é a última linha não vazia da saída — onde pip e uv põem o erro.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// managedPiper é o piper do ambiente que o cnnbr mantém, ou "" se não houver.
func managedPiper(dir string) string {
	bin := filepath.Join(dir, venvBin, piperBin)
	if info, err := os.Stat(bin); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		return bin
	}
	return ""
}

// CanInstall diz se dá para instalar o piper nesta máquina — precisa de uv ou de
// python3.
func CanInstall() bool {
	for _, bin := range []string{"uv", "python3"} {
		if _, err := exec.LookPath(bin); err == nil {
			return true
		}
	}
	return false
}
