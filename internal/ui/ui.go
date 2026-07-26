// Package ui implementa o leitor em bubbletea: uma lista de manchetes e um
// leitor de artigo, navegáveis com teclas estilo vim.
package ui

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/levyvix/cnnbr/internal/article"
	"github.com/levyvix/cnnbr/internal/feed"
	"github.com/levyvix/cnnbr/internal/store"
)

type mode int

const (
	modeList mode = iota
	modeReader
)

// linesPerItem é a altura de cada item na lista (título + metadados + respiro).
const linesPerItem = 3

// Config são as opções de execução do leitor.
type Config struct {
	Pages   int           // páginas do feed a buscar (60 matérias por página)
	TTL     time.Duration // validade do cache
	Justify bool          // texto justificado nas duas margens
	Client  *http.Client
	Cache   feed.Cache
	Store   *store.Store
}

// Model é o estado da aplicação.
type Model struct {
	cfg Config

	mode   mode
	width  int
	height int

	items  []feed.Item
	view   []int // índices de items visíveis, após filtro
	cursor int   // posição em view
	top    int   // primeiro item visível na tela

	onlySaved bool
	loading   bool
	fetchedAt time.Time
	fromCache bool

	statusText string
	statusErr  bool
	showHelp   bool

	reader     viewport.Model
	readingIdx int // índice em items da matéria aberta
	blocks     []article.Block
}

// New cria o modelo inicial.
func New(cfg Config) Model {
	if cfg.Pages < 1 {
		cfg.Pages = 2
	}
	return Model{cfg: cfg, loading: true, reader: viewport.New(80, 20)}
}

// feedMsg carrega o resultado de uma busca de feed.
type feedMsg feed.Result

// clearStatusMsg apaga o aviso da barra de status.
type clearStatusMsg struct{}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetch(false), tea.EnterAltScreen)
}

func (m Model) fetch(force bool) tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return feedMsg(feed.Get(ctx, cfg.Client, cfg.Cache, cfg.Pages, force))
	}
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearStatusMsg{} })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.reader.Width = msg.Width
		m.reader.Height = m.bodyHeight()
		if m.mode == modeReader {
			m.reader.SetContent(m.renderArticle())
		}
		m.clampCursor()
		return m, nil

	case feedMsg:
		m.loading = false
		if msg.Err != nil && len(msg.Items) == 0 {
			m.statusText = "falha ao buscar o feed: " + msg.Err.Error()
			m.statusErr = true
			return m, nil
		}
		m.items = msg.Items
		m.fetchedAt = msg.FetchedAt
		m.fromCache = msg.FromCache
		m.rebuildView()
		if msg.Err != nil {
			m.statusText = "sem rede — mostrando cache"
			m.statusErr = true
			return m, clearStatusAfter(4 * time.Second)
		}
		return m, nil

	case statusMsg:
		m.statusText = msg.text
		m.statusErr = msg.isErr
		return m, clearStatusAfter(3 * time.Second)

	case clearStatusMsg:
		m.statusText = ""
		m.statusErr = false
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)
	}

	return m, nil
}

// handleMouse trata a roda do mouse: quem já tem a mão no mouse não deveria
// precisar do teclado só para rolar.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelDown:
		if m.mode == modeReader {
			m.reader.ScrollDown(3)
		} else {
			m.moveCursor(1)
		}
	case tea.MouseButtonWheelUp:
		if m.mode == modeReader {
			m.reader.ScrollUp(3)
		} else {
			m.moveCursor(-1)
		}
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// A ajuda intercepta qualquer tecla.
	if m.showHelp {
		switch key {
		case "ctrl+c":
			return m, tea.Quit
		default:
			m.showHelp = false
			return m, nil
		}
	}

	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "?":
		m.showHelp = true
		return m, nil
	case "r":
		if m.loading {
			return m, nil
		}
		m.loading = true
		return m, tea.Batch(m.fetch(true), status("atualizando…"))
	case "t":
		m.cfg.Justify = !m.cfg.Justify
		if m.mode == modeReader {
			at := m.reader.YOffset
			m.reader.SetContent(m.renderArticle())
			m.reader.SetYOffset(at)
		}
		if m.cfg.Justify {
			return m, status("texto justificado")
		}
		return m, status("texto alinhado à esquerda")
	}

	if m.mode == modeReader {
		return m.handleReaderKey(key)
	}
	return m.handleListKey(key)
}

func (m Model) handleListKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc":
		return m, tea.Quit

	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "ctrl+d", "pgdown", " ":
		m.moveCursor(m.itemsPerPage())
	case "ctrl+u", "pgup":
		m.moveCursor(-m.itemsPerPage())
	case "g", "home":
		m.cursor, m.top = 0, 0
	case "G", "end":
		m.cursor = len(m.view) - 1
		m.clampCursor()

	case "enter", "l", "right":
		if it, ok := m.selected(); ok {
			m.readingIdx = m.view[m.cursor]
			m.blocks = article.Parse(it.HTML)
			m.cfg.Store.MarkRead(it.ID())
			m.mode = modeReader
			m.reader = viewport.New(m.width, m.bodyHeight())
			m.reader.SetContent(m.renderArticle())
			return m, nil
		}

	case "o":
		if it, ok := m.selected(); ok {
			return m, openInBrowser(it.Link)
		}
	case "y":
		if it, ok := m.selected(); ok {
			return m, copyToClipboard(it.Link)
		}
	case "f":
		if it, ok := m.selected(); ok {
			saved := m.cfg.Store.ToggleSaved(it.ID())
			if m.onlySaved {
				m.rebuildView()
			}
			if saved {
				return m, status("salva nos favoritos")
			}
			return m, status("removida dos favoritos")
		}
	case "m":
		if it, ok := m.selected(); ok {
			if m.cfg.Store.ToggleRead(it.ID()) {
				return m, status("marcada como lida")
			}
			return m, status("marcada como não lida")
		}
	case "s":
		m.onlySaved = !m.onlySaved
		m.cursor, m.top = 0, 0
		m.rebuildView()
		if m.onlySaved {
			return m, status("mostrando só favoritos")
		}
		return m, status("mostrando todas")
	}

	return m, nil
}

func (m Model) handleReaderKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc", "h", "left":
		m.mode = modeList
		return m, nil

	case "j", "down":
		m.reader.ScrollDown(1)
	case "k", "up":
		m.reader.ScrollUp(1)
	case "ctrl+d", " ", "pgdown":
		m.reader.HalfPageDown()
	case "ctrl+u", "pgup":
		m.reader.HalfPageUp()
	case "g", "home":
		m.reader.GotoTop()
	case "G", "end":
		m.reader.GotoBottom()

	case "J", "n":
		return m.jumpArticle(1)
	case "K", "p":
		return m.jumpArticle(-1)

	case "o":
		return m, openInBrowser(m.items[m.readingIdx].Link)
	case "y":
		return m, copyToClipboard(m.items[m.readingIdx].Link)
	case "f":
		it := m.items[m.readingIdx]
		saved := m.cfg.Store.ToggleSaved(it.ID())
		m.reader.SetContent(m.renderArticle())
		if saved {
			return m, status("salva nos favoritos")
		}
		return m, status("removida dos favoritos")
	}
	return m, nil
}

// jumpArticle avança/retrocede para a próxima matéria da lista sem voltar.
func (m Model) jumpArticle(delta int) (tea.Model, tea.Cmd) {
	next := m.cursor + delta
	if next < 0 || next >= len(m.view) {
		return m, status("fim da lista")
	}
	m.cursor = next
	m.clampCursor()
	m.readingIdx = m.view[m.cursor]
	it := m.items[m.readingIdx]
	m.blocks = article.Parse(it.HTML)
	m.cfg.Store.MarkRead(it.ID())
	m.reader.SetContent(m.renderArticle())
	m.reader.GotoTop()
	return m, nil
}

// rebuildView recalcula os índices visíveis conforme o filtro ativo.
func (m *Model) rebuildView() {
	m.view = m.view[:0]
	for i, it := range m.items {
		if m.onlySaved && !m.cfg.Store.IsSaved(it.ID()) {
			continue
		}
		m.view = append(m.view, i)
	}
	m.clampCursor()
}

func (m *Model) clampCursor() {
	if len(m.view) == 0 {
		m.cursor, m.top = 0, 0
		return
	}
	if m.cursor >= len(m.view) {
		m.cursor = len(m.view) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}

	per := m.itemsPerPage()
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+per {
		m.top = m.cursor - per + 1
	}
	if m.top < 0 {
		m.top = 0
	}
}

func (m *Model) moveCursor(delta int) {
	m.cursor += delta
	m.clampCursor()
}

func (m Model) selected() (feed.Item, bool) {
	if len(m.view) == 0 || m.cursor >= len(m.view) {
		return feed.Item{}, false
	}
	return m.items[m.view[m.cursor]], true
}

// bodyHeight é a altura disponível entre cabeçalho e barra de status.
func (m Model) bodyHeight() int {
	h := m.height - 4 // header (2 linhas) + status bar (2 linhas)
	if h < 3 {
		h = 3
	}
	return h
}

func (m Model) itemsPerPage() int {
	per := m.bodyHeight() / linesPerItem
	if per < 1 {
		per = 1
	}
	return per
}

// relativeTime formata a data como "há 2h", "há 15min", "ontem 21:40".
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "agora"
	case d < time.Hour:
		return fmt.Sprintf("há %dmin", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("há %dh", int(d.Hours()))
	case d < 48*time.Hour:
		return "ontem " + t.Format("15:04")
	default:
		return t.Format("02/01 15:04")
	}
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// hardWrapWidth garante que nada desenhado pela UI estoure a tela.
func (m Model) clip(s string) string {
	if m.width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if lipgloss.Width(ln) > m.width {
			lines[i] = truncateStyled(ln, m.width)
		}
	}
	return strings.Join(lines, "\n")
}
