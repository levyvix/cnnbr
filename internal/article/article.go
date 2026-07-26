// Package article converte o HTML de content:encoded em blocos semânticos
// prontos para renderizar no terminal.
package article

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Kind identifica o tipo de bloco.
type Kind int

const (
	Paragraph Kind = iota
	Heading
	Subheading
	ListItem
	Quote
	Caption
	Rule
	Related // card "leia também" que a CNN embute no meio do texto
	Embed   // vídeo/tweet/iframe que não dá para exibir no terminal
)

// Block é uma unidade de conteúdo do artigo.
type Block struct {
	Kind  Kind
	Text  string
	Links []string // URLs citadas dentro do bloco, na ordem de aparição
}

// skipSelectors remove ruído do WordPress da CNN: anúncios, scripts,
// "leia também", newsletters e afins.
var skipSelectors = []string{
	"script", "style", "noscript", "form", "ins",
	".custom__ad__element", "[id^=mid]", ".ad", ".advertisement",
	".single__related", ".related", ".newsletter", ".tags",
	".social", ".share", ".breadcrumb", ".author__box",
}

// Parse transforma o HTML da matéria em blocos.
func Parse(rawHTML string) []Block {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return []Block{{Kind: Paragraph, Text: collapse(rawHTML)}}
	}

	for _, sel := range skipSelectors {
		doc.Find(sel).Remove()
	}

	var blocks []Block
	root := doc.Find("body")
	if root.Length() == 0 {
		root = doc.Selection
	}
	walk(root, &blocks)
	return dedupe(blocks)
}

func walk(sel *goquery.Selection, out *[]Block) {
	sel.Contents().Each(func(_ int, s *goquery.Selection) {
		node := s.Get(0)

		if node.Type == html.TextNode {
			// Texto solto entre tags: só vale se tiver conteúdo real.
			if t := collapse(node.Data); t != "" {
				appendBlock(out, Block{Kind: Paragraph, Text: t})
			}
			return
		}
		if node.Type != html.ElementNode {
			return
		}

		// Wrappers de embed e cards "leia também" são reconhecidos pela classe,
		// não pela tag: a CNN usa blockquote, div e iframe para todos eles.
		class := strings.ToLower(attr(s, "class"))
		switch {
		case strings.Contains(class, "wp-embedded-content"):
			// Card de matéria relacionada. Vem sempre em par (blockquote + iframe);
			// o iframe é descartado no case "iframe".
			if b, ok := textBlock(s, Related); ok {
				appendBlock(out, b)
			}
			return
		case node.Data != "iframe" && isEmbedContainer(s):
			appendBlock(out, Block{Kind: Embed, Text: embedLabel(s)})
			return
		}

		switch node.Data {
		case "p":
			if b, ok := textBlock(s, Paragraph); ok {
				appendBlock(out, b)
			}
		case "h1", "h2":
			if b, ok := textBlock(s, Heading); ok {
				appendBlock(out, b)
			}
		case "h3", "h4", "h5", "h6":
			if b, ok := textBlock(s, Subheading); ok {
				appendBlock(out, b)
			}
		case "blockquote":
			if b, ok := textBlock(s, Quote); ok {
				appendBlock(out, b)
			}
		case "ul", "ol":
			s.ChildrenFiltered("li").Each(func(_ int, li *goquery.Selection) {
				if b, ok := textBlock(li, ListItem); ok {
					appendBlock(out, b)
				}
			})
		case "figure":
			if cap, ok := textBlock(s.Find("figcaption"), Caption); ok {
				appendBlock(out, cap)
			} else if alt, exists := s.Find("img").Attr("alt"); exists && collapse(alt) != "" {
				appendBlock(out, Block{Kind: Caption, Text: collapse(alt)})
			}
		case "figcaption":
			if b, ok := textBlock(s, Caption); ok {
				appendBlock(out, b)
			}
		case "hr":
			appendBlock(out, Block{Kind: Rule})
		case "iframe":
			src := attr(s, "src")
			if strings.Contains(src, "embed=true") || strings.Contains(class, "wp-embedded") {
				return // duplicata do card de relacionada
			}
			appendBlock(out, Block{Kind: Embed, Text: embedLabel(s), Links: attrLinks(s, "src")})
		case "img", "picture", "br", "aside":
			// Imagens sem legenda e elementos laterais não somam no terminal.
		case "table":
			// Tabelas da CNN são raras e quase sempre placar; texto plano serve.
			if b, ok := textBlock(s, Paragraph); ok {
				appendBlock(out, b)
			}
		default:
			walk(s, out)
		}
	})
}

func textBlock(s *goquery.Selection, kind Kind) (Block, bool) {
	if s.Length() == 0 {
		return Block{}, false
	}
	text := collapse(s.Text())
	if text == "" {
		return Block{}, false
	}
	return Block{Kind: kind, Text: text, Links: attrLinks(s.Find("a"), "href")}, true
}

func attrLinks(s *goquery.Selection, attr string) []string {
	var links []string
	seen := make(map[string]bool)
	s.Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr(attr)
		href = strings.TrimSpace(href)
		if !ok || href == "" || strings.HasPrefix(href, "#") || seen[href] {
			return
		}
		seen[href] = true
		links = append(links, href)
	})
	return links
}

func attr(s *goquery.Selection, name string) string {
	v, _ := s.Attr(name)
	return v
}

// isEmbedContainer detecta os wrappers de vídeo/tweet usados pela CNN.
func isEmbedContainer(s *goquery.Selection) bool {
	class := strings.ToLower(attr(s, "class"))
	for _, marker := range []string{"dugout", "twitter-tweet", "instagram-media", "youtube", "video", "embed"} {
		if strings.Contains(class, marker) {
			return true
		}
	}
	return false
}

func embedLabel(s *goquery.Selection) string {
	class, _ := s.Attr("class")
	src, _ := s.Attr("src")
	hay := strings.ToLower(class + " " + src)
	switch {
	case strings.Contains(hay, "youtube"), strings.Contains(hay, "dugout"), strings.Contains(hay, "video"):
		return "vídeo"
	case strings.Contains(hay, "twitter"), strings.Contains(hay, "x.com"):
		return "post no X"
	case strings.Contains(hay, "instagram"):
		return "post no Instagram"
	default:
		return "conteúdo incorporado"
	}
}

func appendBlock(out *[]Block, b Block) {
	if b.Kind != Rule && b.Kind != Embed && b.Text == "" {
		return
	}
	*out = append(*out, b)
}

// dedupe remove blocos repetidos consecutivos e Rule/Embed duplicados, comuns
// quando o WordPress aninha wrappers.
func dedupe(in []Block) []Block {
	out := make([]Block, 0, len(in))
	for _, b := range in {
		if n := len(out); n > 0 {
			prev := out[n-1]
			if prev.Kind == b.Kind && prev.Text == b.Text {
				continue
			}
		}
		out = append(out, b)
	}
	// Remove Rule nas bordas.
	for len(out) > 0 && out[0].Kind == Rule {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1].Kind == Rule {
		out = out[:len(out)-1]
	}
	return out
}

var spaceReplacer = strings.NewReplacer(
	" ", " ", // nbsp
	"​", "", // zero-width space
	"\n", " ", "\r", " ", "\t", " ",
)

// collapse normaliza espaços em branco de um trecho de texto.
func collapse(s string) string {
	s = spaceReplacer.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// EstimateReadingTime estima o tempo de leitura direto do HTML, sem montar a
// árvore: a lista de manchetes precisa disso para 120 matérias de uma vez.
func EstimateReadingTime(rawHTML string) int {
	words, inTag, inWord := 0, false, false
	for _, r := range rawHTML {
		switch {
		case r == '<':
			inTag, inWord = true, false
		case r == '>':
			inTag = false
		case inTag:
		case r == ' ' || r == '\n' || r == '\t' || r == '\r':
			inWord = false
		case !inWord:
			inWord = true
			words++
		}
	}
	minutes := words / 200
	if minutes < 1 {
		minutes = 1
	}
	return minutes
}

// ReadingTime estima o tempo de leitura em minutos (200 palavras/min).
func ReadingTime(blocks []Block) int {
	words := 0
	for _, b := range blocks {
		words += len(strings.Fields(b.Text))
	}
	minutes := words / 200
	if minutes < 1 {
		minutes = 1
	}
	return minutes
}
