package ui

import (
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/levyvix/cnnbr/internal/feed"
	"github.com/levyvix/cnnbr/internal/prefs"
	"github.com/levyvix/cnnbr/internal/store"
)

// openPanel devolve um modelo com o painel aberto e um tamanho de tela usável.
func openPanel(t *testing.T, m Model) Model {
	t.Helper()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = press(t, next.(Model), "c")
	if m.mode != modePanel {
		t.Fatal("c deveria abrir o painel a partir da lista")
	}
	return m
}

// cursorOn põe o cursor do painel na linha da preferência pedida. A navegação com
// j/k tem teste próprio; aqui interessa a escolha, não como se chega nela.
func cursorOn(t *testing.T, m Model, label string) Model {
	t.Helper()
	for i, r := range m.panelRows() {
		if r.pref != nil && r.pref.label == label {
			m.panel.cursor = i
			return m
		}
	}
	t.Fatalf("o painel não tem a linha %q", label)
	return m
}

func TestPanelOpensOnlyFromTheList(t *testing.T) {
	m := openPanel(t, newTestModel(t))
	if m.mode != modePanel {
		t.Fatalf("modo = %v, quero o painel", m.mode)
	}

	// Do leitor não abre: ocultar a seção da matéria aberta deixaria o leitor
	// apontando para uma aba que não existe mais.
	r := newTestModel(t)
	r.mode = modeReader
	r = press(t, r, "c")
	if r.mode != modeReader {
		t.Errorf("c no leitor mudou o modo para %v, quero continuar no leitor", r.mode)
	}
}

func TestPanelCursorSkipsGroupTitles(t *testing.T) {
	m := openPanel(t, newTestModel(t))

	rows := m.panelRows()
	if !rows[m.panel.cursor].selectable() {
		t.Fatal("o painel abre com o cursor num subtítulo")
	}

	// Uma descida completa nunca pousa num subtítulo, e para na última linha.
	for i := 0; i < len(rows)+2; i++ {
		m = press(t, m, "j")
		if !rows[m.panel.cursor].selectable() {
			t.Fatalf("j parou no subtítulo %q", rows[m.panel.cursor].title)
		}
	}
	if want := len(rows) - 1; m.panel.cursor != want {
		t.Errorf("j sem fim parou em %d, quero %d", m.panel.cursor, want)
	}

	for i := 0; i < len(rows)+2; i++ {
		m = press(t, m, "k")
		if !rows[m.panel.cursor].selectable() {
			t.Fatalf("k parou no subtítulo %q", rows[m.panel.cursor].title)
		}
	}
	if want := firstPrefRow(); m.panel.cursor != want {
		t.Errorf("k sem fim parou em %d, quero %d", m.panel.cursor, want)
	}
}

func TestPanelCyclesClosedValues(t *testing.T) {
	tests := []struct {
		name  string
		label string
		keys  []string
		want  prefs.Prefs
	}{
		{"justificar desliga", "Justificar", []string{"l"}, mut(func(p *prefs.Prefs) { p.Justify = false })},
		{"justificar volta", "Justificar", []string{"l", "l"}, mut(func(p *prefs.Prefs) {})},
		{"h também alterna", "Justificar", []string{"h"}, mut(func(p *prefs.Prefs) { p.Justify = false })},
		{"espaço também alterna", "Justificar", []string{" "}, mut(func(p *prefs.Prefs) { p.Justify = false })},

		{"páginas avança", "Páginas", []string{"l"}, mut(func(p *prefs.Prefs) { p.Pages = 3 })},
		{"páginas volta", "Páginas", []string{"h"}, mut(func(p *prefs.Prefs) { p.Pages = 1 })},
		{"páginas dá a volta pelo fim", "Páginas", repeat("l", 4), mut(func(p *prefs.Prefs) { p.Pages = 1 })},
		{"páginas dá a volta pelo início", "Páginas", []string{"h", "h"}, mut(func(p *prefs.Prefs) { p.Pages = 5 })},

		{"ttl avança", "TTL do cache", []string{"l"}, mut(func(p *prefs.Prefs) { p.TTL = 30 * time.Minute })},
		{"ttl volta", "TTL do cache", []string{"h"}, mut(func(p *prefs.Prefs) { p.TTL = 5 * time.Minute })},
		{"ttl dá a volta pelo início", "TTL do cache", []string{"h", "h"}, mut(func(p *prefs.Prefs) { p.TTL = 6 * time.Hour })},

		{"retenção avança", "Retenção do histórico", []string{"l"}, mut(func(p *prefs.Prefs) { p.RetentionDays = 180 })},
		{"retenção chega em nunca", "Retenção do histórico", []string{"l", "l"}, mut(func(p *prefs.Prefs) { p.RetentionDays = 0 })},
		{"retenção volta", "Retenção do histórico", []string{"h"}, mut(func(p *prefs.Prefs) { p.RetentionDays = 30 })},

		{"voz avança", "Voz", []string{"l"}, mut(func(p *prefs.Prefs) { p.Voice = "jeff" })},
		{"voz volta", "Voz", []string{"h"}, mut(func(p *prefs.Prefs) { p.Voice = "cadu" })},
		{"voz dá a volta pelo início", "Voz", []string{"h", "h"}, mut(func(p *prefs.Prefs) { p.Voice = "edresson" })},
		{"voz dá a volta pelo fim", "Voz", repeat("l", 4), mut(func(p *prefs.Prefs) {})},

		{"velocidade avança", "Velocidade", []string{"l"}, mut(func(p *prefs.Prefs) { p.SpeechRate = 125 })},
		{"velocidade dá a volta pelo início", "Velocidade", []string{"h"}, mut(func(p *prefs.Prefs) { p.SpeechRate = 250 })},
		{"velocidade dá a volta pelo fim", "Velocidade", repeat("l", 7), mut(func(p *prefs.Prefs) {})},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := cursorOn(t, openPanel(t, newTestModel(t)), tc.label)
			m = press(t, m, tc.keys...)
			if !reflect.DeepEqual(scalars(m.prefs), tc.want) {
				t.Errorf("preferências = %+v, quero %+v", m.prefs, tc.want)
			}
		})
	}
}

// scalars são as preferências sem a lista de seções, que tem testes próprios e
// não cabe numa comparação de struct.
func scalars(p prefs.Prefs) prefs.Prefs {
	p.Sections = nil
	return p
}

// mut devolve os padrões com uma alteração aplicada.
func mut(f func(*prefs.Prefs)) prefs.Prefs {
	p := prefs.Defaults()
	f(&p)
	return p
}

func TestPanelJumpsToTheNearestPreset(t *testing.T) {
	tests := []struct {
		name  string
		label string
		start func(*prefs.Prefs)
		key   string
		want  func(prefs.Prefs) any
		val   any
	}{
		{
			"páginas fora da lista", "Páginas",
			func(p *prefs.Prefs) { p.Pages = 7 }, "l",
			func(p prefs.Prefs) any { return p.Pages }, 5,
		},
		{
			"ttl fora da lista", "TTL do cache",
			func(p *prefs.Prefs) { p.TTL = 7 * time.Minute }, "l",
			func(p prefs.Prefs) any { return p.TTL }, 5 * time.Minute,
		},
		{
			"retenção fora da lista", "Retenção do histórico",
			func(p *prefs.Prefs) { p.RetentionDays = 20 }, "h",
			func(p prefs.Prefs) any { return p.RetentionDays }, 30,
		},
		{
			// "nunca" mora no fim da reta da retenção, não no começo: um
			// histórico curto salta para 7 dias, não para nunca podar.
			"retenção curta não vira nunca", "Retenção do histórico",
			func(p *prefs.Prefs) { p.RetentionDays = 3 }, "l",
			func(p prefs.Prefs) any { return p.RetentionDays }, 7,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := prefs.Defaults()
			tc.start(&p)
			m := New(Config{Store: store.New(filepath.Join(t.TempDir(), "state.json"))}, p)
			m = cursorOn(t, openPanel(t, m), tc.label)
			m = press(t, m, tc.key)
			if got := tc.want(m.prefs); got != tc.val {
				t.Errorf("primeiro %q levou a %v, quero o predefinido mais próximo %v", tc.key, got, tc.val)
			}
		})
	}
}

func TestPanelShowsUnknownValueAsIs(t *testing.T) {
	tests := []struct {
		label string
		p     prefs.Prefs
		want  string
	}{
		{"Páginas", mut(func(p *prefs.Prefs) { p.Pages = 7 }), "7"},
		{"TTL do cache", mut(func(p *prefs.Prefs) { p.TTL = 7 * time.Minute }), "7m"},
		{"TTL do cache", mut(func(p *prefs.Prefs) { p.TTL = 90 * time.Second }), "1m30s"},
		{"Retenção do histórico", mut(func(p *prefs.Prefs) { p.RetentionDays = 45 }), "45 dias"},
		{"Retenção do histórico", prefs.Defaults(), "60 dias"},
		{"Retenção do histórico", mut(func(p *prefs.Prefs) { p.RetentionDays = 0 }), "nunca"},
		{"Justificar", prefs.Defaults(), "sim"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			pref := findPref(t, tc.label)
			if got := pref.display(tc.p); got != tc.want {
				t.Errorf("%s = %q, quero %q", tc.label, got, tc.want)
			}
		})
	}
}

func findPref(t *testing.T, label string) preference {
	t.Helper()
	for _, r := range prefRows {
		if r.pref != nil && r.pref.label == label {
			return *r.pref
		}
	}
	t.Fatalf("o painel não tem a linha %q", label)
	return preference{}
}

func TestPanelShowsRSSHealth(t *testing.T) {
	m := openPanel(t, newTestModel(t))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	m = next.(Model)
	m.sourceHealth = []feed.SourceHealth{
		{
			SourceID:    "ok",
			SourceName:  "Fonte OK",
			Status:      feed.SourceOK,
			LastSuccess: time.Now().Add(-time.Hour),
		},
		{
			SourceID:    "falha",
			SourceName:  "Fonte Falha",
			Status:      feed.SourceFailed,
			LastSuccess: time.Now().Add(-2 * time.Hour),
			LastErrorAt: time.Now().Add(-time.Minute),
			LastError:   "feed respondeu 502 Bad Gateway",
		},
		{
			SourceID:   "nunca",
			SourceName: "Fonte Nunca",
			Status:     feed.SourceNeverLoaded,
		},
	}

	view := m.View()
	for _, want := range []string{
		"Fontes RSS",
		"Fonte OK", "OK", "último sucesso",
		"Fonte Falha", "falhou", "último erro", "feed respondeu 502 Bad Gateway",
		"Fonte Nunca", "nunca carregou",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("painel não mostrou %q:\n%s", want, view)
		}
	}
}

func TestPanelHealthRowsAreReadOnly(t *testing.T) {
	m := openPanel(t, newTestModel(t))
	for i, r := range m.panelRows() {
		if r.sourceHealth != nil {
			m.panel.cursor = i
			break
		}
	}

	before := m.Chosen()
	m = press(t, m, " ", "l", "h", "J", "K")
	if !reflect.DeepEqual(m.Chosen(), before) {
		t.Fatalf("linha de saúde alterou preferências: %+v", m.Chosen())
	}
}

func TestOpeningPanelDoesNotFetch(t *testing.T) {
	transport := &countingRoundTripper{}
	m := New(Config{
		Client: &http.Client{Transport: transport},
		Cache:  feed.Cache{Dir: t.TempDir(), TTL: time.Hour},
		Store:  store.New(filepath.Join(t.TempDir(), "state.json")),
	}, prefs.Defaults())

	next, cmd := m.Update(keyMsg("c"))
	m = next.(Model)
	if m.mode != modePanel {
		t.Fatalf("c abriu modo %v, quero painel", m.mode)
	}
	if cmd != nil {
		t.Fatal("abrir o painel não deveria devolver comando")
	}
	if transport.calls != 0 {
		t.Fatalf("abrir o painel fez %d requisições", transport.calls)
	}
}

type countingRoundTripper struct {
	calls int
}

func (rt *countingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	rt.calls++
	return nil, http.ErrHandlerTimeout
}

func TestPanelAppliesJustifyAndTTLImmediately(t *testing.T) {
	m := cursorOn(t, openPanel(t, newTestModel(t)), "TTL do cache")
	m = press(t, m, "l")
	if want := 30 * time.Minute; m.cfg.Cache.TTL != want {
		t.Errorf("TTL do cache em uso = %v, quero %v — o efeito é imediato e sem rede", m.cfg.Cache.TTL, want)
	}

	m = cursorOn(t, m, "Justificar")
	m = press(t, m, "l")
	if m.prefs.Justify {
		t.Error("a justificação deveria valer já na sessão")
	}
}

func TestPanelRecordsOnlyWhatWasChanged(t *testing.T) {
	m := cursorOn(t, openPanel(t, newTestModel(t)), "Páginas")
	m = press(t, m, "l")

	chosen := m.Chosen()
	if chosen.Pages == nil || *chosen.Pages != 3 {
		t.Fatalf("páginas escolhidas = %v, quero 3", chosen.Pages)
	}
	if chosen.Justify != nil || chosen.TTL != nil || chosen.RetentionDays != nil {
		t.Errorf("o painel gravou preferência que ninguém mexeu: %+v", chosen)
	}
}

func TestPanelCloseSavesOnce(t *testing.T) {
	for _, key := range []string{"esc", "q", "c"} {
		t.Run(key, func(t *testing.T) {
			saves := 0
			cfg := Config{
				Store:     store.New(filepath.Join(t.TempDir(), "state.json")),
				SavePrefs: func(prefs.Partial) error { saves++; return nil },
			}
			m := openPanel(t, New(cfg, prefs.Defaults()))
			m = press(t, cursorOn(t, m, "Páginas"), "l")

			next, cmd := m.Update(keyMsg(key))
			m = next.(Model)
			runCmd(cmd)

			if m.mode != modeList {
				t.Errorf("%q não fechou o painel: modo = %v", key, m.mode)
			}
			if saves != 1 {
				t.Errorf("%q gravou %d vezes, quero 1", key, saves)
			}
		})
	}
}

func TestPanelCloseWithoutChangesDoesNotWriteTheFile(t *testing.T) {
	saves := 0
	cfg := Config{
		Store:     store.New(filepath.Join(t.TempDir(), "state.json")),
		SavePrefs: func(prefs.Partial) error { saves++; return nil },
	}
	m := openPanel(t, New(cfg, prefs.Defaults()))

	next, cmd := m.Update(keyMsg("esc"))
	m = next.(Model)
	runCmd(cmd)

	if saves != 0 {
		t.Errorf("fechar sem mexer em nada gravou %d vezes, quero 0", saves)
	}
}

// runCmd executa o comando devolvido pelo Update, incluindo os de um Batch.
func runCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			runCmd(c)
		}
	}
}

func TestHelpOverPanelReturnsToPanel(t *testing.T) {
	m := press(t, openPanel(t, newTestModel(t)), "?")
	if !m.showHelp {
		t.Fatal("? deveria abrir a ajuda por cima do painel")
	}
	m = press(t, m, "j")
	if m.showHelp {
		t.Error("qualquer tecla deveria fechar a ajuda")
	}
	if m.mode != modePanel {
		t.Errorf("fechar a ajuda saiu do painel: modo = %v", m.mode)
	}
}

func TestPanelOwnsItsKeymap(t *testing.T) {
	m := openPanel(t, newTestModel(t))
	before := m.active

	m = press(t, m, "3", "tab")
	if m.mode != modePanel {
		t.Error("um dígito ou tab fechou o painel")
	}
	if m.active != before {
		t.Errorf("a aba mudou para %d com o painel aberto, quero %d", m.active, before)
	}
}

func TestPanelKeepsHeaderTabsAndStatus(t *testing.T) {
	m := openPanel(t, newTestModel(t))
	view := m.View()

	for _, want := range []string{"CNN Brasil", "Home", "Leitura", "Feed", "Justificar", "fechar"} {
		if !strings.Contains(view, want) {
			t.Errorf("o painel não desenhou %q", want)
		}
	}
	if lines := strings.Count(view, "\n") + 1; lines != 24 {
		t.Errorf("o painel desenhou %d linhas, quero as 24 da tela", lines)
	}
}

func TestPanelScrollsWhenItDoesNotFit(t *testing.T) {
	next, _ := newTestModel(t).Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	m := press(t, next.(Model), "c")

	m = press(t, m, repeat("j", len(m.panelRows()))...)
	if m.panel.top == 0 {
		t.Error("com a tela curta o painel deveria rolar")
	}
	if got := strings.Count(m.viewPanel(), "\n") + 1; got != m.bodyHeight() {
		t.Errorf("o painel desenhou %d linhas, quero %d", got, m.bodyHeight())
	}
	if label := m.rowLabel(m.panelRows()[m.panel.cursor]); !strings.Contains(m.viewPanel(), label) {
		t.Error("a linha sob o cursor saiu da tela")
	}
}

func TestPanelNotesWhenPagesTakeEffect(t *testing.T) {
	if findPref(t, "Páginas").note == "" {
		t.Error("a linha de páginas precisa dizer que o valor só vale na próxima busca")
	}
	if findPref(t, "Retenção do histórico").note == "" {
		t.Error("a linha de retenção precisa dizer que o valor vale na próxima execução")
	}
	// A voz baixa 63 MB e a velocidade já está no pipe: nenhuma das duas pode
	// valer no momento da tecla, e a linha tem de dizer isso.
	if findPref(t, "Voz").note == "" {
		t.Error("a linha de voz precisa dizer que a voz baixa na primeira vez que ouvir")
	}
	if findPref(t, "Velocidade").note == "" {
		t.Error("a linha de velocidade precisa dizer que o valor vale na próxima fala")
	}
}

// Escolher uma voz grava a preferência e não baixa nada: é isso que preserva a
// invariante que o painel documenta sobre si mesmo, e evita que percorrer as
// quatro vozes com h/l dispare quatro downloads.
func TestPanelVoiceRecordsWithoutDownloading(t *testing.T) {
	p := newFakePlayer()
	cfg := Config{Store: store.New(filepath.Join(t.TempDir(), "state.json")), Speech: p}
	m := cursorOn(t, openPanel(t, New(cfg, prefs.Defaults())), "Voz")

	// faber, jeff, edresson: três teclas, um só valor gravado.
	m = press(t, m, "l", "l")

	chosen := m.Chosen()
	if chosen.Voice == nil {
		t.Fatal("o painel não gravou a voz")
	}
	if *chosen.Voice != "edresson" {
		t.Fatalf("voz escolhida = %q, quero edresson", *chosen.Voice)
	}
	if p.speaks != 0 {
		t.Errorf("percorrer as vozes pediu %d falas", p.speaks)
	}
	if chosen.SpeechRate != nil {
		t.Errorf("o painel gravou a velocidade sem ninguém mexer nela: %v", chosen.SpeechRate)
	}
}

func TestPanelSpeechRateRecordsWhatWasChosen(t *testing.T) {
	m := cursorOn(t, openPanel(t, newTestModel(t)), "Velocidade")
	m = press(t, m, "l", "l")

	chosen := m.Chosen()
	if chosen.SpeechRate == nil || *chosen.SpeechRate != 150 {
		t.Fatalf("velocidade escolhida = %v, quero 150", chosen.SpeechRate)
	}
	if chosen.Voice != nil {
		t.Errorf("o painel gravou a voz sem ninguém mexer nela: %v", chosen.Voice)
	}
}

func TestPanelShowsTheAudioGroup(t *testing.T) {
	view := openPanel(t, newTestModel(t)).View()
	for _, want := range []string{"Áudio", "Voz", "faber", "Velocidade", "1×"} {
		if !strings.Contains(view, want) {
			t.Errorf("o painel não desenhou %q", want)
		}
	}
}

// Uma velocidade fora dos predefinidos aparece como é, com a vírgula decimal dos
// rótulos.
func TestPanelShowsUnknownSpeechRateWithAComma(t *testing.T) {
	tests := map[int]string{140: "1,4×", 175: "1,75×", 190: "1,9×", 100: "1×"}
	for rate, want := range tests {
		got := findPref(t, "Velocidade").display(mut(func(p *prefs.Prefs) { p.SpeechRate = rate }))
		if got != want {
			t.Errorf("velocidade %d = %q, quero %q", rate, got, want)
		}
	}
}

// Um config.json com uma voz que não existe não pode calar o painel.
func TestPanelUnknownVoiceFallsBackToTheDefault(t *testing.T) {
	got := findPref(t, "Voz").display(mut(func(p *prefs.Prefs) { p.Voice = "luciana" }))
	if got != "faber" {
		t.Errorf("voz desconhecida = %q, quero o padrão faber", got)
	}
}
