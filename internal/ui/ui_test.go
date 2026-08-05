package ui

import (
	"path/filepath"
	"strings"
	"testing"

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
	m.tabs[0].items = []feed.Item{{
		Source:   feed.SourceG1,
		SourceID: feed.SourceG1ID,
		Title:    "Uma matéria do G1",
		Link:     "https://g1.globo.com/politica/noticia/uma-materia.ghtml",
	}}
	m.tabs[0].loaded = true
	m.rebuildView(0)

	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	view := next.(Model).View()
	if !strings.Contains(view, "G1") {
		t.Errorf("a Home não identificou a fonte na lista:\n%s", view)
	}
}

func repeat(key string, n int) []string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = key
	}
	return keys
}
