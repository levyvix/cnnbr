package feed

import (
	"os"
	"strings"
	"testing"
)

// openFixture abre o feed real usado nos testes. O arquivo não vai para o
// repositório (600 KB de matérias da CNN); baixe com `make testdata`.
func openFixture(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open("testdata/feed.xml")
	if err != nil {
		t.Skip("testdata/feed.xml ausente — rode `make testdata`")
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestParseRealFeed(t *testing.T) {
	items, err := Parse(openFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("nenhum item decodificado")
	}

	for i, it := range items {
		if it.Title == "" {
			t.Errorf("item %d sem título", i)
		}
		if it.HTML == "" {
			t.Errorf("item %d (%s) sem content:encoded", i, it.Title)
		}
		if it.Published.IsZero() {
			t.Errorf("item %d (%s) sem data válida", i, it.Title)
		}
		if it.Section == "" {
			t.Errorf("item %d sem seção", i)
		}
	}
}

func TestSections(t *testing.T) {
	slugs := make(map[string]bool)
	cats := make(map[int]bool)
	for i, s := range Sections {
		if s.Slug == "" || s.Name == "" {
			t.Errorf("seção %d incompleta: %+v", i, s)
		}
		if slugs[s.Slug] {
			t.Errorf("slug duplicado: %s", s.Slug)
		}
		slugs[s.Slug] = true

		if i == 0 {
			if s.Cat != 0 {
				t.Error("a primeira aba deve ser o feed geral (Cat 0)")
			}
			continue
		}
		if s.Cat <= 0 {
			t.Errorf("seção %s sem ID de categoria", s.Slug)
		}
		if cats[s.Cat] {
			t.Errorf("categoria duplicada em %s: %d", s.Slug, s.Cat)
		}
		cats[s.Cat] = true
	}
}

func TestSubsection(t *testing.T) {
	cases := []struct {
		link string
		cats []string
		want string
	}{
		{
			// A ordem das categorias é alfabética; o slug da URL decide.
			link: "https://www.cnnbrasil.com.br/esportes/brasileirao/remo-x-vitoria/",
			cats: []string{"Brasileirão", "Campeonato Brasileiro", "Remo", "Vitória (time)"},
			want: "Brasileirão",
		},
		{
			// Três níveis de categoria: vale o segundo segmento.
			link: "https://www.cnnbrasil.com.br/esportes/futebol/flamengo/flamengo-anuncia-bustos/",
			cats: []string{"Flamengo", "Futebol", "Futebol brasileiro"},
			want: "Futebol",
		},
		{
			// Matéria direto na seção: o segundo segmento é o slug do título,
			// não uma editoria.
			link: "https://www.cnnbrasil.com.br/politica/pt-aciona-stf-por-video-de-ia-de-bolsonaro/",
			cats: []string{"Política", "PT"},
			want: "",
		},
		{
			// A matéria não lista a categoria do caminho: formatamos o slug.
			link: "https://www.cnnbrasil.com.br/esportes/futebol/flamengo-anuncia-bustos/",
			cats: []string{"Futebol brasileiro", "Flamengo"},
			want: "Futebol",
		},
		{
			// Slug longo demais para ser editoria: provavelmente é título.
			link: "https://www.cnnbrasil.com.br/pop/uma-frase-bem-longa-aqui/mais/",
			cats: []string{"Celebridades"},
			want: "",
		},
		{
			link: "https://www.cnnbrasil.com.br/politica/",
			cats: []string{"Política"},
			want: "",
		},
	}
	for _, c := range cases {
		if got := subsectionOf(c.link, c.cats); got != c.want {
			t.Errorf("subsectionOf(%q) = %q, quero %q", c.link, got, c.want)
		}
	}
}

// TestSubsectionRealFeed exige que toda subseção seja uma categoria de verdade
// da própria matéria. Sem isso, o slug do título vira rótulo — era o que
// acontecia em Política e Internacional, onde a URL não tem subcategoria.
func TestSubsectionRealFeed(t *testing.T) {
	items, err := Parse(openFixture(t))
	if err != nil {
		t.Fatal(err)
	}

	comSub := 0
	for _, it := range items {
		if it.Subsection == "" {
			continue
		}
		comSub++

		// A subseção tem de ser o segundo segmento da URL — nunca o slug do
		// título, que é o que aparecia antes em Política e Internacional.
		parts := strings.Split(strings.Trim(strings.SplitN(it.Link, ".com.br", 2)[1], "/"), "/")
		if len(parts) < 3 {
			t.Errorf("subseção %q numa URL sem subcategoria: %s", it.Subsection, it.Link)
			continue
		}
		if slugify(it.Subsection) != parts[1] {
			t.Errorf("subseção %q não corresponde ao caminho %q em %q", it.Subsection, parts[1], it.Title)
		}
		if palavras := len(strings.Fields(it.Subsection)); palavras > 3 {
			t.Errorf("subseção suspeita de ser título: %q em %q", it.Subsection, it.Title)
		}
	}

	if comSub == 0 {
		t.Error("nenhuma matéria com subseção — a detecção deve estar quebrada")
	}
	t.Logf("%d de %d matérias têm subseção", comSub, len(items))
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Brasileirão":           "brasileirao",
		"Vitória (time)":        "vitoria-time",
		"Eleições 2026":         "eleicoes-2026",
		"Futebol Internacional": "futebol-internacional",
		"Saúde":                 "saude",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, quero %q", in, got, want)
		}
	}
}

func TestSectionOf(t *testing.T) {
	cases := map[string]string{
		"https://www.cnnbrasil.com.br/esportes/brasileirao/x/": "Esportes",
		"https://www.cnnbrasil.com.br/politica/y/":             "Política",
		"https://www.cnnbrasil.com.br/pop/z/":                  "Pop",
		"https://www.cnnbrasil.com.br/":                        "Notícias",
		"não é url::":                                          "Notícias",
	}
	for link, want := range cases {
		if got := SectionOf(link); got != want {
			t.Errorf("SectionOf(%q) = %q, quero %q", link, got, want)
		}
	}
}

func TestDedupeAndOrder(t *testing.T) {
	items, err := Parse(openFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, it := range items {
		if seen[it.ID()] {
			t.Errorf("ID duplicado: %s", it.ID())
		}
		seen[it.ID()] = true
	}
}
