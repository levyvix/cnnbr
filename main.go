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
	"github.com/levyvix/cnnbr/internal/store"
	"github.com/levyvix/cnnbr/internal/ui"
)

func main() {
	pages := flag.Int("pages", 2, "páginas do feed a buscar (60 matérias por página)")
	ttl := flag.Duration("ttl", 15*time.Minute, "validade do cache antes de buscar de novo")
	justify := flag.Bool("justify", false, "justificar o texto nas duas margens (alterna com t)")
	flag.Parse()

	st := store.Default()
	st.Prune(60 * 24 * time.Hour)

	model := ui.New(ui.Config{
		Pages:   *pages,
		TTL:     *ttl,
		Justify: *justify,
		Client:  &http.Client{Timeout: 30 * time.Second},
		Cache:   feed.DefaultCache(*ttl),
		Store:   st,
	})

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}

	if err := st.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "aviso: não consegui salvar o estado:", err)
	}
}
