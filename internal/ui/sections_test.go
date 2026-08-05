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

// cursorOnSection põe o cursor do painel na linha da seção pedida.
func cursorOnSection(t *testing.T, m Model, slug string) Model {
	t.Helper()
	for i, r := range m.panelRows() {
		if r.section != nil && m.tabs[*r.section].section.Slug == slug {
			m.panel.cursor = i
			return m
		}
	}
	t.Fatalf("o painel não tem a seção %q", slug)
	return m
}

// visibleSlugs são os slugs da barra de abas, na ordem em que aparecem.
func visibleSlugs(m Model) []string {
	slugs := make([]string, 0, len(m.visible))
	for _, idx := range m.visible {
		slugs = append(slugs, m.tabs[idx].section.Slug)
	}
	return slugs
}

// modelWithSections monta um modelo com as seções já configuradas, como se o
// arquivo de preferências as tivesse trazido.
func modelWithSections(t *testing.T, sections []prefs.Section) Model {
	t.Helper()
	p := prefs.Defaults()
	p.Sections = sections
	return New(Config{Store: store.New(filepath.Join(t.TempDir(), "state.json"))}, p)
}

func TestPanelListsEverySectionInOrder(t *testing.T) {
	m := openPanel(t, newTestModel(t))

	var listed []string
	for _, r := range m.panelRows() {
		if r.section != nil {
			listed = append(listed, m.tabs[*r.section].section.Slug)
		}
	}
	if len(listed) != len(feed.Sections) {
		t.Fatalf("o painel lista %d seções, quero as %d do binário", len(listed), len(feed.Sections))
	}
	for i, s := range feed.Sections {
		if listed[i] != s.Slug {
			t.Errorf("seção %d = %q, quero %q", i, listed[i], s.Slug)
		}
	}

	if view := m.View(); !strings.Contains(view, "Seções") {
		t.Error("o painel não desenhou o grupo Seções")
	}
}

func TestSpaceHidesAndShowsASection(t *testing.T) {
	m := cursorOnSection(t, openPanel(t, newTestModel(t)), "pop")

	m = press(t, m, " ")
	if m.mode != modePanel {
		t.Fatal("ocultar uma seção fechou o painel")
	}
	for _, slug := range visibleSlugs(m) {
		if slug == "pop" {
			t.Fatal("pop continua na barra de abas depois de ocultada")
		}
	}
	if want := len(feed.Sections) - 1; len(m.visible) != want {
		t.Errorf("%d abas visíveis, quero %d", len(m.visible), want)
	}

	m = press(t, m, " ")
	if got := visibleSlugs(m); len(got) != len(feed.Sections) {
		t.Errorf("reexibir deixou %d abas, quero %d", len(got), len(feed.Sections))
	}
}

func TestHiddenSectionsAreSkippedWhenCycling(t *testing.T) {
	// Oculta politica e nacional, as duas seguintes à Home.
	m := modelWithSections(t, []prefs.Section{
		{Slug: "politica", Visible: false},
		{Slug: "nacional", Visible: false},
	})

	for _, key := range []string{"tab", "l"} {
		t.Run(key+" pula as ocultas", func(t *testing.T) {
			if got := activeSlug(press(t, m, key)); got != "internacional" {
				t.Errorf("%q levou a %q, quero internacional", key, got)
			}
		})
	}

	// Nas pontas a volta também respeita as ocultas.
	last := feed.Sections[len(feed.Sections)-1].Slug
	if got := activeSlug(press(t, m, "shift+tab")); got != last {
		t.Errorf("shift+tab da primeira levou a %q, quero %q", got, last)
	}

	end := modelWithSections(t, []prefs.Section{
		{Slug: "home", Visible: false},
		{Slug: "eleicoes", Visible: false},
	})
	end = press(t, end, repeat("h", 1)...) // da primeira visível para trás
	if got := activeSlug(end); got != "saude" {
		t.Errorf("h da primeira visível levou a %q, quero saude — eleicoes está oculta", got)
	}
	if got := activeSlug(press(t, end, "l")); got != "politica" {
		t.Errorf("l da última visível levou a %q, quero politica — home está oculta", got)
	}
}

func TestDigitsNumberTheVisibleList(t *testing.T) {
	m := modelWithSections(t, []prefs.Section{{Slug: "politica", Visible: false}})

	if got := activeSlug(press(t, m, "1")); got != "home" {
		t.Errorf("1 levou a %q, quero home", got)
	}
	if got := activeSlug(press(t, m, "2")); got != "nacional" {
		t.Errorf("2 levou a %q, quero nacional — 2 é sempre a segunda aba da barra", got)
	}
	if got := activeSlug(press(t, m, "0")); got != "home" {
		t.Errorf("0 com nove abas visíveis levou a %q, quero não fazer nada", got)
	}
}

func TestHidingTheActiveSectionMovesToTheNearest(t *testing.T) {
	m := press(t, newTestModel(t), "3") // nacional
	m = cursorOnSection(t, openPanel(t, m), "nacional")

	m = press(t, m, " ")
	if m.mode != modePanel {
		t.Error("ocultar a seção ativa fechou o painel")
	}
	if got := activeSlug(m); got != "internacional" {
		t.Errorf("a aba ativa virou %q, quero a visível mais próxima internacional", got)
	}

	// Na última, a mais próxima é a anterior.
	last := feed.Sections[len(feed.Sections)-1]
	e := press(t, newTestModel(t), "0")
	e = press(t, cursorOnSection(t, openPanel(t, e), last.Slug), " ")
	if got := activeSlug(e); got != feed.Sections[len(feed.Sections)-2].Slug {
		t.Errorf("ocultar a última deixou %q ativa, quero a anterior", got)
	}
}

func TestLastVisibleSectionCannotBeHidden(t *testing.T) {
	only := []prefs.Section{{Slug: "home", Visible: true}}
	for _, s := range feed.Sections[1:] {
		only = append(only, prefs.Section{Slug: s.Slug, Visible: false})
	}

	m := cursorOnSection(t, openPanel(t, modelWithSections(t, only)), "home")

	next, cmd := m.Update(keyMsg(" "))
	m = next.(Model)

	if len(m.visible) != 1 {
		t.Fatalf("%d abas visíveis, quero manter a última", len(m.visible))
	}
	if m.tabs[m.visible[0]].hidden {
		t.Error("a última seção visível foi ocultada")
	}
	if cmd == nil {
		t.Error("a recusa deveria dizer por que nada aconteceu")
	}
}

func TestShowingASectionAgainKeepsWhatWasLoaded(t *testing.T) {
	m := press(t, newTestModel(t), "2") // politica
	idx := m.active

	m.tabs[idx].items = []feed.Item{
		{Title: "uma", Sections: []string{"politica"}},
		{Title: "outra", Sections: []string{"politica"}},
	}
	m.rebuildView(idx)
	m.tabs[idx].loaded = true
	m.tabs[idx].loading = false
	m.tabs[idx].cursor = 1

	m = cursorOnSection(t, openPanel(t, m), "politica")
	m = press(t, m, " ", " ") // oculta e reexibe

	next, cmd := m.Update(keyMsg("esc"))
	m = next.(Model)
	next, cmd = m.Update(keyMsg("2"))
	m = next.(Model)

	if cmd != nil {
		t.Error("reexibir uma seção já carregada disparou busca")
	}
	if got := len(m.tabs[idx].items); got != 2 {
		t.Errorf("sobraram %d matérias, quero as 2 já carregadas", got)
	}
	if m.tabs[idx].cursor != 1 {
		t.Errorf("o cursor virou %d, quero manter 1", m.tabs[idx].cursor)
	}
}

func TestHidingRecordsTheFullSectionList(t *testing.T) {
	m := press(t, cursorOnSection(t, openPanel(t, newTestModel(t)), "pop"), " ")

	chosen := m.Chosen().Sections
	if len(chosen) != len(feed.Sections) {
		t.Fatalf("gravou %d seções, quero a lista completa com %d", len(chosen), len(feed.Sections))
	}
	for i, s := range chosen {
		want := feed.Sections[i].Slug
		if s.Slug != want {
			t.Errorf("seção %d = %q, quero %q", i, s.Slug, want)
		}
		if visible := s.Slug != "pop"; s.Visible != visible {
			t.Errorf("%s visível = %v, quero %v", s.Slug, s.Visible, visible)
		}
	}
	if chosen := m.Chosen(); chosen.Pages != nil || chosen.Justify != nil {
		t.Errorf("mexer nas seções gravou preferência que ninguém pediu: %+v", chosen)
	}
}

func TestSectionsFromTheFileGovernTheTabBar(t *testing.T) {
	m := modelWithSections(t, []prefs.Section{
		{Slug: "economia", Visible: true},
		{Slug: "home", Visible: false},
	})

	if got := visibleSlugs(m)[0]; got != "economia" {
		t.Errorf("a primeira aba é %q, quero economia — o arquivo manda na ordem", got)
	}
	if got := activeSlug(m); got != "economia" {
		t.Errorf("a aba ativa nasceu %q, quero uma visível", got)
	}
	for _, slug := range visibleSlugs(m) {
		if slug == "home" {
			t.Error("home veio oculta do arquivo e apareceu na barra")
		}
	}
}

func TestMoveSectionReordersTheTabBar(t *testing.T) {
	m := cursorOnSection(t, openPanel(t, newTestModel(t)), "politica")

	m = press(t, m, "J")
	if got := visibleSlugs(m); got[1] != "nacional" || got[2] != "politica" {
		t.Fatalf("depois de J a barra é %v, quero politica na terceira posição", got[:3])
	}
	// O cursor acompanha a seção movida.
	if r := m.panelRows()[m.panel.cursor]; r.section == nil || m.tabs[*r.section].section.Slug != "politica" {
		t.Error("o cursor não acompanhou a seção movida")
	}

	m = press(t, m, "K")
	if got := visibleSlugs(m); got[1] != "politica" {
		t.Errorf("K não desfez o movimento: %v", got[:3])
	}

	if m.mode != modePanel {
		t.Error("mover uma seção fechou o painel")
	}
}

func TestMoveSectionStopsAtTheEnds(t *testing.T) {
	first := feed.Sections[0].Slug
	last := feed.Sections[len(feed.Sections)-1].Slug

	up := press(t, cursorOnSection(t, openPanel(t, newTestModel(t)), first), "K")
	if got := visibleSlugs(up)[0]; got != first {
		t.Errorf("K na primeira deu a volta: a primeira aba virou %q", got)
	}

	down := press(t, cursorOnSection(t, openPanel(t, newTestModel(t)), last), "J")
	if got := visibleSlugs(down); got[len(got)-1] != last {
		t.Errorf("J na última deu a volta: a última aba virou %q", got[len(got)-1])
	}
}

func TestMoveSectionChangesTheDigits(t *testing.T) {
	m := press(t, cursorOnSection(t, openPanel(t, newTestModel(t)), "economia"), "K", "K", "K")
	m = press(t, m, "esc")

	if got := activeSlug(press(t, m, "2")); got != "economia" {
		t.Errorf("2 levou a %q, quero economia na segunda posição", got)
	}
}

// A ordem escolhida no painel tem que atravessar o arquivo: é o main que
// empilha as camadas e grava, então o teste faz o mesmo caminho.
func TestChosenOrderSurvivesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	m := press(t, cursorOnSection(t, openPanel(t, newTestModel(t)), "economia"), "K", "K", "K")
	m = press(t, cursorOnSection(t, m, "pop"), " ")

	if err := prefs.Save(path, prefs.Resolve(prefs.Partial{}, m.Chosen())); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fromFile, err := prefs.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	again := New(Config{Store: store.New(filepath.Join(t.TempDir(), "state.json"))},
		prefs.Resolve(fromFile, prefs.Partial{}))

	if got := visibleSlugs(again); got[1] != "economia" {
		t.Errorf("na execução seguinte a segunda aba é %q, quero economia", got[1])
	}
	for _, slug := range visibleSlugs(again) {
		if slug == "pop" {
			t.Error("pop foi ocultada e voltou na execução seguinte")
		}
	}
}

func TestMoveOnANonSectionRowDoesNothing(t *testing.T) {
	m := cursorOn(t, openPanel(t, newTestModel(t)), "Páginas")
	before := m.prefs.Pages

	m = press(t, m, "J", "K")
	if m.prefs.Pages != before {
		t.Errorf("J/K mexeram nas páginas: %d, quero %d", m.prefs.Pages, before)
	}
	if got := visibleSlugs(m)[0]; got != feed.Sections[0].Slug {
		t.Errorf("J/B fora do grupo Seções reordenaram a barra: %q na frente", got)
	}
}

func TestMoveAHiddenSectionKeepsItsPlace(t *testing.T) {
	m := cursorOnSection(t, openPanel(t, newTestModel(t)), "pop")
	m = press(t, m, " ", "K") // oculta e sobe uma posição

	chosen := m.Chosen().Sections
	pos := -1
	for i, s := range chosen {
		if s.Slug == "pop" {
			pos = i
		}
	}
	if want := 5; pos != want {
		t.Fatalf("pop oculta ficou na posição %d, quero %d", pos, want)
	}

	m = press(t, m, " ") // reexibe
	if got := visibleSlugs(m)[5]; got != "pop" {
		t.Errorf("pop reapareceu em outro lugar: a sexta aba é %q", got)
	}
}

// homeWithItems põe na Home uma matéria de cada caminho pedido.
func homeWithItems(t *testing.T, m Model, links ...string) Model {
	t.Helper()
	items := make([]feed.Item, 0, len(links))
	for _, link := range links {
		items = append(items, feed.Item{Title: link, Link: "https://www.cnnbrasil.com.br" + link})
	}
	m.tabs[0].items = items
	m.tabs[0].loaded = true
	m.rebuildView(0)
	return m
}

// homeTitles são os links das matérias que a Home está listando.
func homeTitles(m Model) []string {
	out := make([]string, 0, len(m.tabs[0].view))
	for _, i := range m.tabs[0].view {
		out = append(out, m.tabs[0].items[i].Title)
	}
	return out
}

func TestHomeDoesNotListArticlesFromHiddenSections(t *testing.T) {
	m := homeWithItems(t, newTestModel(t),
		"/politica/pt-aciona-stf/",
		"/esportes/brasileirao/remo-x-vitoria/",
		"/lifestyle/como-dormir-melhor/",
	)
	if got := len(homeTitles(m)); got != 3 {
		t.Fatalf("a Home nasceu com %d matérias, quero as 3", got)
	}

	m = press(t, cursorOnSection(t, openPanel(t, m), "esportes"), " ")

	got := homeTitles(m)
	for _, link := range got {
		if strings.HasPrefix(link, "/esportes/") {
			t.Errorf("a Home continua listando %q com esportes oculta", link)
		}
	}
	if len(got) != 2 {
		t.Fatalf("a Home ficou com %d matérias, quero 2: %v", len(got), got)
	}
	// Lifestyle não é seção do leitor: não há como ocultá-la, e ela fica.
	if got[1] != "/lifestyle/como-dormir-melhor/" {
		t.Errorf("a Home perdeu a matéria de fora das seções: %v", got)
	}

	m = press(t, m, " ") // reexibe esportes
	if got := len(homeTitles(m)); got != 3 {
		t.Errorf("reexibir esportes deixou a Home com %d matérias, quero as 3", got)
	}
}

func TestSectionFiltersByGlobalClassification(t *testing.T) {
	m := modelWithSections(t, []prefs.Section{{Slug: "politica", Visible: false}})
	idx := 1 // politica
	m.tabs[idx].items = []feed.Item{
		{Title: "uma", Sections: []string{"politica"}},
		{Title: "outra", Sections: []string{"politica", "economia"}},
		{Title: "sem classificação"},
	}
	m.rebuildView(idx)

	if got := len(m.tabs[idx].view); got != 2 {
		t.Errorf("politica listou %d matérias, quero as 2 classificadas nela", got)
	}
}

func TestPanelHintsFollowTheRowUnderTheCursor(t *testing.T) {
	next, _ := newTestModel(t).Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m := press(t, next.(Model), "c")

	if view := m.View(); !strings.Contains(view, "alternar") {
		t.Error("as dicas de uma preferência escalar não mencionam h/l")
	}

	view := cursorOnSection(t, m, "pop").View()
	for _, want := range []string{"espaço", "J/K", "reordenar"} {
		if !strings.Contains(view, want) {
			t.Errorf("as dicas de uma seção não mencionam %q", want)
		}
	}
}
