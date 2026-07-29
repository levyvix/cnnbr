package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"

	"github.com/levyvix/cnnbr/internal/article"
	"github.com/levyvix/cnnbr/internal/render"
)

var (
	brandStyle    = lipgloss.NewStyle().Bold(true).Foreground(render.Red)
	headerMeta    = lipgloss.NewStyle().Foreground(render.Muted)
	cursorStyle   = lipgloss.NewStyle().Bold(true).Foreground(render.Red)
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(render.Text)
	readStyle     = lipgloss.NewStyle().Foreground(render.Muted)
	itemMetaStyle = lipgloss.NewStyle().Foreground(render.Faint)
	sectionStyle  = lipgloss.NewStyle().Foreground(render.Red)
	savedStyle    = lipgloss.NewStyle().Foreground(render.Red)
	keyStyle      = lipgloss.NewStyle().Bold(true).Foreground(render.Text)
	hintStyle     = lipgloss.NewStyle().Foreground(render.Muted)
	errStyle      = lipgloss.NewStyle().Bold(true).Foreground(render.Red)
	okStyle       = lipgloss.NewStyle().Foreground(render.Accent)
	dividerStyle  = lipgloss.NewStyle().Foreground(render.Faint)
	tabActive     = lipgloss.NewStyle().Bold(true).Foreground(render.Red)
	tabIdle       = lipgloss.NewStyle().Foreground(render.Muted)
	tabPending    = lipgloss.NewStyle().Foreground(render.Faint)
	helpBoxStyle  = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(render.Red).
			Padding(1, 2)
)

func (m Model) View() string {
	if m.width == 0 {
		return "carregando…"
	}
	if m.showHelp {
		return m.viewHelp()
	}

	t := m.tabs[m.active]

	var body string
	switch {
	case t.loading && len(t.items) == 0:
		body = m.centered("buscando " + t.section.Name + "…")
	case t.err != nil && len(t.items) == 0:
		body = m.centered("não consegui carregar " + t.section.Name + " — r tenta de novo")
	case m.mode == modeReader:
		body = m.reader.View()
	default:
		body = m.viewList()
	}

	return m.clip(strings.Join([]string{
		m.viewHeader(),
		body,
		m.viewStatus(),
	}, "\n"))
}

func (m Model) viewHeader() string {
	left := brandStyle.Render("CNN Brasil")

	t := m.tabs[m.active]

	var meta []string
	if m.mode == modeReader {
		meta = append(meta, fmt.Sprintf("%d/%d", t.cursor+1, len(t.view)))
	} else {
		meta = append(meta, plural(len(t.view), "matéria", "matérias"))
	}
	if m.onlySaved {
		meta = append(meta, "★ favoritos")
	}
	if t.loading {
		meta = append(meta, "atualizando…")
	} else if !t.fetchedAt.IsZero() {
		label := "atualizado " + relativeTime(t.fetchedAt)
		if t.fromCache {
			label += " (cache)"
		}
		meta = append(meta, label)
	}

	right := headerMeta.Render(strings.Join(meta, " · "))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right + "\n" +
		m.viewTabs() + "\n" +
		dividerStyle.Render(strings.Repeat("─", m.width))
}

// viewTabs desenha a barra de seções, deslizando a janela visível quando as
// abas não cabem na largura do terminal.
func (m Model) viewTabs() string {
	labels := make([]string, len(m.visible))
	for pos, idx := range m.visible {
		t := m.tabs[idx]
		name := t.section.Name
		switch {
		case idx == m.active:
			labels[pos] = tabActive.Render("▌" + name)
		case t.loading:
			labels[pos] = tabPending.Render(" " + name + "…")
		case t.loaded:
			labels[pos] = tabIdle.Render(" " + name)
		default:
			labels[pos] = tabPending.Render(" " + name)
		}
	}
	if len(labels) == 0 {
		return ""
	}

	const sep = "  "
	width := func(i int) int { return lipgloss.Width(labels[i]) }

	// Reserva espaço para as setas de continuação nas duas pontas.
	avail := m.width - 4
	if avail < 1 {
		avail = 1
	}

	active := m.visiblePos()
	start, end := active, active
	total := width(active)
	for {
		grew := false
		if end+1 < len(labels) && total+len(sep)+width(end+1) <= avail {
			end++
			total += len(sep) + width(end)
			grew = true
		}
		if start-1 >= 0 && total+len(sep)+width(start-1) <= avail {
			start--
			total += len(sep) + width(start)
			grew = true
		}
		if !grew {
			break
		}
	}

	bar := strings.Join(labels[start:end+1], sep)
	if start > 0 {
		bar = tabPending.Render("‹") + bar
	}
	if end < len(labels)-1 {
		bar += tabPending.Render(" ›")
	}
	return bar
}

func (m Model) viewList() string {
	height := m.bodyHeight()
	t := m.tabs[m.active]
	if len(t.view) == 0 {
		if m.onlySaved {
			return m.centered("nenhuma matéria salva em " + t.section.Name + " — use f para favoritar")
		}
		return m.centered("nada em " + t.section.Name)
	}

	per := m.itemsPerPage()
	end := t.top + per
	if end > len(t.view) {
		end = len(t.view)
	}

	var lines []string
	for i := t.top; i < end; i++ {
		lines = append(lines, m.viewItem(i)...)
	}

	// Preenche o resto da área para a barra de status ficar colada embaixo.
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:height], "\n")
}

func (m Model) viewItem(i int) []string {
	t := m.tabs[m.active]
	it := t.items[t.view[i]]
	isRead := m.cfg.Store.IsRead(it.ID())
	isSaved := m.cfg.Store.IsSaved(it.ID())

	// A barra ocupa as duas linhas do item: a seleção precisa ser legível de
	// relance, sem depender de um caractere pequeno numa lista densa.
	prefix, indent := "  ", "  "
	if i == t.cursor {
		prefix = cursorStyle.Render("█ ")
		indent = prefix
	}

	titleWidth := m.width - 2 - 2
	if titleWidth < 10 {
		titleWidth = 10
	}

	style := titleStyle
	if isRead {
		style = readStyle
	}
	title := style.Render(truncate.StringWithTail(it.Title, uint(titleWidth), "…"))

	// Na Home o rótulo útil é a seção; dentro de uma seção ela é redundante,
	// então mostramos a editoria específica (Brasileirão, Eleições, …).
	label := it.Section
	if t.section.Cat != 0 && it.Subsection != "" {
		label = it.Subsection
	}

	meta := []string{strings.ToUpper(label)}
	if rel := relativeTime(it.Published); rel != "" {
		meta = append(meta, rel)
	}
	meta = append(meta, fmt.Sprintf("%d min", article.EstimateReadingTime(it.HTML)))

	metaLine := sectionStyle.Render(meta[0]) + itemMetaStyle.Render(" · "+strings.Join(meta[1:], " · "))
	if isSaved {
		metaLine += savedStyle.Render("  ★")
	}

	return []string{
		prefix + title,
		indent + metaLine,
		"",
	}
}

// renderArticle monta o conteúdo do leitor para a matéria aberta.
func (m Model) renderArticle() string {
	it := m.reading()
	l := render.FitLayout(m.width, m.prefs.Justify)

	head := render.RenderHeader(render.Header{
		Section:     it.Section,
		Title:       it.Title,
		Author:      it.Author,
		Published:   relativeTime(it.Published),
		ReadingTime: article.ReadingTime(m.blocks),
		Saved:       m.cfg.Store.IsSaved(it.ID()),
	}, l)

	return head + "\n" + render.RenderBody(m.blocks, l) + render.RenderLinks(m.blocks, l) + "\n"
}

func (m Model) viewStatus() string {
	t := m.tabs[m.active]

	right := ""
	switch {
	case m.mode == modeReader:
		right = headerMeta.Render(fmt.Sprintf("%3.0f%%", m.reader.ScrollPercent()*100))
	case len(t.view) > 0:
		right = headerMeta.Render(fmt.Sprintf("%d/%d", t.cursor+1, len(t.view)))
	}

	// As dicas disputam a linha com o indicador da direita: só o essencial
	// fica fixo, o resto vive em `?`.
	avail := m.width - lipgloss.Width(right) - 2

	var left string
	switch {
	case m.statusText != "" && m.statusErr:
		left = errStyle.Render("! " + m.statusText)
	case m.statusText != "":
		left = okStyle.Render("✓ " + m.statusText)
	case m.mode == modeReader:
		left = m.hints(avail, "j/k", "rolar", "J/K", "próxima", "o", "browser", "esc", "voltar", "?", "ajuda")
	default:
		left = m.hints(avail, "enter", "ler", "f", "salvar", "s", "favoritos", "?", "ajuda")
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		// Corta as dicas em vez de quebrar a linha.
		left = truncate.String(left, uint(max(0, m.width-lipgloss.Width(right)-1)))
		gap = 1
	}

	return dividerStyle.Render(strings.Repeat("─", m.width)) + "\n" +
		left + strings.Repeat(" ", gap) + right
}

// hints monta pares tecla/descrição em até `avail` colunas. Quando não cabem
// todos, descarta a partir do penúltimo: o último é sempre `? ajuda`, que é o
// caminho para tudo que foi cortado.
func (m Model) hints(avail int, pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keyStyle.Render(pairs[i])+hintStyle.Render(" "+pairs[i+1]))
	}

	for len(parts) > 1 {
		joined := strings.Join(parts, hintStyle.Render("  ·  "))
		if lipgloss.Width(joined) <= avail {
			return joined
		}
		parts = append(parts[:len(parts)-2], parts[len(parts)-1])
	}
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func (m Model) viewHelp() string {
	rows := [][2]string{
		{"tab / shift+tab", "próxima / seção anterior"},
		{"h / l / ← / →", "trocar de seção (na lista)"},
		{"1…9 / 0", "pular direto para uma seção"},
		{"j / k / ↓ / ↑", "navegar ou rolar"},
		{"ctrl+d / ctrl+u", "meia página"},
		{"g / G", "início / fim"},
		{"enter", "abrir a matéria"},
		{"esc / q", "voltar (ou sair, na lista)"},
		{"J / K", "próxima / anterior sem sair do leitor"},
		{"o", "abrir no navegador"},
		{"y", "copiar o link"},
		{"f", "salvar nos favoritos"},
		{"m", "alternar lida / não lida"},
		{"s", "ver só os favoritos"},
		{"t", "alternar texto justificado"},
		{"r", "recarregar o feed"},
		{"?", "esta ajuda"},
		{"ctrl+c", "sair"},
	}

	width := 0
	for _, r := range rows {
		if w := lipgloss.Width(r[0]); w > width {
			width = w
		}
	}

	var b strings.Builder
	b.WriteString(brandStyle.Render("cnnbr — atalhos"))
	b.WriteString("\n\n")
	for _, r := range rows {
		b.WriteString(keyStyle.Render(fmt.Sprintf("%-*s", width, r[0])))
		b.WriteString(hintStyle.Render("  " + r[1]))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(hintStyle.Render("qualquer tecla fecha"))

	box := helpBoxStyle.Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) centered(text string) string {
	return lipgloss.Place(m.width, m.bodyHeight(), lipgloss.Center, lipgloss.Center,
		hintStyle.Render(text))
}

// truncateStyled corta uma linha já estilizada, preservando as sequências ANSI.
func truncateStyled(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return truncate.StringWithTail(s, uint(width), "…")
}
