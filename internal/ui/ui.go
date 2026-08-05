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
	"github.com/levyvix/cnnbr/internal/speech"
	"github.com/levyvix/cnnbr/internal/store"
)

type mode int

const (
	modeList mode = iota
	modeReader
	modePanel // preferências: uma terceira tela, não um booleano paralelo
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

	// Speech fala a matéria aberta. É ponteiro no main porque o bubbletea copia
	// o Model a cada Update, e um player por valor perderia os handles dos
	// processos.
	Speech Player

	// SavePrefs grava as escolhas do leitor quando o painel fecha. Quem empilha
	// as camadas e sabe onde fica o arquivo é o main.
	SavePrefs func(prefs.Partial) error
}

// tab é uma seção do feed com seu próprio conteúdo e posição de leitura.
//
// Ocultar uma aba não joga nada fora: ela sai de m.visible e continua com os
// itens, o cursor e o cache que já tinha.
type tab struct {
	hidden    bool
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

	prefs  prefs.Prefs
	chosen prefs.Partial // só o que o leitor mudou nesta sessão

	mode   mode
	width  int
	height int

	tabs    []tab
	order   []int // todos os índices de tabs, na ordem escolhida pelo leitor
	visible []int // índices de tabs visíveis, na ordem da barra de abas
	active  int   // índice em tabs

	onlySaved bool

	statusText string
	statusErr  bool
	showHelp   bool

	panel  panelState
	speech speechState

	reader     viewport.Model
	readingIdx int // índice em tabs[active].items da matéria aberta
	blocks     []article.Block
}

// New cria o modelo inicial com uma aba por seção, na ordem e com a
// visibilidade que as preferências pedem.
func New(cfg Config, p prefs.Prefs) Model {
	if p.Pages < 1 {
		p.Pages = prefs.Defaults().Pages
	}

	tabs := make([]tab, len(feed.Sections))
	known := make([]string, len(feed.Sections))
	at := make(map[string]int, len(feed.Sections))
	for i, s := range feed.Sections {
		tabs[i] = tab{section: s}
		known[i] = s.Slug
		at[s.Slug] = i
	}

	// A ordem e a visibilidade vêm do arquivo; quais seções existem, do binário.
	p.Sections = prefs.ReconcileSections(p.Sections, known)
	order := make([]int, 0, len(p.Sections))
	for _, s := range p.Sections {
		idx := at[s.Slug]
		tabs[idx].hidden = !s.Visible
		order = append(order, idx)
	}

	m := Model{cfg: cfg, prefs: p, tabs: tabs, order: order, reader: viewport.New(80, 20)}
	m.rebuildVisible()
	// A primeira aba visível é a ativa: a Home pode estar oculta.
	if len(m.visible) > 0 {
		m.active = m.visible[0]
	}
	return m
}

// rebuildVisible recalcula a barra de abas a partir da ordem escolhida.
func (m *Model) rebuildVisible() {
	m.visible = m.visible[:0]
	for _, idx := range m.order {
		if !m.tabs[idx].hidden {
			m.visible = append(m.visible, idx)
		}
	}
}

// Chosen devolve só as preferências que o leitor mudou nesta sessão, para o
// main gravar na saída ao lado do flush do store. É uma camada, não o estado
// resolvido: gravar o resolvido eternizaria no arquivo o `-pages 8` que era
// para valer só nesta execução.
func (m Model) Chosen() prefs.Partial { return m.chosen }

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
	if cmd := waitSpeech(m.cfg.Speech); cmd != nil {
		cmds = append(cmds, cmd)
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
		return feedMsg(feed.GetSources(ctx, cfg.Client, cfg.Cache, feed.SourcesFor(s), s, pages, force))
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
		m.clampPanel()
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

	case speechEvent:
		return m.handleSpeechEvent(speech.Event(msg))

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
		switch m.mode {
		case modeReader:
			m.reader.ScrollDown(3)
		case modePanel:
			m.movePanelCursor(1)
		default:
			m.moveCursor(1)
		}
	case tea.MouseButtonWheelUp:
		switch m.mode {
		case modeReader:
			m.reader.ScrollUp(3)
		case modePanel:
			m.movePanelCursor(-1)
		default:
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
	}

	// O painel tem um mapa de teclas só: nem abas, nem dígitos, nem r.
	if m.mode == modePanel {
		return m.handlePanelKey(key)
	}

	switch key {
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
		justify := !m.prefs.Justify
		m.prefs.Justify = justify
		m.chosen.Justify = &justify
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

	// Trocar de aba sai do leitor, e a fala está atada à matéria aberta.
	m.stopSpeech()
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

	case "c":
		m.openPanel()
		return m, nil

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
		m.stopSpeech()
		m.mode = modeList
		return m, nil

	case "a":
		// A chamada vem antes do return: ela mexe em m.speech, e num
		// `return m, m.toggleSpeech()` a ordem em que o Go avalia os dois
		// operandos não é garantida — o m devolvido poderia ser o de antes.
		cmd := m.toggleSpeech()
		return m, cmd

	case "A":
		cmd := m.installNeural()
		return m, cmd

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
	m.stopSpeech()
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
	hidden := m.hiddenSlugs(idx)
	t.view = t.view[:0]
	for i, it := range t.items {
		if m.onlySaved && !m.cfg.Store.IsSaved(it.ID()) {
			continue
		}
		if hidden[feed.SlugOf(it.Link)] {
			continue
		}
		t.view = append(t.view, i)
	}
	if idx == m.active {
		m.clampCursor()
	}
}

// hiddenSlugs são os slugs das seções ocultas, e só valem para o feed geral: a
// Home reúne matérias de todas as seções, então ocultar uma seção também tira as
// matérias dela dali — senão o que o leitor mandou sumir continua na primeira
// tela. Dentro de uma seção não há o que filtrar: o feed já vem só dela.
func (m Model) hiddenSlugs(idx int) map[string]bool {
	if m.tabs[idx].section.Cat != 0 {
		return nil
	}
	var slugs map[string]bool
	for i := range m.tabs {
		if m.tabs[i].hidden {
			if slugs == nil {
				slugs = make(map[string]bool, len(m.tabs))
			}
			slugs[m.tabs[i].section.Slug] = true
		}
	}
	return slugs
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

	t.top = clampWindow(t.cursor, t.top, m.itemsPerPage())
}

// clampWindow desliza a janela de rolagem o mínimo para conter o cursor.
func clampWindow(cursor, top, per int) int {
	if cursor < top {
		top = cursor
	}
	if cursor >= top+per {
		top = cursor - per + 1
	}
	if top < 0 {
		return 0
	}
	return top
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
