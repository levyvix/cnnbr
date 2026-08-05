package feed

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCacheSeparatesSourcesAndRetainsSevenDays(t *testing.T) {
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	previousNow := nowFn
	nowFn = func() time.Time { return now }
	t.Cleanup(func() { nowFn = previousNow })

	c := Cache{Dir: filepath.Join(t.TempDir(), "cache"), TTL: time.Hour}
	wantCNN := Item{
		Source:     SourceCNNBrasil,
		SourceID:   SourceCNNBrasilID,
		Title:      "Uma notícia",
		Link:       "https://cnn.example/recente",
		Author:     "Repórter",
		Published:  now.Add(-time.Hour),
		Summary:    "Um resumo.",
		Section:    "Política",
		Subsection: "Congresso",
		Categories: []string{"Política", "Congresso"},
		HTML:       "<p>O corpo.</p>",
	}
	cnnItems := []Item{
		wantCNN,
		{Source: SourceCNNBrasil, Link: "https://cnn.example/antiga", Published: now.Add(-8 * 24 * time.Hour)},
		{Source: SourceCNNBrasil, Link: "https://cnn.example/sem-data"},
	}
	g1Items := []Item{{Source: "G1", Link: "https://g1.example/recente", Published: now.Add(-time.Hour)}}

	if err := c.Save(SourceCNNBrasil, "home", cnnItems, now); err != nil {
		t.Fatal(err)
	}
	if err := c.Save("G1", "home", g1Items, now); err != nil {
		t.Fatal(err)
	}

	gotCNN, _, err := c.Load(SourceCNNBrasil, "home")
	if err != nil {
		t.Fatal(err)
	}
	if len(gotCNN) != 2 || !reflect.DeepEqual(gotCNN[0], wantCNN) || gotCNN[1].Link != "https://cnn.example/sem-data" {
		t.Errorf("cache da CNN = %#v, quero só a notícia recente da CNN", gotCNN)
	}

	gotG1, _, err := c.Load("G1", "home")
	if err != nil {
		t.Fatal(err)
	}
	if len(gotG1) != 1 || gotG1[0].Link != "https://g1.example/recente" {
		t.Errorf("cache do G1 = %#v, quero só a notícia do G1", gotG1)
	}

	if _, _, err := c.Load(SourceCNNBrasil, "home"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Load("Outra CNN", "home"); err == nil {
		t.Error("fontes com chaves diferentes não deveriam compartilhar o cache")
	}
	if c.path("CNN Brasil", "home") == c.path("CNN-Brasil", "home") {
		t.Error("fontes parecidas não deveriam compartilhar o caminho do cache")
	}
}
