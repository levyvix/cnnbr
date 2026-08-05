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
	"unicode"

	"golang.org/x/net/html/charset"
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

const UOLFeedURL = "https://rss.uol.com.br/feed/noticias.xml"

const SourceUOL = "UOL Notícias"

const SourceUOLID = "uol"

var UOLSource = Source{ID: SourceUOLID, Name: SourceUOL, FeedURL: UOLFeedURL}

const FolhaFeedURL = "https://feeds.folha.uol.com.br/emcimadahora/rss091.xml"

const SourceFolha = "Folha de S.Paulo"

const SourceFolhaID = "folha"

var FolhaSource = Source{ID: SourceFolhaID, Name: SourceFolha, FeedURL: FolhaFeedURL}

const EstadaoFeedURL = "https://www.estadao.com.br/arc/outboundfeeds/feeds/rss/sections/geral/"

const SourceEstadao = "Estadão"

const SourceEstadaoID = "estadao"

var EstadaoSource = Source{ID: SourceEstadaoID, Name: SourceEstadao, FeedURL: EstadaoFeedURL}

const MetropolesFeedURL = "https://www.metropoles.com/feed"

const SourceMetropoles = "Metrópoles"

const SourceMetropolesID = "metropoles"

var MetropolesSource = Source{ID: SourceMetropolesID, Name: SourceMetropoles, FeedURL: MetropolesFeedURL}

const Poder360FeedURL = "https://www.poder360.com.br/feed/"

const SourcePoder360 = "Poder360"

const SourcePoder360ID = "poder360"

var Poder360Source = Source{ID: SourcePoder360ID, Name: SourcePoder360, FeedURL: Poder360FeedURL}

// ExternalSources são as fontes com RSS oficial validado que entram na Home.
// As demais seções continuam sendo recortes do feed da CNN Brasil até que a
// classificação global de fontes seja implementada.
// Na validação de 2026-08-05, o RSS oficial de O Globo estava vazio e o R7 não
// expunha um RSS; ambos ficam fora até que um feed oficial seja validado.
var ExternalSources = []Source{
	G1Source,
	UOLSource,
	FolhaSource,
	EstadaoSource,
	MetropolesSource,
	Poder360Source,
}

// AllSources devolve as fontes canônicas, na ordem fixa do leitor.
func AllSources() []Source {
	return append([]Source{CNNBrasilSource}, ExternalSources...)
}

func (s Source) key() string {
	if s.ID != "" {
		return s.ID
	}
	return s.Name
}

// SourcesFor devolve as fontes que alimentam uma seção. Todas as seções usam as
// fontes validadas; a filtragem por seção acontece depois do parse, pela
// taxonomia global.
func SourcesFor(s Section) []Source {
	return AllSources()
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
	Sections   []string
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
	if !sourceUsesCNNCategories(source) {
		cat = 0
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

func sourceUsesCNNCategories(source Source) bool {
	return source.key() == SourceCNNBrasilID
}

// ItemsForSection filtra uma lista já parseada pela taxonomia global. A Home
// aceita tudo; seções específicas só aceitam matérias classificadas com
// confiança para aquele slug.
func ItemsForSection(section Section, items []Item) []Item {
	if section.Cat == 0 {
		return items
	}
	out := make([]Item, 0, len(items))
	for _, item := range items {
		if ItemInSection(item, section.Slug) {
			out = append(out, item)
		}
	}
	return out
}

func ItemInSection(item Item, slug string) bool {
	for _, section := range SectionsOf(item) {
		if section == slug {
			return true
		}
	}
	return false
}

// SectionsOf devolve as seções globais da matéria, recalculando quando a
// matéria veio de um cache antigo que ainda não guardava esse campo.
func SectionsOf(item Item) []string {
	if len(item.Sections) > 0 {
		return item.Sections
	}
	return Classify(item)
}

// Classify classifica a matéria nas seções globais. Categorias oficiais do RSS
// vêm primeiro e podem mapear a matéria para mais de uma seção; quando elas
// faltam, usamos regras determinísticas sobre URL, título, resumo e corpo
// disponível. Sem confiança, devolve nil para que a matéria apareça só na Home.
func Classify(item Item) []string {
	seen := make(map[string]bool)
	var sections []string
	add := func(slug string) {
		if slug == "" || seen[slug] {
			return
		}
		seen[slug] = true
		sections = append(sections, slug)
	}

	for _, category := range item.Categories {
		if slug, ok := globalCategorySlugs[slugify(category)]; ok {
			add(slug)
		}
	}
	if len(sections) > 0 {
		return sections
	}
	text := strings.ToLower(strings.Join([]string{item.Title, item.Summary, plainText(item.HTML)}, " "))
	for _, rule := range sectionRules {
		for _, term := range rule.terms {
			if containsTerm(text, term) {
				add(rule.slug)
				break
			}
		}
	}
	if len(sections) == 0 {
		return nil
	}
	return sections
}

func containsTerm(text, term string) bool {
	for start := 0; ; {
		idx := strings.Index(text[start:], term)
		if idx < 0 {
			return false
		}
		idx += start
		before := idx == 0 || !isWordRune(runeBefore(text, idx))
		afterIdx := idx + len(term)
		after := afterIdx == len(text) || !isWordRune(runeAfter(text, afterIdx))
		if before && after {
			return true
		}
		start = idx + len(term)
		if start >= len(text) {
			return false
		}
	}
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func runeBefore(s string, idx int) rune {
	var prev rune
	for _, r := range s[:idx] {
		prev = r
	}
	return prev
}

func runeAfter(s string, idx int) rune {
	for _, r := range s[idx:] {
		return r
	}
	return 0
}

type sectionRule struct {
	slug  string
	terms []string
}

var sectionRules = []sectionRule{
	{"eleicoes", []string{"eleição", "eleições", "eleicoes", "eleitoral", "tse", "urna"}},
	{"politica", []string{"política", "politica", "político", "politico", "congresso", "senado", "câmara", "stf", "planalto", "lula", "bolsonaro"}},
	{"economia", []string{"economia", "mercado", "dólar", "bolsa", "inflação", "juros", "banco central", "ibovespa", "pib"}},
	{"esportes", []string{"esporte", "esportes", "futebol", "brasileirão", "brasileirao", "libertadores", "olimpíada", "jogo", "time", "gol"}},
	{"tecnologia", []string{"tecnologia", "inteligência artificial", "celular", "software", "aplicativo", "internet"}},
	{"saude", []string{"saúde", "saude", "hospital", "médico", "vacina", "covid", "ans", "doença"}},
	{"internacional", []string{"internacional", "eua", "estados unidos", "china", "rússia", "ucrânia", "israel", "gaza", "trump"}},
	{"pop", []string{"pop", "celebridade", "filme", "série", "música", "show", "festival", "cantor", "atriz", "ator"}},
	{"nacional", []string{"brasil", "brasileiro", "governo federal", "polícia federal", "rio de janeiro", "são paulo"}},
}

var globalCategorySlugs = map[string]string{
	"politica":              "politica",
	"poder":                 "politica",
	"brasil":                "nacional",
	"nacional":              "nacional",
	"internacional":         "internacional",
	"mundo":                 "internacional",
	"economia":              "economia",
	"mercado":               "economia",
	"macroeconomia":         "economia",
	"negocios":              "economia",
	"investimentos":         "economia",
	"esporte":               "esportes",
	"esportes":              "esportes",
	"futebol":               "esportes",
	"futebol-brasileiro":    "esportes",
	"futebol-internacional": "esportes",
	"campeonato-brasileiro": "esportes",
	"brasileirao":           "esportes",
	"outros-esportes":       "esportes",
	"volei":                 "esportes",
	"automobilismo":         "esportes",
	"pop":                   "pop",
	"entretenimento":        "pop",
	"cultura":               "pop",
	"celebridades":          "pop",
	"cnnpop":                "pop",
	"cinema":                "pop",
	"streaming":             "pop",
	"tv":                    "pop",
	"tecnologia":            "tecnologia",
	"saude":                 "saude",
	"eleicoes":              "eleicoes",
	"eleicoes-2026":         "eleicoes",
}

func plainText(rawHTML string) string {
	var b strings.Builder
	inTag := false
	for _, r := range rawHTML {
		switch r {
		case '<':
			inTag = true
			b.WriteRune(' ')
		case '>':
			inTag = false
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// Parse decodifica um feed RSS da CNN Brasil.
func Parse(r io.Reader) ([]Item, error) {
	return ParseSource(r, CNNBrasilSource)
}

// ParseSource decodifica um feed RSS e associa a fonte a cada item.
func ParseSource(r io.Reader, source Source) ([]Item, error) {
	var doc rss
	decoder := xml.NewDecoder(r)
	decoder.CharsetReader = charset.NewReaderLabel
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decodificando RSS: %w", err)
	}

	outItems := make([]Item, 0, len(doc.Items))
	for _, raw := range doc.Items {
		link := strings.TrimSpace(raw.Link)
		if link == "" {
			continue
		}
		cats := cleanCategories(raw.Categories)
		item := Item{
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
		}
		item.Sections = Classify(item)
		outItems = append(outItems, item)
	}
	return outItems, nil
}

func parseDate(s string) time.Time {
	s = normalizeDate(strings.TrimSpace(s))
	for _, layout := range []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		"02 Jan 2006 15:04:05 -0700",
		"02 Jan 2006 15:04:05 MST",
		"02 Jan 06 15:04 -0700",
	} {
		if t, err := time.Parse(layout, s); nil == err {
			return t.Local()
		}
	}
	return time.Time{}
}

func normalizeDate(s string) string {
	for portuguese, english := range map[string]string{
		"Dom": "Sun",
		"Seg": "Mon",
		"Ter": "Tue",
		"Qua": "Wed",
		"Qui": "Thu",
		"Sex": "Fri",
		"Sáb": "Sat",
		"Jan": "Jan",
		"Fev": "Feb",
		"Mar": "Mar",
		"Abr": "Apr",
		"Mai": "May",
		"Jun": "Jun",
		"Jul": "Jul",
		"Ago": "Aug",
		"Set": "Sep",
		"Out": "Oct",
		"Nov": "Nov",
		"Dez": "Dec",
	} {
		s = strings.Replace(s, portuguese, english, 1)
	}
	return s
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
