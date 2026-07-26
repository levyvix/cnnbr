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
	"strings"
	"time"
)

// FeedURL é o host que responde de fato ao /feed/ (www redireciona para cá).
const FeedURL = "https://admin.cnnbrasil.com.br/feed/"

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) cnnbr/0.1"

// Item é uma matéria do feed.
type Item struct {
	Title      string
	Link       string
	Author     string
	Published  time.Time
	Summary    string
	Section    string
	Categories []string
	HTML       string // conteúdo de content:encoded
}

// ID identifica a matéria de forma estável entre execuções. O <guid> do feed da
// CNN não é confiável (vários itens compartilham o mesmo valor), então usamos o
// link.
func (i Item) ID() string { return i.Link }

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
// deduplicados por link, do mais recente para o mais antigo.
func Fetch(ctx context.Context, client *http.Client, pages int) ([]Item, error) {
	if pages < 1 {
		pages = 1
	}

	seen := make(map[string]bool)
	var all []Item

	for page := 1; page <= pages; page++ {
		items, err := fetchPage(ctx, client, page)
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

	sort.SliceStable(all, func(a, b int) bool {
		return all[a].Published.After(all[b].Published)
	})
	return all, nil
}

func fetchPage(ctx context.Context, client *http.Client, page int) ([]Item, error) {
	u := FeedURL
	if page > 1 {
		u = fmt.Sprintf("%s?paged=%d", FeedURL, page)
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
	return Parse(resp.Body)
}

// Parse decodifica um feed RSS da CNN Brasil.
func Parse(r io.Reader) ([]Item, error) {
	var doc rss
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decodificando RSS: %w", err)
	}

	items := make([]Item, 0, len(doc.Items))
	for _, raw := range doc.Items {
		link := strings.TrimSpace(raw.Link)
		if link == "" {
			continue
		}
		items = append(items, Item{
			Title:      strings.TrimSpace(raw.Title),
			Link:       link,
			Author:     strings.TrimSpace(raw.Creator),
			Published:  parseDate(raw.PubDate),
			Summary:    strings.TrimSpace(raw.Description),
			Section:    SectionOf(link),
			Categories: cleanCategories(raw.Categories),
			HTML:       raw.Content,
		})
	}
	return items, nil
}

func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822} {
		if t, err := time.Parse(layout, s); err == nil {
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

// SectionOf extrai a seção a partir do caminho da URL da matéria. Os feeds por
// categoria da CNN caem silenciosamente no feed geral quando o slug não existe,
// então derivar da URL é mais confiável do que confiar em <category>.
func SectionOf(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return "Notícias"
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "Notícias"
	}
	slug := parts[0]
	if label, ok := sectionLabels[slug]; ok {
		return label
	}
	return strings.ToUpper(slug[:1]) + slug[1:]
}
