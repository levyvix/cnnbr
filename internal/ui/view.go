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

	var body string
	switch {
	case m.loading && len(m.items) == 0:
		body = m.centered("buscando as manchetes da CNN Brasil…")
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

	var meta []string
	if m.mode == modeReader {
		meta = append(meta, fmt.Sprintf("%d/%d", m.cursor+1, len(m.view)))
	} else {
		meta = append(meta, plural(len(m.view), "matéria", "matérias"))
	}
	if m.onlySaved {
		meta = append(meta, "★ favoritos")
	}
	if m.loading {
		meta = append(meta, "atualizando…")
	} else if !m.fetchedAt.IsZero() {
		label := "atualizado " + relativeTime(m.fetchedAt)
		if m.fromCache {
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
		dividerStyle.Render(strings.Repeat("─", m.width))
}

func (m Model) viewList() string {
	height := m.bodyHeight()
	if len(m.view) == 0 {
		if m.onlySaved {
			return m.centered("nenhuma matéria salva ainda — use f para favoritar")
		}
		return m.centered("nada no feed")
	}

	per := m.itemsPerPage()
	end := m.top + per
	if end > len(m.view) {
		end = len(m.view)
	}

	var lines []string
	for i := m.top; i < end; i++ {
		lines = append(lines, m.viewItem(i)...)
	}

	// Preenche o resto da área para a barra de status ficar colada embaixo.
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:height], "\n")
}

func (m Model) viewItem(i int) []string {
	it := m.items[m.view[i]]
	isRead := m.cfg.Store.IsRead(it.ID())
	isSaved := m.cfg.Store.IsSaved(it.ID())

	number := fmt.Sprintf("%3d ", i+1)
	marker := "  "
	if i == m.cursor {
		marker = cursorStyle.Render("▸ ")
	}

	prefix := marker + itemMetaStyle.Render(number)
	titleWidth := m.width - lipgloss.Width(prefix) - 2
	if titleWidth < 10 {
		titleWidth = 10
	}

	style := titleStyle
	if isRead {
		style = readStyle
	}
	title := style.Render(truncate.StringWithTail(it.Title, uint(titleWidth), "…"))

	meta := []string{strings.ToUpper(it.Section)}
	if rel := relativeTime(it.Published); rel != "" {
		meta = append(meta, rel)
	}
	meta = append(meta, fmt.Sprintf("%d min", article.EstimateReadingTime(it.HTML)))

	metaLine := sectionStyle.Render(meta[0]) + itemMetaStyle.Render(" · "+strings.Join(meta[1:], " · "))
	if isSaved {
		metaLine += savedStyle.Render("  ★")
	}

	indent := strings.Repeat(" ", lipgloss.Width(prefix))
	return []string{
		prefix + title,
		indent + metaLine,
		"",
	}
}

// renderArticle monta o conteúdo do leitor para a matéria aberta.
func (m Model) renderArticle() string {
	it := m.items[m.readingIdx]
	l := render.FitLayout(m.width, m.cfg.Justify)

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
	var left string
	switch {
	case m.statusText != "" && m.statusErr:
		left = errStyle.Render("! " + m.statusText)
	case m.statusText != "":
		left = okStyle.Render("✓ " + m.statusText)
	case m.mode == modeReader:
		left = m.hints(
			"j/k", "rolar", "J/K", "próxima", "o", "browser", "y", "copiar", "f", "salvar", "esc", "voltar", "?", "ajuda",
		)
	default:
		left = m.hints(
			"j/k", "navegar", "enter", "ler", "o", "browser", "y", "copiar", "f", "salvar", "s", "favoritos", "r", "recarregar", "?", "ajuda",
		)
	}

	right := ""
	if m.mode == modeReader {
		right = headerMeta.Render(fmt.Sprintf("%3.0f%%", m.reader.ScrollPercent()*100))
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

// hints monta pares tecla/descrição, cortando os que não couberem.
func (m Model) hints(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keyStyle.Render(pairs[i])+hintStyle.Render(" "+pairs[i+1]))
	}

	for len(parts) > 1 {
		joined := strings.Join(parts, hintStyle.Render("  ·  "))
		if lipgloss.Width(joined) <= m.width {
			return joined
		}
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func (m Model) viewHelp() string {
	rows := [][2]string{
		{"j / k / ↓ / ↑", "navegar ou rolar"},
		{"ctrl+d / ctrl+u", "meia página"},
		{"g / G", "início / fim"},
		{"enter / l", "abrir a matéria"},
		{"esc / h / q", "voltar (ou sair, na lista)"},
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
