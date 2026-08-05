package feed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
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

func TestGetSourcesMixesHomeItemsByDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		if strings.HasPrefix(r.URL.Path, "/g1") {
			_, _ = w.Write([]byte(`<rss><channel><item>
				<title>G1 mais recente</title><link>https://g1.example/mais-recente</link>
				<pubDate>Wed, 05 Aug 2026 12:00:00 -0300</pubDate>
			</item></channel></rss>`))
			return
		}
		_, _ = w.Write([]byte(`<rss><channel><item>
			<title>CNN anterior</title><link>https://cnn.example/anterior</link>
			<pubDate>Wed, 05 Aug 2026 11:00:00 -0300</pubDate>
		</item></channel></rss>`))
	}))
	t.Cleanup(server.Close)

	sources := []Source{
		{ID: "cnn-test", Name: "CNN Brasil", FeedURL: server.URL + "/cnn"},
		{ID: "g1-test", Name: "g1", FeedURL: server.URL + "/g1"},
	}
	result := GetSources(context.Background(), server.Client(), Cache{Dir: t.TempDir(), TTL: time.Hour}, sources, Sections[0], 1, true)

	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("GetSources devolveu %d itens, quero 2", len(result.Items))
	}
	if result.Items[0].Source != "g1" || result.Items[1].Source != "CNN Brasil" {
		t.Errorf("ordem da Home = %#v, quero G1 antes da CNN", result.Items)
	}
	if result.Items[0].ID() == result.Items[1].ID() {
		t.Error("fontes diferentes compartilharam o estado da matéria")
	}
}

func TestGetSourcesKeepsCNNWhenG1Fails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/g1") {
			http.Error(w, "indisponível", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`<rss><channel><item>
			<title>CNN disponível</title><link>https://cnn.example/disponível</link>
			<pubDate>Wed, 05 Aug 2026 11:00:00 -0300</pubDate>
		</item></channel></rss>`))
	}))
	t.Cleanup(server.Close)

	result := GetSources(context.Background(), server.Client(), Cache{Dir: t.TempDir(), TTL: time.Hour}, []Source{
		{ID: "cnn-test", Name: "CNN Brasil", FeedURL: server.URL + "/cnn"},
		{ID: "g1-test", Name: "g1", FeedURL: server.URL + "/g1"},
	}, Sections[0], 1, true)

	if result.Err != nil {
		t.Fatalf("falha do G1 não deveria impedir a CNN: %v", result.Err)
	}
	if len(result.Items) != 1 || result.Items[0].Source != "CNN Brasil" {
		t.Fatalf("itens após falha do G1 = %#v, quero a CNN", result.Items)
	}
}
