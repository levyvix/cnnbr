// Package feed busca e decodifica o RSS da CNN Brasil.
//
// O feed expõe o artigo inteiro em <content:encoded>, então não é necessário
// fazer scraping das páginas: uma requisição traz 60 matérias completas.
package feed

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FeedURL é o host que responde de fato ao /feed/ (www redireciona para cá).
const FeedURL = "https://admin.cnnbrasil.com.br/feed/"

// SourceCNNBrasil é a fonte dos feeds da CNN Brasil.
const SourceCNNBrasil = "CNN Brasil"

// SourceCNNBrasilID é a identidade estável da fonte da CNN.
const SourceCNNBrasilID = "cnnbrasil"

// Source descreve uma fonte RSS que o leitor pode consumir.
type Source struct {
	ID      string
	Name    string
	FeedURL string
}

// CNNBrasilSource é a fonte configurada atualmente.
var CNNBrasilSource = Source{ID: SourceCNNBrasilID, Name: SourceCNNBrasil, FeedURL: FeedURL}

// G1FeedURL é o RSS oficial do feed geral do G1.
const G1FeedURL = "https://g1.globo.com/rss/g1/"

// SourceG1 é o nome exibido para a fonte do G1.
const SourceG1 = "g1"

// SourceG1ID é a identidade estável da fonte do G1.
const SourceG1ID = "g1"

// G1Source é a fonte externa que aparece na Home.
var G1Source = Source{ID: SourceG1ID, Name: SourceG1, FeedURL: G1FeedURL}

func (s Source) key() string {
	if s.ID != "" {
		return s.ID
	}
	return s.Name
}

// SourcesFor devolve as fontes que alimentam uma seção. Só a Home agrega o
// G1; as demais continuam sendo recortes do feed da CNN Brasil.
func SourcesFor(s Section) []Source {
	if s.Cat == 0 {
		return []Source{CNNBrasilSource, G1Source}
	}
	return []Source{CNNBrasilSource}
}

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) cnnbr/0.1"

// Item é uma matéria do feed.
type Item struct {
	Source     string
	SourceID   string
	Title      string
	Link       string
	Author     string
	Published  time.Time
	Summary    string
	Section    string
	Subsection string // editoria mais específica, ex. "Brasileirão" em Esportes
	Categories []string
	HTML       string // conteúdo de content:encoded
}

// ID identifica a matéria de forma estável entre execuções. O <guid> do feed da
// CNN não é confiável (vários itens compartilham o mesmo valor), então usamos a
// fonte e o link.
func (i Item) ID() string {
	source := i.SourceID
	if source == "" {
		source = i.Source
	}
	if source == "" {
		source = SourceCNNBrasilID
	}
	return source + "\x00" + i.Link
}

type rss struct {
	Items []struct {
		Title       string   `xml:"title"`
		Link        string   `xml:"link"`
		Creator     string   `xml:"creator"`
		PubDate     string   `xml:"pubDate"`
		Description string   `xml:"description"`
		Categories  []string `xml:"category"`
		Content     string   `xml:"encoded"`
	} `xml:"channel>item"`
}

// Fetch busca `pages` páginas do feed (60 itens cada) e devolve os itens
// deduplicados por link, do mais recente para o mais antigo. Com cat > 0 o feed
// vem filtrado por categoria.
func Fetch(ctx context.Context, client *http.Client, cat, pages int) ([]Item, error) {
	return FetchSource(ctx, client, CNNBrasilSource, cat, pages)
}

// FetchSource busca páginas de uma fonte RSS e devolve os itens deduplicados.
func FetchSource(ctx context.Context, client *http.Client, source Source, cat, pages int) ([]Item, error) {
	if pages < 1 {
		pages = 1
	}

	seen := make(map[string]bool)
	var all []Item

	for page := 1; page <= pages; page++ {
		items, err := fetchPage(ctx, client, source, cat, page)
		if err != nil {
			// Uma página posterior que falha não invalida o que já veio.
			if len(all) > 0 {
				break
			}
			return nil, err
		}
		for _, it := range items {
			if seen[it.ID()] {
				continue
			}
			seen[it.ID()] = true
			all = append(all, it)
		}
	}

	// recent items first
	sort.SliceStable(all, func(a, b int) bool {
		return all[a].Published.After(all[b].Published)
	})
	return all, nil
}

func fetchPage(ctx context.Context, client *http.Client, source Source, cat, page int) ([]Item, error) {
	q := url.Values{}
	if cat > 0 {
		q.Set("cat", strconv.Itoa(cat))
	}
	if page > 1 {
		q.Set("paged", strconv.Itoa(page))
	}
	u := source.FeedURL
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/xml;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("buscando feed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feed respondeu %s", resp.Status)
	}
	return ParseSource(resp.Body, source)
}

// Parse decodifica um feed RSS da CNN Brasil.
func Parse(r io.Reader) ([]Item, error) {
	return ParseSource(r, CNNBrasilSource)
}

// ParseSource decodifica um feed RSS e associa a fonte a cada item.
func ParseSource(r io.Reader, source Source) ([]Item, error) {
	var doc rss
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decodificando RSS: %w", err)
	}

	outItems := make([]Item, 0, len(doc.Items))
	for _, raw := range doc.Items {
		link := strings.TrimSpace(raw.Link)
		if link == "" {
			continue
		}
		cats := cleanCategories(raw.Categories)
		outItems = append(outItems, Item{
			Source:     source.Name,
			SourceID:   source.key(),
			Title:      strings.TrimSpace(raw.Title),
			Link:       link,
			Author:     strings.TrimSpace(raw.Creator),
			Published:  parseDate(raw.PubDate),
			Summary:    strings.TrimSpace(raw.Description),
			Section:    SectionOf(link),
			Subsection: subsectionOf(link, cats),
			Categories: cats,
			HTML:       raw.Content,
		})
	}
	return outItems, nil
}

func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822} {
		if t, err := time.Parse(layout, s); nil == err {
			return t.Local()
		}
	}
	return time.Time{}
}

func cleanCategories(in []string) []string {
	out := make([]string, 0, len(in))
	for _, c := range in {
		c = strings.TrimSpace(c)
		// O feed traz marcadores internos como "-agencia-cnn-".
		if c == "" || strings.HasPrefix(c, "-") {
			continue
		}
		out = append(out, c)
	}
	return out
}

// subsectionOf devolve a editoria mais específica da matéria, ou "" quando não
// existe uma.
//
// Só há subseção quando o caminho tem pelo menos três segmentos
// (/esportes/brasileirao/remo-x-vitoria/): boa parte das matérias vive direto
// na seção (/politica/pt-aciona-stf/), e aí o segundo segmento é o slug do
// próprio título — foi o que já apareceu como "PT ACIONA STF POR VIDEO DE IA".
//
// O nome bonito, com acentos, vem da <category> correspondente. A ordem das
// categorias no feed é alfabética, então pegar a primeira daria "Vitória
// (time)" em vez de "Brasileirão". Sem categoria correspondente devolvemos ""
// em vez de inventar um rótulo a partir do slug.
func subsectionOf(link string, cats []string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 3 || parts[1] == "" {
		return ""
	}

	slug := parts[1]
	for _, c := range cats {
		if slugify(c) == slug {
			return c
		}
	}

	// A matéria nem sempre lista a categoria do caminho ("Futebol brasileiro"
	// no lugar de "Futebol"). Com três segmentos o slug é categoria de verdade,
	// então dá para formatá-lo — perdendo os acentos. O limite de palavras é o
	// cinto de segurança contra slug de título.
	if strings.Count(slug, "-") > 2 {
		return ""
	}
	return strings.ToUpper(slug[:1]) + strings.ReplaceAll(slug[1:], "-", " ")
}

// slugify aproxima a transformação que o WordPress faz no nome da categoria:
// minúsculas, sem acentos, espaços viram hífen.
func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '_':
			b.WriteRune('-')
		default:
			if folded, ok := accentFolding[r]; ok {
				b.WriteRune(folded)
			}
			// Pontuação e parênteses somem, como no WordPress.
		}
	}
	return strings.Trim(b.String(), "-")
}

var accentFolding = map[rune]rune{
	'á': 'a', 'à': 'a', 'ã': 'a', 'â': 'a', 'ä': 'a',
	'é': 'e', 'ê': 'e', 'è': 'e', 'ë': 'e',
	'í': 'i', 'î': 'i', 'ì': 'i', 'ï': 'i',
	'ó': 'o', 'õ': 'o', 'ô': 'o', 'ò': 'o', 'ö': 'o',
	'ú': 'u', 'û': 'u', 'ù': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n',
}

// sectionLabels traduz o primeiro segmento da URL para um rótulo de seção.
var sectionLabels = map[string]string{
	"nacional":           "Nacional",
	"internacional":      "Internacional",
	"politica":           "Política",
	"economia":           "Economia",
	"esportes":           "Esportes",
	"eleicoes":           "Eleições",
	"pop":                "Pop",
	"saude":              "Saúde",
	"lifestyle":          "Lifestyle",
	"auto":               "Auto",
	"tecnologia":         "Tecnologia",
	"entretenimento":     "Entretenimento",
	"viagemegastronomia": "Viagem",
	"noticias":           "Notícias",
}

// SlugOf é o primeiro segmento do caminho da matéria. Quando ele coincide com o
// Slug de uma Section, é a seção a que a matéria pertence — o que permite ao
// leitor tirar da Home as matérias de uma seção que ele ocultou. A CNN publica
// também em caminhos que não são seção do leitor (/lifestyle/, /auto/), e para
// esses o slug simplesmente não casa com nenhuma.
func SlugOf(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return parts[0]
}

// SectionOf extrai a seção a partir do caminho da URL da matéria — o rótulo
// mostrado em cada item da lista. É mais confiável que <category>, que traz
// dezenas de tags por matéria ("Corinthians", "-agencia-cnn-", …).
func SectionOf(link string) string {
	slug := SlugOf(link)
	if slug == "" {
		return "Notícias"
	}
	if label, ok := sectionLabels[slug]; ok {
		return label
	}
	return strings.ToUpper(slug[:1]) + slug[1:]
}
