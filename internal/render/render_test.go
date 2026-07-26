package render

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/levyvix/cnnbr/internal/article"
	"github.com/levyvix/cnnbr/internal/feed"
)

func loadItems(t *testing.T) []feed.Item {
	t.Helper()
	f, err := os.Open("../feed/testdata/feed.xml")
	if err != nil {
		t.Skip("testdata do feed ausente")
	}
	defer f.Close()
	items, err := feed.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// TestRenderRespeitaLargura garante que nenhuma linha estoura a largura pedida
// em nenhuma das 60 matérias do feed real.
func TestRenderRespeitaLargura(t *testing.T) {
	items := loadItems(t)
	for _, width := range []int{40, 60, 80, 120} {
		l := Layout{Width: width, Indent: 2}
		for _, it := range items {
			blocks := article.Parse(it.HTML)
			out := RenderHeader(Header{
				Section: it.Section, Title: it.Title, Author: it.Author,
				Published: "26 jul 18:35", ReadingTime: article.ReadingTime(blocks),
			}, l) + RenderBody(blocks, l)

			for _, line := range strings.Split(out, "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Fatalf("largura %d: linha com %d colunas em %q\nlinha: %q", width, w, it.Title, line)
				}
			}
		}
	}
}

// TestNaoQuebraPalavras garante que o wrap só corta entre palavras: o texto
// renderizado, com espaços normalizados, tem de bater palavra por palavra com o
// original. Só URLs (mais largas que a coluna) podem ser partidas.
func TestNaoQuebraPalavras(t *testing.T) {
	items := loadItems(t)
	for _, width := range []int{40, 60, 100} {
		l := Layout{Width: width, Indent: 2}
		for _, it := range items {
			for _, b := range article.Parse(it.HTML) {
				if b.Kind != article.Paragraph {
					continue
				}
				original := strings.Fields(b.Text)
				rendered := strings.Fields(wrap(b.Text, l.column()))

				temPalavraLonga := false
				for _, w := range original {
					if lipgloss.Width(w) > l.column() {
						temPalavraLonga = true
					}
				}
				if temPalavraLonga {
					continue
				}

				if len(original) != len(rendered) {
					t.Fatalf("largura %d: %d palavras viraram %d em %q",
						width, len(original), len(rendered), it.Title)
				}
				for i := range original {
					if original[i] != rendered[i] {
						t.Fatalf("largura %d: palavra %d virou %q (era %q) em %q",
							width, i, rendered[i], original[i], it.Title)
					}
				}
			}
		}
	}
}

// TestJustificadoAlinhaMargem confere que o modo justificado preenche a coluna
// sem estourar e sem alterar as palavras.
func TestJustificadoAlinhaMargem(t *testing.T) {
	items := loadItems(t)
	for _, width := range []int{60, 100} {
		l := FitLayout(width, true)
		col := l.column()
		for _, it := range items[:10] {
			for _, b := range article.Parse(it.HTML) {
				if b.Kind != article.Paragraph {
					continue
				}
				lines := strings.Split(justifyText(wrap(b.Text, col), col), "\n")
				for i, line := range lines {
					if w := lipgloss.Width(line); w > col {
						t.Fatalf("linha justificada com %d > %d colunas: %q", w, col, line)
					}
					if i < len(lines)-1 && strings.HasSuffix(line, " ") {
						t.Fatalf("linha justificada termina em espaço: %q", line)
					}
				}
			}
		}
	}
}

// TestBlocosNaoVazios verifica que todo artigo produz conteúdo legível.
func TestBlocosNaoVazios(t *testing.T) {
	items := loadItems(t)
	for _, it := range items {
		blocks := article.Parse(it.HTML)
		texto := 0
		for _, b := range blocks {
			if b.Kind != article.Embed && b.Kind != article.Rule && strings.TrimSpace(b.Text) != "" {
				texto++
			}
		}
		if texto == 0 {
			t.Errorf("nenhum bloco de texto em %q (%s)", it.Title, it.Link)
		}
	}
}

// TestSemRuidoDeScript garante que scripts e anúncios não vazam para o texto.
func TestSemRuidoDeScript(t *testing.T) {
	items := loadItems(t)
	ruido := []string{"lazyload", "function(", "googletag", "custom__ad", "<div", "cnn-brazil.js"}
	for _, it := range items {
		for _, b := range article.Parse(it.HTML) {
			low := strings.ToLower(b.Text)
			for _, r := range ruido {
				if strings.Contains(low, r) {
					t.Errorf("ruído %q em %q: %.120q", r, it.Title, b.Text)
				}
			}
		}
	}
}

// TestDump imprime uma matéria formatada para inspeção visual:
// go test ./internal/render -run TestDump -v -args 3
func TestDump(t *testing.T) {
	if os.Getenv("DUMP") == "" {
		t.Skip("defina DUMP=<índice> para inspecionar")
	}
	idx, _ := strconv.Atoi(os.Getenv("DUMP"))
	items := loadItems(t)
	if idx >= len(items) {
		t.Fatalf("índice %d fora do feed (%d itens)", idx, len(items))
	}
	it := items[idx]
	blocks := article.Parse(it.HTML)
	l := Layout{Width: 80, Indent: 2}
	t.Log("\n" + RenderHeader(Header{
		Section: it.Section, Title: it.Title, Author: it.Author,
		Published: it.Published.Format("02 Jan 15:04"), ReadingTime: article.ReadingTime(blocks),
	}, l) + "\n" + RenderBody(blocks, l) + RenderLinks(blocks, l))
}
