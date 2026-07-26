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
