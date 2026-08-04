// cnnbr é um leitor de terminal para o cnnbrasil.com.br.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/levyvix/cnnbr/internal/feed"
	"github.com/levyvix/cnnbr/internal/prefs"
	"github.com/levyvix/cnnbr/internal/speech"
	"github.com/levyvix/cnnbr/internal/store"
	"github.com/levyvix/cnnbr/internal/ui"
)

func main() {
	// Os padrões do flag são os padrões embutidos das preferências, para que
	// `-h` mostre o que o programa realmente faz quando nada é passado.
	defaults := prefs.Defaults()
	pages := flag.Int("pages", defaults.Pages, "páginas do feed a buscar (60 matérias por página)")
	ttl := flag.Duration("ttl", defaults.TTL, "validade do cache antes de buscar de novo")
	justify := flag.Bool("justify", defaults.Justify, "justificar o texto nas duas margens (alterna com t)")
	flag.Parse()

	prefsPath := prefs.DefaultPath()
	fromFile, loadErr := prefs.Load(prefsPath)

	// Só as flags realmente digitadas sobrepõem o arquivo: o padrão de
	// `-justify` é true, então sem isso o arquivo nunca conseguiria desligar a
	// justificação.
	var fromFlags prefs.Partial
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "pages":
			fromFlags.Pages = pages
		case "ttl":
			fromFlags.TTL = ttl
		case "justify":
			fromFlags.Justify = justify
		}
	})

	p := prefs.Resolve(fromFile, fromFlags)

	notice := ""
	if loadErr != nil {
		notice = "preferências inválidas — usando padrões"
	}

	st := store.Default()
	if retention := p.Retention(); retention > 0 {
		st.Prune(retention)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	// As vozes têm um cliente próprio: o par do faber são 63 MB, e o Timeout do
	// http.Client cobre a leitura do corpo inteiro, então os 30 s dos feeds
	// cortariam o download no meio.
	player := speech.New(speech.Base(), &http.Client{Timeout: 15 * time.Minute})

	model := ui.New(ui.Config{
		Client: client,
		Cache:  feed.DefaultCache(p.TTL),
		Store:  st,
		Notice: notice,
		Speech: player,
		SavePrefs: func(chosen prefs.Partial) error {
			return prefs.Save(prefsPath, prefs.Resolve(fromFile, chosen))
		},
	}, p)

	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := prog.Run()

	// Antes de qualquer outra coisa na saída: sem isto, sair do programa deixaria
	// o espeak falando sozinho no terminal.
	player.Stop()

	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}

	if err := st.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "aviso: não consegui salvar o estado:", err)
	}

	// O que o leitor escolheu entra por cima do arquivo, não por cima do que as
	// flags resolveram: uma flag vale para esta execução, não para sempre.
	if m, ok := final.(ui.Model); ok {
		if chosen := m.Chosen(); !chosen.Empty() {
			if err := prefs.Save(prefsPath, prefs.Resolve(fromFile, chosen)); err != nil {
				fmt.Fprintln(os.Stderr, "aviso: não consegui salvar as preferências:", err)
			}
		}
	}
}
