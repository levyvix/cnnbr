package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/levyvix/cnnbr/internal/feed"
	"github.com/levyvix/cnnbr/internal/prefs"
	"github.com/levyvix/cnnbr/internal/store"
)

// newTestModel monta um Model sem rede: a busca de cada seção vira um comando
// que o teste simplesmente não executa.
func newTestModel(t *testing.T) Model {
	t.Helper()
	return New(Config{Store: store.New(filepath.Join(t.TempDir(), "state.json"))}, prefs.Defaults())
}

func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

// press envia uma sequência de teclas e devolve o modelo resultante.
func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, key := range keys {
		next, _ := m.Update(keyMsg(key))
		m = next.(Model)
	}
	return m
}

func activeSlug(m Model) string { return m.tabs[m.active].section.Slug }

func TestTabCyclesThroughSections(t *testing.T) {
	last := feed.Sections[len(feed.Sections)-1].Slug

	tests := []struct {
		name string
		keys []string
		want string
	}{
		{"tab avança", []string{"tab"}, feed.Sections[1].Slug},
		{"tab duas vezes", []string{"tab", "tab"}, feed.Sections[2].Slug},
		{"shift+tab dá a volta pelo início", []string{"shift+tab"}, last},
		{"tab dá a volta pelo fim", append(repeat("tab", len(feed.Sections)-1), "tab"), feed.Sections[0].Slug},
		{"l avança", []string{"l"}, feed.Sections[1].Slug},
		{"h dá a volta pelo início", []string{"h"}, last},
		{"l e h voltam ao ponto de partida", []string{"l", "l", "h", "h"}, feed.Sections[0].Slug},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := press(t, newTestModel(t), tc.keys...)
			if got := activeSlug(m); got != tc.want {
				t.Errorf("seção ativa = %q, quero %q", got, tc.want)
			}
		})
	}
}

func TestDigitsJumpToSection(t *testing.T) {
	digits := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0"}

	for i, digit := range digits {
		if i >= len(feed.Sections) {
			break
		}
		want := feed.Sections[i].Slug
		t.Run(digit, func(t *testing.T) {
			m := press(t, newTestModel(t), digit)
			if got := activeSlug(m); got != want {
				t.Errorf("%q levou a %q, quero %q", digit, got, want)
			}
		})
	}
}

func TestDigitBeyondSectionsDoesNothing(t *testing.T) {
	if len(feed.Sections) >= 10 {
		t.Skip("todas as dez posições de dígito estão ocupadas")
	}
	m := press(t, newTestModel(t), "0")
	if got := activeSlug(m); got != feed.Sections[0].Slug {
		t.Errorf("seção ativa = %q, quero %q", got, feed.Sections[0].Slug)
	}
}

func TestJustifyToggleIsTheOnlyPrefChosen(t *testing.T) {
	m := newTestModel(t)
	if !m.Chosen().Empty() {
		t.Fatal("um modelo recém-criado não tem preferência para gravar")
	}

	m = press(t, m, "t")

	chosen := m.Chosen()
	if chosen.Justify == nil {
		t.Fatal("t deveria registrar a justificação como escolhida")
	}
	if want := !prefs.Defaults().Justify; *chosen.Justify != want {
		t.Errorf("justificação escolhida = %v, quero %v", *chosen.Justify, want)
	}
	// Só a justificação: gravar as outras eternizaria no arquivo o que veio de
	// flag nesta execução.
	if chosen.Pages != nil || chosen.TTL != nil || chosen.RetentionDays != nil {
		t.Errorf("t mexeu em preferência que não pediu: %+v", chosen)
	}
}

func TestHomeIdentifiesItemsBySource(t *testing.T) {
	m := newTestModel(t)
	m.tabs[0].items = make([]feed.Item, len(feed.ExternalSources))
	for i, source := range feed.ExternalSources {
		m.tabs[0].items[i] = feed.Item{
			Source:   source.Name,
			SourceID: source.ID,
			Title:    "Uma matéria de " + source.Name,
			Link:     "https://example.com/" + source.ID + "/uma-materia",
		}
	}
	m.tabs[0].loaded = true
	m.rebuildView(0)

	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	view := next.(Model).View()
	for _, source := range feed.ExternalSources {
		if !strings.Contains(view, source.Name) {
			t.Errorf("a Home não identificou %q na lista:\n%s", source.Name, view)
		}
	}
}

func TestSectionHeadlinesShowSource(t *testing.T) {
	m := press(t, newTestModel(t), "2") // Política
	m.tabs[m.active].items = []feed.Item{{
		Source:   feed.SourceG1,
		SourceID: feed.SourceG1ID,
		Title:    "Uma matéria do G1",
		Link:     "https://g1.globo.com/politica/noticia/2026/08/05/uma-materia.ghtml",
		Sections: []string{"politica"},
	}}
	m.tabs[m.active].loaded = true
	m.rebuildView(m.active)

	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	view := next.(Model).View()
	if !strings.Contains(view, feed.SourceG1) {
		t.Fatalf("seção não identificou a fonte %q:\n%s", feed.SourceG1, view)
	}
}

func TestHeadlinesTabShowsCoverageGroups(t *testing.T) {
	m := headlinesModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	view := next.(Model).View()
	if !strings.Contains(view, "Manchetes") || !strings.Contains(view, "Banco Central mantém juros em 15% (3 fontes)") {
		t.Fatalf("Manchetes não mostrou grupo com contagem de fontes:\n%s", view)
	}
	if !strings.Contains(view, "Banco Central mantém juros") {
		t.Fatalf("Manchetes não mostrou título da pauta:\n%s", view)
	}
}

func TestHeadlinesReaderSeparatesSourcesAndSharesReadState(t *testing.T) {
	m := headlinesModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = next.(Model)
	m = press(t, m, "enter")
	if m.mode != modeReader {
		t.Fatalf("enter abriu modo %v, quero leitor", m.mode)
	}
	view := m.renderArticle()
	for _, want := range []string{feed.SourceCNNBrasil, feed.SourceG1, feed.SourceUOL, "Corpo CNN", "Corpo G1", "Corpo UOL"} {
		if !strings.Contains(view, want) {
			t.Fatalf("leitor de Manchetes não mostrou %q:\n%s", want, view)
		}
	}
	for _, item := range m.tabs[m.active].items {
		if !m.cfg.Store.IsRead(item.ID()) {
			t.Fatalf("matéria %s não compartilhou estado lido", item.ID())
		}
	}
}

func TestHeadlinesFavoriteTogglesEveryGroupedItem(t *testing.T) {
	m := headlinesModel(t)
	m = press(t, m, "f")
	for _, item := range m.tabs[m.active].items {
		if !m.cfg.Store.IsSaved(item.ID()) {
			t.Fatalf("matéria %s não foi salva pelo grupo", item.ID())
		}
	}

	m = press(t, m, "enter", "f")
	for _, item := range m.tabs[m.active].items {
		if m.cfg.Store.IsSaved(item.ID()) {
			t.Fatalf("matéria %s não foi removida pelo leitor do grupo", item.ID())
		}
	}
}

func headlinesModel(t *testing.T) Model {
	t.Helper()
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	m := newTestModel(t)
	for i := range m.tabs {
		if m.tabs[i].section.Slug == feed.HeadlinesSlug {
			m.active = i
			break
		}
	}
	m.tabs[m.active].items = []feed.Item{
		{Source: feed.SourceCNNBrasil, SourceID: feed.SourceCNNBrasilID, Title: "Banco Central mantém juros em 15%", Link: "https://cnn.example/juros", Published: now, HTML: "<p>Corpo CNN.</p>"},
		{Source: feed.SourceG1, SourceID: feed.SourceG1ID, Title: "Banco Central mantém taxa de juros em 15%", Link: "https://g1.example/juros", Published: now.Add(-time.Hour), HTML: "<p>Corpo G1.</p>"},
		{Source: feed.SourceUOL, SourceID: feed.SourceUOLID, Title: "Banco Central mantém juros em 15 por cento", Link: "https://uol.example/juros", Published: now.Add(-2 * time.Hour), HTML: "<p>Corpo UOL.</p>"},
	}
	m.tabs[m.active].loaded = true
	m.rebuildView(m.active)
	return m
}

func repeat(key string, n int) []string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = key
	}
	return keys
}
