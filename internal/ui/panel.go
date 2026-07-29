package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/levyvix/cnnbr/internal/prefs"
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

// panelRows é a lista plana do painel, na ordem em que aparece: subtítulos de
// grupo, que o cursor pula, e as preferências.
var panelRows = []panelRow{
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
}

// panelRow é uma linha do painel: ou um subtítulo de grupo, que o cursor pula,
// ou uma preferência.
type panelRow struct {
	title string
	pref  *preference
}

// firstPrefRow é onde o cursor pousa ao abrir o painel.
func firstPrefRow() int {
	for i, r := range panelRows {
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
		m.cyclePref(1)
	case "h", "left":
		m.cyclePref(-1)
	}
	return m, nil
}

func (m *Model) cyclePref(delta int) {
	pref := panelRows[m.panel.cursor].pref
	if pref == nil {
		return
	}
	pref.apply(m, pref.next(m.prefs, delta))
}

// movePanelCursor anda até a próxima preferência, parando nas pontas.
func (m *Model) movePanelCursor(delta int) {
	for i := m.panel.cursor + delta; i >= 0 && i < len(panelRows); i += delta {
		if panelRows[i].pref != nil {
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

	width := 0
	for _, r := range panelRows {
		if r.pref != nil {
			if w := lipgloss.Width(r.pref.label); w > width {
				width = w
			}
		}
	}

	end := m.panel.top + height
	if end > len(panelRows) {
		end = len(panelRows)
	}

	lines := make([]string, 0, height)
	for i := m.panel.top; i < end; i++ {
		lines = append(lines, m.viewPanelRow(i, width))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:height], "\n")
}

func (m Model) viewPanelRow(i, width int) string {
	r := panelRows[i]
	if r.pref == nil {
		return "  " + titleStyle.Render(r.title)
	}

	prefix := "  "
	if i == m.panel.cursor {
		prefix = cursorStyle.Render("█ ")
	}

	line := prefix + hintStyle.Render(fmt.Sprintf("%-*s", width, r.pref.label)) +
		"  " + itemMetaStyle.Render("‹ ") + keyStyle.Render(r.pref.display(m.prefs)) +
		itemMetaStyle.Render(" ›")
	if r.pref.note != "" {
		line += itemMetaStyle.Render("   " + r.pref.note)
	}
	return line
}
