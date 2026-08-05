package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/levyvix/cnnbr/internal/feed"
	"github.com/levyvix/cnnbr/internal/prefs"
	"github.com/levyvix/cnnbr/internal/speech"
)

// panelState é a posição do leitor dentro do painel de preferências.
//
// Toda linha do painel é um ciclo fechado de valores predefinidos, percorrido
// com h/l ou espaço — não há campo de texto em lugar nenhum, e isso é
// deliberado. Sem entrada de texto não existe widget de input, nem parse de
// duração falhando, nem estado "editando vs navegando", nem validação, nem
// mensagem de erro: toda linha vira a mesma coisa, escolha um de N, e o painel
// tem um mapa de teclas só.
type panelState struct {
	cursor int // índice em panelRows; nunca pousa num subtítulo
	top    int // primeira linha desenhada, quando o painel não cabe na tela
}

// value é um dos valores predefinidos de uma preferência. `n` é o valor em si,
// numa forma comparável: é por ele que se acha o predefinido mais próximo
// quando o valor atual veio de uma flag ou de um arquivo editado à mão.
type value struct {
	label string
	n     int64
}

// preference é uma linha do painel: uma preferência e o ciclo de valores dela.
type preference struct {
	label   string
	note    string // quando a escolha passa a valer, se não for na hora
	values  []value
	current func(prefs.Prefs) int64
	format  func(int64) string // rótulo de um valor fora dos predefinidos
	apply   func(*Model, value)
}

// display é o valor atual como aparece no painel. Um valor fora da lista de
// predefinidos é exibido como é, em vez de ser silenciosamente arredondado.
func (pref preference) display(p prefs.Prefs) string {
	n := pref.current(p)
	for _, v := range pref.values {
		if v.n == n {
			return v.label
		}
	}
	return pref.format(n)
}

// next é o próximo valor do ciclo. Partindo de um valor fora da lista, salta
// para o predefinido mais próximo — a primeira tecla ancora, a seguinte anda.
func (pref preference) next(p prefs.Prefs, delta int) value {
	n := pref.current(p)
	for i, v := range pref.values {
		if v.n == n {
			i = (i + delta) % len(pref.values)
			if i < 0 {
				i += len(pref.values)
			}
			return pref.values[i]
		}
	}

	nearest := pref.values[0]
	for _, v := range pref.values[1:] {
		if abs(v.n-n) < abs(nearest.n-n) {
			nearest = v
		}
	}
	return nearest
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// forever é "nunca podar" na reta da retenção. No arquivo isso é zero, mas aqui
// precisa ficar no fim da reta e não no começo: com zero, um histórico de 3
// dias editado à mão teria "nunca" como predefinido mais próximo, e uma tecla
// desligaria a poda sem querer.
const forever = math.MaxInt32

// prefRows são as linhas escalares do painel, na ordem em que aparecem:
// subtítulos de grupo, que o cursor pula, e as preferências.
var prefRows = []panelRow{
	{title: "Leitura"},

	{pref: &preference{
		label:  "Justificar",
		values: []value{{"não", 0}, {"sim", 1}},
		current: func(p prefs.Prefs) int64 {
			if p.Justify {
				return 1
			}
			return 0
		},
		// Inalcançável: as duas opções cobrem todos os valores de um bool.
		format: func(n int64) string { return strconv.FormatInt(n, 10) },
		apply: func(m *Model, v value) {
			justify := v.n == 1
			m.prefs.Justify = justify
			m.chosen.Justify = &justify
		},
	}},

	{pref: &preference{
		label:  "Retenção do histórico",
		note:   "vale na próxima execução",
		values: []value{{"7 dias", 7}, {"30 dias", 30}, {"60 dias", 60}, {"180 dias", 180}, {"nunca", forever}},
		current: func(p prefs.Prefs) int64 {
			if p.RetentionDays <= 0 {
				return forever
			}
			return int64(p.RetentionDays)
		},
		format: func(n int64) string { return fmt.Sprintf("%d dias", n) },
		apply: func(m *Model, v value) {
			days := 0
			if v.n != forever {
				days = int(v.n)
			}
			m.prefs.RetentionDays = days
			m.chosen.RetentionDays = &days
		},
	}},

	{title: "Feed"},

	{pref: &preference{
		label:   "Páginas",
		note:    "vale na próxima busca",
		values:  []value{{"1", 1}, {"2", 2}, {"3", 3}, {"4", 4}, {"5", 5}},
		current: func(p prefs.Prefs) int64 { return int64(p.Pages) },
		format:  func(n int64) string { return strconv.FormatInt(n, 10) },
		apply: func(m *Model, v value) {
			pages := int(v.n)
			m.prefs.Pages = pages
			m.chosen.Pages = &pages
		},
	}},

	{pref: &preference{
		label: "TTL do cache",
		values: []value{
			{"5min", int64(5 * time.Minute)},
			{"15min", int64(15 * time.Minute)},
			{"30min", int64(30 * time.Minute)},
			{"1h", int64(time.Hour)},
			{"6h", int64(6 * time.Hour)},
		},
		current: func(p prefs.Prefs) int64 { return int64(p.TTL) },
		format:  func(n int64) string { return shortDuration(time.Duration(n)) },
		apply: func(m *Model, v value) {
			ttl := time.Duration(v.n)
			m.prefs.TTL = ttl
			// O TTL vale na hora e sem rede: quem decide se o cache está fresco
			// é o Cache que o main injetou.
			m.cfg.Cache.TTL = ttl
			m.chosen.TTL = &ttl
		},
	}},

	{title: "Áudio"},

	{pref: &preference{
		label: "Voz",
		// Escolher uma voz grava a preferência e não baixa nada. Um download de
		// 63 MB violaria a invariante que o painel documenta sobre si mesmo —
		// "cada mudança vale no momento da tecla" — e percorrer as quatro vozes
		// com h/l dispararia quatro downloads.
		note:   "baixa na primeira vez que ouvir",
		values: voiceValues(),
		current: func(p prefs.Prefs) int64 {
			return int64(voiceIndex(speech.VoiceOr(p.Voice).Name))
		},
		// Inalcançável: VoiceOr resolve qualquer nome para um dos predefinidos.
		format: func(n int64) string { return strconv.FormatInt(n, 10) },
		apply: func(m *Model, v value) {
			name := speech.Voices[v.n].Name
			m.prefs.Voice = name
			m.chosen.Voice = &name
		},
	}},

	{pref: &preference{
		label: "Velocidade",
		// A velocidade também não pode valer na hora: o áudio já está no pipe.
		note: "vale na próxima fala",
		values: []value{
			{"1×", 100}, {"1,25×", 125}, {"1,5×", 150}, {"1,75×", 175},
			{"2×", 200}, {"2,25×", 225}, {"2,5×", 250},
		},
		current: func(p prefs.Prefs) int64 { return int64(p.SpeechRate) },
		format:  formatRate,
		apply: func(m *Model, v value) {
			rate := int(v.n)
			m.prefs.SpeechRate = rate
			m.chosen.SpeechRate = &rate
		},
	}},
}

// voiceValues são as vozes do piper como ciclo do painel. O valor é o índice na
// tabela, e não o nome, porque o painel compara valores em int64 — é assim que
// ele acha o predefinido mais próximo.
func voiceValues() []value {
	values := make([]value, len(speech.Voices))
	for i, v := range speech.Voices {
		values[i] = value{v.Name, int64(i)}
	}
	return values
}

func voiceIndex(name string) int {
	for i, v := range speech.Voices {
		if v.Name == name {
			return i
		}
	}
	return 0
}

// formatRate escreve uma velocidade fora dos predefinidos com a vírgula decimal
// que os rótulos usam: 1,4× e não 1.4×.
func formatRate(n int64) string {
	s := strconv.FormatFloat(float64(n)/100, 'f', -1, 64)
	return strings.Replace(s, ".", ",", 1) + "×"
}

// panelRow é uma linha do painel: um subtítulo de grupo, que o cursor pula, uma
// preferência escalar, ou uma seção.
type panelRow struct {
	title        string
	pref         *preference
	sourceHealth *int // índice em m.sourceHealth, quando a linha é uma fonte RSS
	section      *int // índice da seção em m.tabs, quando a linha é uma seção
}

// selectable diz se o cursor pousa nesta linha. Subtítulos, não.
func (r panelRow) selectable() bool {
	return r.pref != nil || r.sourceHealth != nil || r.section != nil
}

// panelRows é a lista plana do painel. As linhas de seção dependem do estado do
// modelo — a ordem delas é a da barra de abas, que muda com J/K.
func (m Model) panelRows() []panelRow {
	rows := make([]panelRow, 0, len(prefRows)+2+len(m.sourceHealth)+len(m.order))
	rows = append(rows, prefRows...)
	rows = append(rows, panelRow{title: "Fontes RSS"})
	for i := range m.sourceHealth {
		idx := i
		rows = append(rows, panelRow{sourceHealth: &idx})
	}
	rows = append(rows, panelRow{title: "Seções"})
	for _, idx := range m.order {
		rows = append(rows, panelRow{section: &idx})
	}
	return rows
}

// firstPrefRow é onde o cursor pousa ao abrir o painel.
func firstPrefRow() int {
	for i, r := range prefRows {
		if r.pref != nil {
			return i
		}
	}
	return 0
}

// shortDuration escreve a duração sem as casas zeradas do fim: "7m", não "7m0s".
func shortDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// openPanel entra no painel a partir da lista.
func (m *Model) openPanel() {
	m.mode = modePanel
	m.panel.cursor = firstPrefRow()
	m.clampPanel()
}

func (m Model) handlePanelKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	// Não há cancelar: cada escolha já valeu no momento da tecla, e fechar
	// grava o arquivo uma vez.
	case "esc", "q", "c":
		m.mode = modeList
		return m, m.savePrefs()

	case "j", "down":
		m.movePanelCursor(1)
	case "k", "up":
		m.movePanelCursor(-1)

	case "l", "right", " ":
		return m.cycleRow(1)
	case "h", "left":
		return m.cycleRow(-1)

	case "J":
		m.moveSection(1)
	case "K":
		m.moveSection(-1)
	}
	return m, nil
}

// cycleRow anda um valor na linha sob o cursor. Numa seção o ciclo tem dois
// valores, visível e oculta, então a direção não importa.
func (m Model) cycleRow(delta int) (tea.Model, tea.Cmd) {
	row := m.panelRows()[m.panel.cursor]
	if row.section != nil {
		return m.toggleSection(*row.section)
	}
	if row.pref != nil {
		row.pref.apply(&m, row.pref.next(m.prefs, delta))
	}
	return m, nil
}

// movePanelCursor anda até a próxima linha selecionável, parando nas pontas.
func (m *Model) movePanelCursor(delta int) {
	rows := m.panelRows()
	for i := m.panel.cursor + delta; i >= 0 && i < len(rows); i += delta {
		if rows[i].selectable() {
			m.panel.cursor = i
			break
		}
	}
	m.clampPanel()
}

func (m *Model) clampPanel() {
	m.panel.top = clampWindow(m.panel.cursor, m.panel.top, m.bodyHeight())
}

// savePrefs grava o que o leitor escolheu. Quem sabe empilhar as camadas e onde
// fica o arquivo é o main; a UI só sabe o que foi escolhido.
func (m Model) savePrefs() tea.Cmd {
	if m.cfg.SavePrefs == nil || m.chosen.Empty() {
		return nil
	}
	save, chosen := m.cfg.SavePrefs, m.chosen
	return func() tea.Msg {
		if err := save(chosen); err != nil {
			return statusMsg{text: "não consegui salvar as preferências: " + err.Error(), isErr: true}
		}
		return nil
	}
}

func (m Model) viewPanel() string {
	height := m.bodyHeight()
	rows := m.panelRows()

	width := 0
	for _, r := range rows {
		if label := m.rowLabel(r); lipgloss.Width(label) > width {
			width = lipgloss.Width(label)
		}
	}

	end := m.panel.top + height
	if end > len(rows) {
		end = len(rows)
	}

	lines := make([]string, 0, height)
	for i := m.panel.top; i < end; i++ {
		lines = append(lines, m.viewPanelRow(rows[i], i, width))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:height], "\n")
}

// rowLabel é o rótulo da esquerda; vazio nos subtítulos, que não se alinham com
// os valores.
func (m Model) rowLabel(r panelRow) string {
	switch {
	case r.pref != nil:
		return r.pref.label
	case r.sourceHealth != nil:
		return m.sourceHealth[*r.sourceHealth].SourceName
	case r.section != nil:
		return m.tabs[*r.section].section.Name
	}
	return ""
}

// rowValue é o valor atual da linha, com a nota de quando ele passa a valer.
func (m Model) rowValue(r panelRow) (value, note string) {
	switch {
	case r.pref != nil:
		return r.pref.display(m.prefs), r.pref.note
	case r.sourceHealth != nil:
		return sourceHealthValue(m.sourceHealth[*r.sourceHealth])
	case r.section != nil:
		if m.tabs[*r.section].hidden {
			return "oculta", ""
		}
		return "visível", ""
	}
	return "", ""
}

func sourceHealthValue(h feed.SourceHealth) (value, note string) {
	switch h.Status {
	case feed.SourceOK:
		value = "OK"
	case feed.SourceFailed:
		value = "falhou"
	default:
		value = "nunca carregou"
	}

	var notes []string
	if !h.LastSuccess.IsZero() {
		notes = append(notes, "último sucesso "+relativeTime(h.LastSuccess))
	}
	if !h.LastErrorAt.IsZero() {
		notes = append(notes, "último erro "+relativeTime(h.LastErrorAt))
	}
	if h.LastError != "" {
		notes = append(notes, h.LastError)
	}
	return value, strings.Join(notes, " · ")
}

func (m Model) viewPanelRow(r panelRow, i, width int) string {
	if !r.selectable() {
		return "  " + titleStyle.Render(r.title)
	}

	prefix := "  "
	if i == m.panel.cursor {
		prefix = cursorStyle.Render("█ ")
	}

	value, note := m.rowValue(r)

	line := prefix + hintStyle.Render(fmt.Sprintf("%-*s", width, m.rowLabel(r))) +
		"  " + itemMetaStyle.Render("‹ ") + keyStyle.Render(value) +
		itemMetaStyle.Render(" ›")
	if note != "" {
		line += itemMetaStyle.Render("   " + note)
	}
	return line
}
