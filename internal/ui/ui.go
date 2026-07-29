// Package ui implementa o leitor em bubbletea: abas por seção, uma lista de
// manchetes e um leitor de artigo, navegáveis com teclas estilo vim.
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
	"github.com/levyvix/cnnbr/internal/prefs"
	"github.com/levyvix/cnnbr/internal/store"
)

type mode int

const (
	modeList mode = iota
	modeReader
)

// linesPerItem é a altura de cada item na lista (título + metadados + respiro).
const linesPerItem = 3

// Config são as dependências de execução que o main injeta. As escolhas do
// leitor não vivem aqui — são preferências, e ficam em prefs.Prefs.
type Config struct {
	Client *http.Client
	Cache  feed.Cache
	Store  *store.Store
	Notice string // aviso a mostrar no arranque, se houver
}

// tab é uma seção do feed com seu próprio conteúdo e posição de leitura.
type tab struct {
	section   feed.Section
	items     []feed.Item
	view      []int // índices de items visíveis, após filtro
	cursor    int   // posição em view
	top       int   // primeiro item visível na tela
	fetchedAt time.Time
	fromCache bool
	loading   bool
	loaded    bool
	err       error
}

// Model é o estado da aplicação.
type Model struct {
	cfg Config

	prefs      prefs.Prefs
	prefsDirty bool // alguma preferência mudou nesta sessão e precisa ser gravada

	mode   mode
	width  int
	height int

	tabs    []tab
	visible []int // índices de tabs visíveis, na ordem da barra de abas
	active  int   // índice em tabs

	onlySaved bool

	statusText string
	statusErr  bool
	showHelp   bool

	reader     viewport.Model
	readingIdx int // índice em tabs[active].items da matéria aberta
	blocks     []article.Block
}

// New cria o modelo inicial com uma aba por seção.
func New(cfg Config, p prefs.Prefs) Model {
	if p.Pages < 1 {
		p.Pages = prefs.Defaults().Pages
	}
	tabs := make([]tab, len(feed.Sections))
	visible := make([]int, len(feed.Sections))
	for i, s := range feed.Sections {
		tabs[i] = tab{section: s}
		visible[i] = i
	}
	return Model{cfg: cfg, prefs: p, tabs: tabs, visible: visible, reader: viewport.New(80, 20)}
}

// Prefs devolve as preferências da sessão e se elas mudaram. O main grava na
// saída, ao lado do flush do store.
func (m Model) Prefs() (prefs.Prefs, bool) { return m.prefs, m.prefsDirty }

// feedMsg carrega o resultado de uma busca de feed.
type feedMsg feed.Result

// clearStatusMsg apaga o aviso da barra de status.
type clearStatusMsg struct{}

// Init dispara a primeira busca e, se o main tiver um aviso, o mostra aqui: com
// a tela alternativa do bubbletea, escrever no stderr só apareceria depois que o
// programa sai.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadActive(false), tea.EnterAltScreen}
	if m.cfg.Notice != "" {
		cmds = append(cmds, statusErr("%s", m.cfg.Notice))
	}
	return tea.Batch(cmds...)
}

// loadActive busca a aba atual, a menos que ela já esteja carregada.
func (m *Model) loadActive(force bool) tea.Cmd {
	t := &m.tabs[m.active]
	if t.loading || (t.loaded && !force) {
		return nil
	}
	t.loading = true
	return m.fetch(t.section, force)
}

func (m Model) fetch(s feed.Section, force bool) tea.Cmd {
	cfg, pages := m.cfg, m.prefs.Pages
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return feedMsg(feed.Get(ctx, cfg.Client, cfg.Cache, s, pages, force))
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
		return m.handleFeed(feed.Result(msg))

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

func (m Model) handleFeed(res feed.Result) (tea.Model, tea.Cmd) {
	idx := -1
	for i := range m.tabs {
		if m.tabs[i].section.Slug == res.Section.Slug {
			idx = i
			break
		}
	}
	if idx < 0 {
		return m, nil
	}

	t := &m.tabs[idx]
	t.loading = false

	if res.Err != nil && len(res.Items) == 0 {
		t.err = res.Err
		t.loaded = false
		if idx == m.active {
			return m, statusErr("falha ao buscar %s: %v", t.section.Name, res.Err)
		}
		return m, nil
	}

	t.items = res.Items
	t.fetchedAt = res.FetchedAt
	t.fromCache = res.FromCache
	t.loaded = true
	t.err = nil
	m.rebuildView(idx)

	if res.Err != nil && idx == m.active {
		return m, statusErr("sem rede — mostrando cache")
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
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		m.showHelp = false
		return m, nil
	}

	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "?":
		m.showHelp = true
		return m, nil

	case "tab":
		return m.cycleTab(1)
	case "shift+tab":
		return m.cycleTab(-1)

	case "r":
		cmd := m.loadActive(true)
		if cmd == nil {
			return m, nil
		}
		return m, tea.Batch(cmd, status("atualizando %s…", m.cur().section.Name))

	case "t":
		m.prefs.Justify = !m.prefs.Justify
		m.prefsDirty = true
		if m.mode == modeReader {
			at := m.reader.YOffset
			m.reader.SetContent(m.renderArticle())
			m.reader.SetYOffset(at)
		}
		if m.prefs.Justify {
			return m, status("texto justificado")
		}
		return m, status("texto alinhado à esquerda")
	}

	// Dígitos saltam direto para a aba: 1 = primeira da barra, 0 = décima.
	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
		n := int(key[0] - '0')
		if n == 0 {
			n = 10
		}
		if n <= len(m.visible) {
			return m.switchTab(m.visible[n-1])
		}
		return m, nil
	}

	if m.mode == modeReader {
		return m.handleReaderKey(key)
	}
	return m.handleListKey(key)
}

// visiblePos é a posição da aba ativa na barra de abas.
func (m Model) visiblePos() int {
	for pos, idx := range m.visible {
		if idx == m.active {
			return pos
		}
	}
	return 0
}

// cycleTab anda `delta` posições na barra de abas, dando a volta nas pontas.
// A navegação passa pela lista de índices visíveis, nunca por m.tabs direto.
func (m Model) cycleTab(delta int) (tea.Model, tea.Cmd) {
	if len(m.visible) == 0 {
		return m, nil
	}
	pos := (m.visiblePos() + delta) % len(m.visible)
	if pos < 0 {
		pos += len(m.visible)
	}
	return m.switchTab(m.visible[pos])
}

// switchTab troca a aba ativa, buscando o feed dela na primeira visita.
func (m Model) switchTab(idx int) (tea.Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.tabs) {
		return m, nil
	}
	if idx == m.active && m.mode == modeList {
		return m, nil
	}

	m.active = idx
	m.mode = modeList
	m.rebuildView(idx)
	return m, m.loadActive(false)
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
		m.cur().cursor, m.cur().top = 0, 0
	case "G", "end":
		m.cur().cursor = len(m.cur().view) - 1
		m.clampCursor()

	case "l", "right":
		return m.cycleTab(1)
	case "h", "left":
		return m.cycleTab(-1)

	case "enter":
		if it, ok := m.selected(); ok {
			t := m.cur()
			m.readingIdx = t.view[t.cursor]
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
				m.rebuildAllViews()
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
		m.cur().cursor, m.cur().top = 0, 0
		m.rebuildAllViews()
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
		return m, openInBrowser(m.reading().Link)
	case "y":
		return m, copyToClipboard(m.reading().Link)
	case "f":
		saved := m.cfg.Store.ToggleSaved(m.reading().ID())
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
	t := m.cur()
	next := t.cursor + delta
	if next < 0 || next >= len(t.view) {
		return m, status("fim da lista")
	}
	t.cursor = next
	m.clampCursor()
	m.readingIdx = t.view[t.cursor]
	it := m.reading()
	m.blocks = article.Parse(it.HTML)
	m.cfg.Store.MarkRead(it.ID())
	m.reader.SetContent(m.renderArticle())
	m.reader.GotoTop()
	return m, nil
}

// cur devolve a aba ativa para leitura e escrita. O bubbletea copia o Model a
// cada Update, mas a cópia compartilha o array de tabs — escrever por aqui
// altera o estado real, que é o que queremos para cursor e conteúdo.
func (m *Model) cur() *tab { return &m.tabs[m.active] }

func (m Model) reading() feed.Item { return m.tabs[m.active].items[m.readingIdx] }

// rebuildView recalcula os índices visíveis de uma aba conforme o filtro ativo.
func (m *Model) rebuildView(idx int) {
	t := &m.tabs[idx]
	t.view = t.view[:0]
	for i, it := range t.items {
		if m.onlySaved && !m.cfg.Store.IsSaved(it.ID()) {
			continue
		}
		t.view = append(t.view, i)
	}
	if idx == m.active {
		m.clampCursor()
	}
}

func (m *Model) rebuildAllViews() {
	for i := range m.tabs {
		m.rebuildView(i)
	}
}

func (m *Model) clampCursor() {
	t := m.cur()
	if len(t.view) == 0 {
		t.cursor, t.top = 0, 0
		return
	}
	if t.cursor >= len(t.view) {
		t.cursor = len(t.view) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}

	per := m.itemsPerPage()
	if t.cursor < t.top {
		t.top = t.cursor
	}
	if t.cursor >= t.top+per {
		t.top = t.cursor - per + 1
	}
	if t.top < 0 {
		t.top = 0
	}
}

func (m *Model) moveCursor(delta int) {
	m.cur().cursor += delta
	m.clampCursor()
}

func (m Model) selected() (feed.Item, bool) {
	t := m.tabs[m.active]
	if len(t.view) == 0 || t.cursor >= len(t.view) {
		return feed.Item{}, false
	}
	return t.items[t.view[t.cursor]], true
}

// bodyHeight é a altura disponível entre cabeçalho e barra de status.
func (m Model) bodyHeight() int {
	h := m.height - 5 // marca + abas + divisor + divisor de status + dicas
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

// clip garante que nada desenhado pela UI estoure a largura da tela.
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
