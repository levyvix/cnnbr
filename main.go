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
	"github.com/levyvix/cnnbr/internal/store"
	"github.com/levyvix/cnnbr/internal/ui"
)

func main() {
	// Os padrões do flag são os padrões embutidos das preferências, para que
	// `-h` mostre o que o programa realmente faz quando nada é passado.
	d := prefs.Defaults()
	pages := flag.Int("pages", d.Pages, "páginas do feed a buscar (60 matérias por página)")
	ttl := flag.Duration("ttl", d.TTL, "validade do cache antes de buscar de novo")
	justify := flag.Bool("justify", d.Justify, "justificar o texto nas duas margens (alterna com t)")
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

	model := ui.New(ui.Config{
		Client: &http.Client{Timeout: 30 * time.Second},
		Cache:  feed.DefaultCache(p.TTL),
		Store:  st,
		Notice: notice,
	}, p)

	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := prog.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}

	if err := st.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "aviso: não consegui salvar o estado:", err)
	}

	if m, ok := final.(ui.Model); ok {
		if chosen, dirty := m.Prefs(); dirty {
			if err := prefs.Save(prefsPath, chosen); err != nil {
				fmt.Fprintln(os.Stderr, "aviso: não consegui salvar as preferências:", err)
			}
		}
	}
}
