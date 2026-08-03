package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/levyvix/cnnbr/internal/article"
	"github.com/levyvix/cnnbr/internal/feed"
	"github.com/levyvix/cnnbr/internal/prefs"
	"github.com/levyvix/cnnbr/internal/speech"
	"github.com/levyvix/cnnbr/internal/store"
)

// fakePlayer registra o que a UI pediu, sem processo nenhum.
type fakePlayer struct {
	engine  speech.Engine
	engErr  error
	missing string // o que falta para haver voz neural
	outcome speech.Outcome
	speakEr error

	lines  []string
	voice  string
	rate   int
	speaks int
	stops  int

	events chan speech.Event
}

func newFakePlayer() *fakePlayer {
	return &fakePlayer{engine: speech.Piper, events: make(chan speech.Event, 8)}
}

func (f *fakePlayer) Engine() (speech.Engine, error) { return f.engine, f.engErr }
func (f *fakePlayer) NeuralMissing() string          { return f.missing }
func (f *fakePlayer) Stop()                          { f.stops++ }
func (f *fakePlayer) Events() <-chan speech.Event    { return f.events }

// quiet fecha o canal de eventos, para o teste poder executar os comandos que o
// Update devolve. Um deles é sempre a espera do próximo evento, que bloqueia de
// propósito; num canal fechado ela devolve nada e sai.
func (f *fakePlayer) quiet() { close(f.events) }

func (f *fakePlayer) Speak(lines []string, voice string, rate int) (speech.Outcome, error) {
	f.speaks++
	f.lines, f.voice, f.rate = lines, voice, rate
	if f.speakEr != nil {
		return 0, f.speakEr
	}
	return f.outcome, nil
}

// readerWith abre o leitor numa matéria de mentira, com o player injetado.
func readerWith(t *testing.T, p Player, pf prefs.Prefs) Model {
	t.Helper()
	cfg := Config{Store: store.New(filepath.Join(t.TempDir(), "state.json")), Speech: p}
	next, _ := New(cfg, pf).Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m := next.(Model)

	tab := m.cur()
	tab.items = []feed.Item{
		{Title: "A primeira", Link: "https://cnnbrasil.com.br/politica/a-primeira/", HTML: "<p>um</p>"},
		{Title: "A segunda", Link: "https://cnnbrasil.com.br/politica/a-segunda/", HTML: "<p>dois</p>"},
	}
	tab.loaded = true
	m.rebuildView(m.active)

	m = press(t, m, "enter")
	if m.mode != modeReader {
		t.Fatal("enter deveria abrir o leitor")
	}
	return m
}

func reader(t *testing.T, p Player) Model {
	t.Helper()
	return readerWith(t, p, prefs.Defaults())
}

func TestSpeakStartsAndStopsWithA(t *testing.T) {
	p := newFakePlayer()
	m := press(t, reader(t, p), "a")

	if p.speaks != 1 {
		t.Fatalf("`a` pediu %d falas, quero 1", p.speaks)
	}
	if !m.speech.playing {
		t.Error("depois de `a` o modelo deveria estar falando")
	}

	m = press(t, m, "a")
	if p.stops == 0 {
		t.Error("`a` de novo deveria parar a fala")
	}
	if m.speech.playing {
		t.Error("depois do segundo `a` o modelo não deveria estar falando")
	}
}

func TestSpeakSendsTheTitleAndTheReadersPreferences(t *testing.T) {
	p := newFakePlayer()
	pf := prefs.Defaults()
	pf.Voice, pf.SpeechRate = "jeff", 150

	m := press(t, readerWith(t, p, pf), "a")

	want := speech.Lines("A primeira", m.blocks)
	if strings.Join(p.lines, "|") != strings.Join(want, "|") {
		t.Errorf("falou %q, quero %q", p.lines, want)
	}
	if p.voice != "jeff" || p.rate != 150 {
		t.Errorf("voz/velocidade = %q/%d, quero jeff/150", p.voice, p.rate)
	}
}

// Falar está atado à matéria aberta: sair dela ou trocar de matéria cala.
func TestLeavingTheArticleStopsTheSpeech(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"esc volta para a lista", "esc"},
		{"q também", "q"},
		{"J pula para a próxima", "J"},
		{"K volta para a anterior", "K"},
		{"tab troca de seção", "tab"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newFakePlayer()
			m := press(t, reader(t, p), "a")
			if !m.speech.playing {
				t.Fatal("a fala não começou")
			}

			// K, na primeira matéria, não tem para onde ir: começamos da segunda.
			if tc.key == "K" {
				m = press(t, m, "J", "a")
				p.stops = 0
			}

			m = press(t, m, tc.key)
			if p.stops == 0 {
				t.Errorf("%q não parou a fala", tc.key)
			}
			if m.speech.playing {
				t.Errorf("depois de %q o modelo continua falando", tc.key)
			}
		})
	}
}

func TestKAtTheStartOfTheListKeepsSpeaking(t *testing.T) {
	p := newFakePlayer()
	m := press(t, reader(t, p), "a", "K")
	if !m.speech.playing || p.stops != 0 {
		t.Error("K sem para onde ir não deveria calar a matéria que está sendo lida")
	}
}

func TestSpeechIndicatorShowsTheEngineNotACounter(t *testing.T) {
	tests := []struct {
		name   string
		engine speech.Engine
		voice  string
		want   string
	}{
		{"piper se mostra pela voz", speech.Piper, "faber", "♪ faber"},
		{"outra voz do piper", speech.Piper, "cadu", "♪ cadu"},
		{"espeak", speech.ESpeak, "faber", "♪ espeak"},
		{"say", speech.Say, "faber", "♪ say"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newFakePlayer()
			p.engine = tc.engine
			pf := prefs.Defaults()
			pf.Voice = tc.voice

			m := press(t, readerWith(t, p, pf), "a")
			if got := m.speechIndicator(); got != tc.want {
				t.Errorf("indicador = %q, quero %q", got, tc.want)
			}
			if !strings.Contains(m.viewStatus(), tc.want) {
				t.Errorf("a barra de status não desenhou %q", tc.want)
			}
		})
	}
}

// O indicador é estado do Model: statusText se apaga em 3 s, e a fala dura mais.
func TestSpeechIndicatorSurvivesTheStatusClearing(t *testing.T) {
	p := newFakePlayer()
	m := press(t, reader(t, p), "a")

	next, _ := m.Update(clearStatusMsg{})
	m = next.(Model)

	if got := m.speechIndicator(); got != "♪ faber" {
		t.Errorf("indicador depois de limpar a barra = %q, quero ♪ faber", got)
	}
}

func TestSpeechWithoutAnEngineExplainsItself(t *testing.T) {
	p := newFakePlayer()
	p.engErr = errors.New("instale piper (voz neural) ou espeak-ng")

	m := reader(t, p)
	next, cmd := m.Update(keyMsg("a"))
	m = next.(Model)

	msg, ok := cmd().(statusMsg)
	if !ok {
		t.Fatalf("`a` sem motor devolveu %T, quero um aviso na barra", cmd())
	}
	if !msg.isErr {
		t.Error("o aviso deveria ser um erro")
	}
	for _, want := range []string{"piper", "espeak-ng"} {
		if !strings.Contains(msg.text, want) {
			t.Errorf("o aviso %q não nomeia %q", msg.text, want)
		}
	}
	if p.speaks != 0 {
		t.Error("sem motor não se pede fala nenhuma")
	}
	if m.speech.playing {
		t.Error("sem motor o modelo não deveria estar falando")
	}
}

// Uma vez por execução, e só quando a fala não sai pelo piper.
func TestNeuralNoticeShowsOnce(t *testing.T) {
	p := newFakePlayer()
	p.engine, p.missing = speech.ESpeak, "piper"

	m := reader(t, p)
	next, cmd := m.Update(keyMsg("a"))
	m = next.(Model)
	if msg, ok := cmd().(statusMsg); !ok || !strings.Contains(msg.text, "piper") {
		t.Fatalf("o primeiro `a` no espeak deveria avisar da voz neural, veio %v", cmd())
	}

	// `a` para, `a` fala de novo: nada de reler o aviso.
	m = press(t, m, "a")
	next, cmd = m.Update(keyMsg("a"))
	m = next.(Model)
	if cmd != nil {
		if msg, ok := cmd().(statusMsg); ok && strings.Contains(msg.text, "piper") {
			t.Error("o aviso da voz neural apareceu duas vezes na mesma execução")
		}
	}
}

func TestNoNeuralNoticeWhenNothingIsMissing(t *testing.T) {
	p := newFakePlayer()
	p.engine, p.missing = speech.ESpeak, "" // piper instalado, mas sem voz usável

	m := reader(t, p)
	_, cmd := m.Update(keyMsg("a"))
	if cmd != nil {
		if msg, ok := cmd().(statusMsg); ok && strings.Contains(msg.text, "instale") {
			t.Error("com o piper pronto não há voz neural a anunciar")
		}
	}
}

// O aviso nomeia o que falta, e não sempre "o piper": numa máquina com piper mas
// sem aplay, mandar instalar o piper seria mentira.
func TestNeuralNoticeNamesWhatIsMissing(t *testing.T) {
	p := newFakePlayer()
	p.engine, p.missing = speech.ESpeak, "alsa-utils (aplay) ou pulseaudio-utils (paplay)"

	m := reader(t, p)
	_, cmd := m.Update(keyMsg("a"))
	msg, ok := cmd().(statusMsg)
	if !ok || !strings.Contains(msg.text, "alsa-utils") {
		t.Errorf("o aviso deveria nomear o que falta, veio %v", cmd())
	}
}

func TestDownloadShowsProgressAndDoesNotCancel(t *testing.T) {
	p := newFakePlayer()
	p.outcome = speech.Fetching

	m := press(t, reader(t, p), "a")
	if !m.speech.downloading {
		t.Fatal("sem a voz em disco, `a` deveria iniciar o download")
	}
	if m.speech.playing {
		t.Error("o download não é fala")
	}

	next, _ := m.Update(speechEvent{Kind: speech.Progress, Pct: 34})
	m = next.(Model)
	if got := m.speechIndicator(); got != "⇣ 34%" {
		t.Errorf("indicador = %q, quero ⇣ 34%%", got)
	}

	// `a` durante o download informa, e não cancela.
	next, cmd := m.Update(keyMsg("a"))
	m = next.(Model)
	if !m.speech.downloading {
		t.Error("`a` cancelou o download")
	}
	if p.stops != 0 {
		t.Error("`a` durante o download não deveria parar nada")
	}
	if p.speaks != 1 {
		t.Errorf("`a` durante o download pediu %d falas, quero seguir com 1", p.speaks)
	}
	if msg, ok := cmd().(statusMsg); !ok || !strings.Contains(msg.text, "34") {
		t.Errorf("`a` durante o download deveria informar o progresso, veio %v", cmd())
	}
}

func TestDownloadSpeaksOnlyIfStillOnTheSameArticle(t *testing.T) {
	t.Run("mesma matéria: fala", func(t *testing.T) {
		p := newFakePlayer()
		p.outcome = speech.Fetching

		m := press(t, reader(t, p), "a")
		p.outcome = speech.Speaking
		p.quiet()

		next, cmd := m.Update(speechEvent{Kind: speech.Ready})
		m = next.(Model)
		runCmd(cmd)

		if p.speaks != 2 {
			t.Errorf("a voz ficou pronta e a fala não começou: %d pedidos", p.speaks)
		}
		if m.speech.downloading {
			t.Error("o download acabou, o indicador deveria sair")
		}
	})

	// Um par de arquivos ruim não pode virar um download de 63 MB em loop.
	t.Run("voz que chegou e não serve não baixa de novo", func(t *testing.T) {
		p := newFakePlayer()
		p.outcome = speech.Fetching

		m := press(t, reader(t, p), "a")
		p.quiet()

		next, cmd := m.Update(speechEvent{Kind: speech.Ready})
		m = next.(Model)

		if m.speech.downloading {
			t.Error("o download recomeçou sozinho")
		}
		var got statusMsg
		collect(cmd, func(msg tea.Msg) {
			if s, ok := msg.(statusMsg); ok {
				got = s
			}
		})
		if !got.isErr {
			t.Errorf("a voz que não serve deveria virar erro na barra, veio %+v", got)
		}
	})

	t.Run("saiu do leitor: silêncio", func(t *testing.T) {
		p := newFakePlayer()
		p.outcome = speech.Fetching

		m := press(t, reader(t, p), "a", "esc")
		next, _ := m.Update(speechEvent{Kind: speech.Ready})
		m = next.(Model)

		if p.speaks != 1 {
			t.Errorf("fora do leitor a voz pronta não deveria falar: %d pedidos", p.speaks)
		}
		if m.speech.playing {
			t.Error("não há fala fora do leitor")
		}
	})

	// Quem saiu não deve ser surpreendido pela fala ao voltar sem apertar `a`.
	t.Run("saiu e voltou para a mesma matéria: silêncio", func(t *testing.T) {
		p := newFakePlayer()
		p.outcome = speech.Fetching

		m := press(t, reader(t, p), "a", "esc", "enter")
		next, _ := m.Update(speechEvent{Kind: speech.Ready})
		m = next.(Model)

		if p.speaks != 1 {
			t.Errorf("voltar para a matéria não deveria disparar a fala: %d pedidos", p.speaks)
		}
		if m.speech.playing {
			t.Error("a fala precisa de um `a`, não de um enter")
		}
	})

	t.Run("pulou de matéria: silêncio", func(t *testing.T) {
		p := newFakePlayer()
		p.outcome = speech.Fetching

		m := press(t, reader(t, p), "a", "J")
		next, _ := m.Update(speechEvent{Kind: speech.Ready})
		m = next.(Model)

		if p.speaks != 1 {
			t.Errorf("em outra matéria a voz pronta não deveria falar: %d pedidos", p.speaks)
		}
		if m.speech.playing {
			t.Error("a fala é da matéria que a pediu")
		}
	})
}

func TestSpeechEndClearsTheIndicator(t *testing.T) {
	p := newFakePlayer()
	m := press(t, reader(t, p), "a")

	next, _ := m.Update(speechEvent{Kind: speech.Done})
	m = next.(Model)

	if m.speech.playing {
		t.Error("depois do fim da fala o modelo continua falando")
	}
	if got := m.speechIndicator(); got != "" {
		t.Errorf("indicador = %q, quero vazio", got)
	}
}

func TestSpeechFailureReportsAndClears(t *testing.T) {
	p := newFakePlayer()
	p.outcome = speech.Fetching
	m := press(t, reader(t, p), "a")
	p.quiet()

	next, cmd := m.Update(speechEvent{Kind: speech.Failed, Err: errors.New("404 no HuggingFace")})
	m = next.(Model)

	if m.speech.downloading || m.speech.playing {
		t.Error("depois de falhar não há download nem fala")
	}
	var got statusMsg
	collect(cmd, func(msg tea.Msg) {
		if s, ok := msg.(statusMsg); ok {
			got = s
		}
	})
	if !got.isErr || !strings.Contains(got.text, "404") {
		t.Errorf("a falha deveria ir para a barra como erro, veio %+v", got)
	}
}

// A tecla `a` é do leitor: na lista ela não faz nada.
func TestAInTheListDoesNothing(t *testing.T) {
	p := newFakePlayer()
	m := press(t, newTestModel(t), "a")
	if p.speaks != 0 || m.speech.playing {
		t.Error("`a` na lista não deveria falar")
	}
}

func TestSpeechKeyIsInTheHelpOverlay(t *testing.T) {
	next, _ := newTestModel(t).Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m := press(t, next.(Model), "?")
	if !strings.Contains(m.View(), "ouvir a matéria") {
		t.Error("`a` precisa aparecer no overlay de ajuda")
	}
}

// Cada evento rearma a espera, senão o segundo evento nunca chegaria.
func TestSpeechEventRearmsTheWait(t *testing.T) {
	p := newFakePlayer()
	m := press(t, reader(t, p), "a")

	_, cmd := m.Update(speechEvent{Kind: speech.Progress, Pct: 10})
	if cmd == nil {
		t.Fatal("o evento não rearmou a espera")
	}

	p.events <- speech.Event{Kind: speech.Done}
	var saw bool
	collect(cmd, func(msg tea.Msg) {
		if ev, ok := msg.(speechEvent); ok && ev.Kind == speech.Done {
			saw = true
		}
	})
	if !saw {
		t.Error("o comando rearmado não entregou o evento seguinte")
	}
}

// collect roda o comando e entrega cada mensagem, entrando nos Batch.
func collect(cmd tea.Cmd, fn func(tea.Msg)) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			collect(c, fn)
		}
		return
	}
	if msg != nil {
		fn(msg)
	}
}

// prefs não importa speech — é a camada de baixo —, então o padrão da voz está
// escrito nos dois lugares. Este teste é o que impede os dois de divergirem.
func TestDefaultVoiceMatchesTheEngineDefault(t *testing.T) {
	if got := prefs.Defaults().Voice; got != speech.DefaultVoice {
		t.Errorf("prefs.Defaults().Voice = %q, quero speech.DefaultVoice (%q)", got, speech.DefaultVoice)
	}
	if speech.VoiceOr(speech.DefaultVoice).Name != speech.DefaultVoice {
		t.Errorf("speech.DefaultVoice (%q) não está na tabela de vozes", speech.DefaultVoice)
	}
}

// blocksAreParsed é a rede do teste: sem blocos, Lines devolveria só o título e
// os testes acima passariam sem provar nada.
func TestReaderHasBlocksToSpeak(t *testing.T) {
	m := reader(t, newFakePlayer())
	if len(m.blocks) == 0 {
		t.Fatal("a matéria de teste precisa ter blocos")
	}
	if m.blocks[0].Kind != article.Paragraph {
		t.Errorf("o primeiro bloco = %v, quero um parágrafo", m.blocks[0].Kind)
	}
}
