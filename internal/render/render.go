// Package render transforma blocos de artigo em texto estilizado para o
// terminal, com controle explícito de largura, indentação e cores.
package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/levyvix/cnnbr/internal/article"
)

// Paleta inspirada na identidade da CNN: vermelho de destaque sobre texto neutro.
var (
	Red    = lipgloss.AdaptiveColor{Light: "#B3121C", Dark: "#E8232E"}
	Text   = lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#E6E6E6"}
	Muted  = lipgloss.AdaptiveColor{Light: "#6B6B6B", Dark: "#8A8A8A"}
	Faint  = lipgloss.AdaptiveColor{Light: "#9A9A9A", Dark: "#5C5C5C"}
	Accent = lipgloss.AdaptiveColor{Light: "#0B5FA5", Dark: "#5FA8E8"}

	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(Text)
	metaStyle        = lipgloss.NewStyle().Foreground(Muted)
	sectionStyle     = lipgloss.NewStyle().Bold(true).Foreground(Red)
	bodyStyle        = lipgloss.NewStyle().Foreground(Text)
	headingStyle     = lipgloss.NewStyle().Bold(true).Foreground(Red)
	subheadingStyle  = lipgloss.NewStyle().Bold(true).Foreground(Text)
	quoteBarStyle    = lipgloss.NewStyle().Foreground(Red)
	quoteTextStyle   = lipgloss.NewStyle().Italic(true).Foreground(Muted)
	captionStyle     = lipgloss.NewStyle().Italic(true).Foreground(Faint)
	bulletStyle      = lipgloss.NewStyle().Foreground(Red)
	embedStyle       = lipgloss.NewStyle().Foreground(Faint)
	relatedStyle     = lipgloss.NewStyle().Foreground(Muted)
	relatedTextStyle = lipgloss.NewStyle().Foreground(Muted).Italic(true)
	linkStyle        = lipgloss.NewStyle().Foreground(Accent).Underline(true)
	ruleStyle        = lipgloss.NewStyle().Foreground(Faint)
)

// Layout define as medidas do texto do artigo.
type Layout struct {
	Width   int  // largura total disponível
	Indent  int  // recuo do corpo
	Justify bool // esticar espaços para alinhar a margem direita
}

// FitLayout escolhe a coluna de leitura para uma largura de terminal: no máximo
// maxColumn colunas de texto, centralizadas quando sobra espaço.
func FitLayout(width int, justify bool) Layout {
	col := width - 4
	if col > maxColumn {
		col = maxColumn
	}
	if col < 20 {
		col = 20
	}
	indent := (width - col) / 2
	if indent < 2 {
		indent = 2
	}
	return Layout{Width: indent + col, Indent: indent, Justify: justify}
}

// maxColumn limita a largura da linha de texto: colunas muito largas cansam a
// leitura, mesmo com terminal grande.
const maxColumn = 78

func (l Layout) column() int {
	w := l.Width - l.Indent
	if w > maxColumn {
		w = maxColumn
	}
	if w < 20 {
		w = 20
	}
	return w
}

// Header monta o cabeçalho da matéria (seção, data, tempo de leitura, título).
type Header struct {
	Section     string
	Title       string
	Author      string
	Published   string
	ReadingTime int
	Saved       bool
}

// RenderHeader devolve as linhas do cabeçalho.
func RenderHeader(h Header, l Layout) string {
	col := l.column()
	pad := strings.Repeat(" ", l.Indent)

	line := metaLine(h, col)

	title := titleStyle.Render(wrap(h.Title, col))

	var b strings.Builder
	b.WriteString(indent(line, pad))
	b.WriteString("\n\n")
	b.WriteString(indent(title, pad))
	b.WriteString("\n")
	b.WriteString(indent(ruleStyle.Render(strings.Repeat("─", col)), pad))
	b.WriteString("\n")
	return b.String()
}

// metaLine monta a linha de metadados na variante mais rica que couber em
// `col` colunas, degradando para versões mais curtas em terminais estreitos.
func metaLine(h Header, col int) string {
	section := strings.ToUpper(h.Section)
	star := ""
	if h.Saved {
		star = "  ★"
	}

	full := fmt.Sprintf("%d min de leitura", h.ReadingTime)
	short := fmt.Sprintf("%dmin", h.ReadingTime)

	variants := [][]string{
		{h.Published, h.Author, full},
		{h.Published, h.Author, short},
		{h.Published, short},
		{short},
		{},
	}

	for _, tail := range variants {
		parts := make([]string, 0, len(tail))
		for _, p := range tail {
			if p != "" {
				parts = append(parts, p)
			}
		}
		plain := section
		if len(parts) > 0 {
			plain += " · " + strings.Join(parts, " · ")
		}
		if lipgloss.Width(plain+star) <= col {
			line := sectionStyle.Render(section)
			if len(parts) > 0 {
				line += metaStyle.Render(" · " + strings.Join(parts, " · "))
			}
			return line + sectionStyle.Render(star)
		}
	}

	// Nem a seção sozinha cabe: trunca.
	return sectionStyle.Render(truncate(section, col))
}

// truncate corta um texto puro em `width` colunas, sinalizando com "…".
func truncate(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// RenderBody devolve o corpo do artigo formatado.
func RenderBody(blocks []article.Block, l Layout) string {
	col := l.column()
	pad := strings.Repeat(" ", l.Indent)

	var out []string
	for _, b := range blocks {
		switch b.Kind {
		case article.Heading:
			out = append(out, indent(headingStyle.Render(wrap(b.Text, col)), pad))
		case article.Subheading:
			out = append(out, indent(subheadingStyle.Render(wrap(b.Text, col)), pad))
		case article.Paragraph:
			text := wrap(b.Text, col)
			if l.Justify {
				text = justifyText(text, col)
			}
			out = append(out, indent(bodyStyle.Render(text), pad))
		case article.ListItem:
			body := wrap(b.Text, col-2)
			item := hang(bulletStyle.Render("• "), bodyStyle.Render(body), 2)
			out = append(out, indent(item, pad))
		case article.Quote:
			lines := strings.Split(wrap(b.Text, col-2), "\n")
			for i, ln := range lines {
				lines[i] = quoteBarStyle.Render("▌ ") + quoteTextStyle.Render(ln)
			}
			out = append(out, indent(strings.Join(lines, "\n"), pad))
		case article.Caption:
			out = append(out, indent(captionStyle.Render(wrap(b.Text, col)), pad))
		case article.Related:
			marker := relatedStyle.Render("→ Leia também: ")
			body := relatedTextStyle.Render(wrap(b.Text, col-lipgloss.Width(marker)))
			out = append(out, indent(hang(marker, body, lipgloss.Width(marker)), pad))
		case article.Embed:
			label := "[ " + b.Text + " — indisponível no terminal ]"
			if lipgloss.Width(label) > col {
				label = "[ " + b.Text + " ]"
			}
			out = append(out, indent(embedStyle.Render(wrap(label, col)), pad))
		case article.Rule:
			out = append(out, indent(ruleStyle.Render(strings.Repeat("·", col/2)), pad))
		}
	}
	return strings.Join(out, "\n\n")
}

// RenderLinks lista os links citados no artigo, numerados.
func RenderLinks(blocks []article.Block, l Layout) string {
	col := l.column()
	pad := strings.Repeat(" ", l.Indent)

	var links []string
	seen := make(map[string]bool)
	for _, b := range blocks {
		for _, href := range b.Links {
			if seen[href] {
				continue
			}
			seen[href] = true
			links = append(links, href)
		}
	}
	if len(links) == 0 {
		return ""
	}

	var out []string
	out = append(out, indent(ruleStyle.Render(strings.Repeat("─", col)), pad))
	out = append(out, indent(subheadingStyle.Render("Links na matéria"), pad))
	for i, href := range links {
		marker := metaStyle.Render(fmt.Sprintf("[%d] ", i+1))
		out = append(out, indent(hang(marker, linkStyle.Render(href), lipgloss.Width(marker)), pad))
	}
	return "\n\n" + strings.Join(out, "\n")
}

// wrap quebra o texto em `width` colunas, sempre entre palavras. Só quebra no
// meio de uma palavra quando ela sozinha não cabe na linha (URLs longas).
//
// O wordwrap do reflow não serve aqui: ele estoura o limite quando a quebra cai
// num breakpoint (`-`, `,`), e o hard wrap corretivo partia palavras ao meio.
func wrap(s string, width int) string {
	if width < 10 {
		width = 10
	}

	var lines []string
	var cur strings.Builder
	curW := 0

	flush := func() {
		lines = append(lines, cur.String())
		cur.Reset()
		curW = 0
	}

	for _, word := range strings.Fields(s) {
		w := lipgloss.Width(word)

		if curW > 0 && curW+1+w > width {
			flush()
		}

		// Palavra mais larga que a linha inteira: parte em pedaços.
		if w > width {
			if curW > 0 {
				flush()
			}
			for _, chunk := range splitWide(word, width) {
				cur.WriteString(chunk)
				curW = lipgloss.Width(chunk)
				if curW == width {
					flush()
				}
			}
			continue
		}

		if curW > 0 {
			cur.WriteString(" ")
			curW++
		}
		cur.WriteString(word)
		curW += w
	}
	if curW > 0 {
		flush()
	}

	return strings.Join(lines, "\n")
}

// justifyText distribui espaços extras entre as palavras para que todas as
// linhas — menos a última do parágrafo — terminem na margem direita.
func justifyText(text string, width int) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i == len(lines)-1 {
			break // a última linha do parágrafo fica alinhada à esquerda
		}
		words := strings.Fields(line)
		if len(words) < 2 {
			continue
		}
		gaps := len(words) - 1
		slack := width - lipgloss.Width(strings.Join(words, ""))
		// Linhas com folga demais ficariam esburacadas; melhor deixar ragged.
		if slack-gaps > gaps*2 {
			continue
		}

		var b strings.Builder
		for w, word := range words {
			b.WriteString(word)
			if w < gaps {
				spaces := slack / gaps
				if w < slack%gaps {
					spaces++
				}
				b.WriteString(strings.Repeat(" ", spaces))
			}
		}
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}

// splitWide corta uma palavra em pedaços de no máximo `width` colunas.
func splitWide(word string, width int) []string {
	var chunks []string
	var cur strings.Builder
	curW := 0

	for _, r := range word {
		rw := lipgloss.Width(string(r))
		if curW+rw > width {
			chunks = append(chunks, cur.String())
			cur.Reset()
			curW = 0
		}
		cur.WriteRune(r)
		curW += rw
	}
	if curW > 0 {
		chunks = append(chunks, cur.String())
	}
	return chunks
}

// indent aplica um prefixo em todas as linhas.
func indent(s, pad string) string {
	if pad == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = pad + ln
	}
	return strings.Join(lines, "\n")
}

// hang prefixa a primeira linha com marker e alinha as demais com `width`
// espaços — usado por itens de lista e links numerados.
func hang(marker, body string, width int) string {
	lines := strings.Split(body, "\n")
	pad := strings.Repeat(" ", width)
	for i := range lines {
		if i == 0 {
			lines[i] = marker + lines[i]
		} else {
			lines[i] = pad + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
