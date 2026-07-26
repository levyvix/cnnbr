package feed

import (
	"os"
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
			// Sem categoria correspondente, cai no slug formatado.
			link: "https://www.cnnbrasil.com.br/pop/musica/show-da-xuxa/",
			cats: []string{"Celebridades"},
			want: "Musica",
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
