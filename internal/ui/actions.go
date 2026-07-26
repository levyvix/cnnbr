package ui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
)

// statusMsg mostra um aviso curto na barra de status.
type statusMsg struct {
	text  string
	isErr bool
}

func status(format string, args ...any) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: fmt.Sprintf(format, args...)} }
}

func statusErr(format string, args ...any) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: fmt.Sprintf(format, args...), isErr: true} }
}

// openInBrowser abre a URL no navegador padrão.
func openInBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		if err := cmd.Start(); err != nil {
			return statusMsg{text: "não consegui abrir o navegador: " + err.Error(), isErr: true}
		}
		// Não esperamos o processo: xdg-open sai rápido, mas o navegador pode não.
		go func() { _ = cmd.Wait() }()
		return statusMsg{text: "aberto no navegador"}
	}
}

// clipboardTools são tentados na ordem; o primeiro presente ganha.
var clipboardTools = [][]string{
	{"wl-copy"},
	{"xclip", "-selection", "clipboard"},
	{"xsel", "--clipboard", "--input"},
	{"pbcopy"},
}

// copyToClipboard usa a ferramenta nativa do sistema e cai para OSC52 (que
// funciona também por SSH) quando nenhuma está instalada.
func copyToClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		for _, tool := range clipboardTools {
			path, err := exec.LookPath(tool[0])
			if err != nil {
				continue
			}
			cmd := exec.Command(path, tool[1:]...)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				continue
			}
			if err := cmd.Start(); err != nil {
				continue
			}
			_, _ = stdin.Write([]byte(text))
			_ = stdin.Close()
			if err := cmd.Wait(); err != nil {
				continue
			}
			return statusMsg{text: "link copiado"}
		}

		// OSC52: sequência de escape que não move o cursor, então não conflita
		// com o desenho do bubbletea.
		enc := base64.StdEncoding.EncodeToString([]byte(text))
		if _, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\a", enc); err != nil {
			return statusMsg{text: "não consegui copiar: " + err.Error(), isErr: true}
		}
		return statusMsg{text: "link copiado (OSC52)"}
	}
}
