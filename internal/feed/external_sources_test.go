package feed

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
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
	if len(section) != 1 || section[0] != CNNBrasilSource {
		t.Errorf("seção não-Home = %#v, quero apenas CNN", section)
	}
}
