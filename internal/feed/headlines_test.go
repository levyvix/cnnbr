package feed

import (
	"testing"
	"time"
)

func TestCoverageGroupsUsesSimilarityWindowAndSourceThreshold(t *testing.T) {
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	items := []Item{
		{SourceID: SourceCNNBrasilID, Source: SourceCNNBrasil, Title: "Banco Central mantém juros em 15%", Published: now},
		{SourceID: SourceCNNBrasilID, Source: SourceCNNBrasil, Title: "Banco Central mantém juros em 15 por cento", Published: now.Add(-30 * time.Minute)},
		{SourceID: SourceG1ID, Source: SourceG1, Title: "Banco Central mantém taxa de juros em 15%", Published: now.Add(-time.Hour)},
		{SourceID: SourceUOLID, Source: SourceUOL, Title: "Banco Central mantém juros em 15 por cento", Published: now.Add(-2 * time.Hour)},
		{SourceID: SourceFolhaID, Source: SourceFolha, Title: "Banco Central mantém juros em 15%", Published: now.Add(-25 * time.Hour)},
		{SourceID: SourceEstadaoID, Source: SourceEstadao, Title: "Flamengo vence clássico no Maracanã", Published: now},
	}

	groups := CoverageGroups(items, AllSources())
	if len(groups) != 1 {
		t.Fatalf("CoverageGroups devolveu %d grupos, quero 1: %#v", len(groups), groups)
	}
	group := groups[0]
	if group.Title != "Banco Central mantém juros em 15%" {
		t.Errorf("título do grupo = %q, quero a matéria mais recente", group.Title)
	}
	if group.SourceCount(items) != 3 {
		t.Errorf("fontes do grupo = %d, quero 3", group.SourceCount(items))
	}
	if len(group.Items) != 4 {
		t.Fatalf("grupo tem %d matérias, quero 4 dentro da janela sem apagar mesma fonte", len(group.Items))
	}
	for i, want := range []string{SourceCNNBrasilID, SourceCNNBrasilID, SourceG1ID, SourceUOLID} {
		if got := items[group.Items[i]].SourceID; got != want {
			t.Errorf("item %d veio da fonte %q, quero %q", i, got, want)
		}
	}
}

func TestCoverageGroupsDoesNotAlterOriginalItems(t *testing.T) {
	items := []Item{
		{SourceID: SourceCNNBrasilID, Title: "Congresso vota projeto de lei", Sections: []string{"politica"}},
		{SourceID: SourceG1ID, Title: "Congresso vota novo projeto de lei", Sections: []string{"politica", "economia"}},
		{SourceID: SourceUOLID, Title: "Congresso vota projeto de lei hoje", Sections: []string{"nacional"}},
	}
	before := append([]Item(nil), items...)

	groups := CoverageGroups(items, AllSources())
	if len(groups) != 1 {
		t.Fatalf("CoverageGroups = %d grupos, quero 1", len(groups))
	}
	for i := range items {
		if items[i].Title != before[i].Title || len(items[i].Sections) != len(before[i].Sections) {
			t.Fatalf("item %d foi alterado: antes %#v depois %#v", i, before[i], items[i])
		}
	}
}
