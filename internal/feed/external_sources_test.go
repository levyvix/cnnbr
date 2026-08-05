package feed

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExternalSourcesHaveFixturesAndParse(t *testing.T) {
	for _, source := range ExternalSources {
		source := source
		t.Run(source.ID, func(t *testing.T) {
			f, err := os.Open(filepath.Join("testdata", source.ID+".xml"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = f.Close() })

			items, err := ParseSource(f, source)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) == 0 {
				t.Fatal("fixture não tem matérias")
			}
			for _, item := range items {
				if item.Source != source.Name || item.SourceID != source.ID {
					t.Errorf("fonte = %q/%q, quero %q/%q", item.Source, item.SourceID, source.Name, source.ID)
				}
				if item.Title == "" || item.Link == "" || item.Published.IsZero() {
					t.Errorf("matéria incompleta: %#v", item)
				}
			}
		})
	}
}

func TestParseSupportsLatin1RSS(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="ISO-8859-1"?><rss><channel><item><title>Caf` + "\xe9" + `</title><link>https://example.com/cafe</link><pubDate>Wed, 05 Aug 2026 12:00:00 -0300</pubDate></item></channel></rss>`)
	items, err := ParseSource(bytes.NewReader(data), UOLSource)
	if err != nil {
		t.Fatal(err)
	}
	if got := items[0].Title; got != "Café" {
		t.Errorf("título Latin-1 = %q, quero %q", got, "Café")
	}
}

func TestParseSupportsExternalDateFormats(t *testing.T) {
	want := time.Date(2026, time.August, 5, 15, 20, 0, 0, time.FixedZone("BRT", -3*60*60))
	for _, tc := range []struct {
		name   string
		date   string
		source Source
	}{
		{name: "UOL em português", date: "Qua, 05 Ago 2026 15:20:00 -0300", source: UOLSource},
		{name: "Folha sem dia da semana", date: "05 Aug 2026 15:20:00 -0300", source: FolhaSource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, err := ParseSource(bytes.NewBufferString(`<rss><channel><item>
				<title>Uma notícia</title>
				<link>https://example.com/noticia</link>
				<pubDate>`+tc.date+`</pubDate>
			</item></channel></rss>`), tc.source)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 {
				t.Fatalf("Parse devolveu %d itens, quero 1", len(items))
			}
			if !items[0].Published.Equal(want) {
				t.Errorf("data = %v, quero %v", items[0].Published, want)
			}
		})
	}
}

func TestOfficialCategoriesMapForEverySource(t *testing.T) {
	tests := []struct {
		source Source
		cats   []string
		want   []string
	}{
		{CNNBrasilSource, []string{"Política", "Eleições 2026"}, []string{"politica", "eleicoes"}},
		{G1Source, []string{"Tecnologia", "Saúde"}, []string{"tecnologia", "saude"}},
		{UOLSource, []string{"Economia", "Internacional"}, []string{"economia", "internacional"}},
		{FolhaSource, []string{"Poder", "Mercado"}, []string{"politica", "economia"}},
		{EstadaoSource, []string{"Esportes", "Cultura"}, []string{"esportes", "pop"}},
		{MetropolesSource, []string{"Brasil", "Celebridades"}, []string{"nacional", "pop"}},
		{Poder360Source, []string{"Poder", "Eleições"}, []string{"politica", "eleicoes"}},
	}

	for _, tc := range tests {
		t.Run(tc.source.ID, func(t *testing.T) {
			var rss bytes.Buffer
			rss.WriteString(`<rss><channel><item><title>Uma notícia</title><link>https://example.com/noticia</link><pubDate>Wed, 05 Aug 2026 12:00:00 -0300</pubDate>`)
			for _, category := range tc.cats {
				rss.WriteString(`<category>`)
				rss.WriteString(category)
				rss.WriteString(`</category>`)
			}
			rss.WriteString(`</item></channel></rss>`)

			items, err := ParseSource(&rss, tc.source)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !hasSection(items[0].Sections, want) {
					t.Fatalf("%s categorias %v deram seções %v, quero %q", tc.source.Name, tc.cats, items[0].Sections, want)
				}
			}
		})
	}
}

func TestHomeAggregatesAllValidatedExternalSources(t *testing.T) {
	home := SourcesFor(Sections[0])
	if len(home) != len(ExternalSources)+1 {
		t.Fatalf("Home tem %d fontes, quero CNN + %d fontes externas", len(home), len(ExternalSources))
	}
	if home[0] != CNNBrasilSource {
		t.Errorf("a CNN deveria ser a primeira fonte da Home: %#v", home[0])
	}
	for i, source := range ExternalSources {
		if home[i+1] != source {
			t.Errorf("fonte externa %d = %#v, quero %#v", i, home[i+1], source)
		}
	}

	section := SourcesFor(Sections[1])
	if len(section) != len(home) {
		t.Fatalf("seção não-Home tem %d fontes, quero as %d da Home", len(section), len(home))
	}
	for i, source := range home {
		if section[i] != source {
			t.Errorf("fonte da seção %d = %#v, quero %#v", i, section[i], source)
		}
	}
}
