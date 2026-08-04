package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/levyvix/cnnbr/internal/speech"
)

// Player é o que a UI precisa de um motor. É interface para os testes
// poderem substituí-lo; o main injeta um *speech.Player.
type Player interface {
	Engine() (speech.Engine, error)
	NeuralMissing() string
	NeuralInstallable() bool
	InstallNeural(voice string)
	Speak(lines []string, voice string, rate int) (speech.Outcome, error)
	Stop()
	Events() <-chan speech.Event
}

// speechState é o que a barra de status mostra sobre a fala.
//
// É estado do Model, e não um statusMsg: statusText se apaga sozinho em três
// segundos (clearStatusAfter), e o indicador tem de ficar enquanto a fala durar.
//
// Não há contador de blocos aqui, de propósito. Como escrevemos tudo no stdin de
// uma vez e o motor corre à frente do áudio, "blocos enviados" não é
// "blocos ouvidos": um 3/18 que bate 18/18 com 20 s de áudio ainda por sair é
// pior que indicador nenhum, e o texto já está na tela para quem quer se
// localizar.
type speechState struct {
	label       string // o motor em uso: faber, espeak, say
	playing     bool
	downloading bool
	pct         int
	step        string // a etapa em curso, quando ela não tem percentual

	// warned é o aviso de que existe voz neural, mostrado uma vez por execução.
	// Um bool no Model, nada persistido: quem já decidiu ficar no espeak não deve
	// reler o aviso a cada matéria, e "já avisei" não vale um campo no
	// config.json.
	warned bool

	// wantID é a matéria que pediu a fala. O download de 63 MB pode levar 40 s, e
	// em 40 s o leitor pode ter voltado para a lista ou pulado com J.
	wantID string
}

// speechEvent embrulha o que o player avisa, para o Update tratar como mensagem.
type speechEvent speech.Event

// waitSpeech bloqueia no canal do player e devolve o próximo evento como
// mensagem. É rearmado a cada evento tratado, então há sempre um leitor só.
func waitSpeech(p Player) tea.Cmd {
	if p == nil {
		return nil
	}
	events := p.Events()
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return nil
		}
		return speechEvent(ev)
	}
}

// toggleSpeech é a tecla `a`: começa a falar a matéria aberta, ou para.
func (m *Model) toggleSpeech() tea.Cmd {
	if m.cfg.Speech == nil {
		return statusErr("ouvir a matéria não está disponível")
	}

	// Durante o download, `a` só informa. Jogar fora 20 MB já baixados num toque
	// de tecla é caro demais para um gesto tão fácil de fazer sem querer.
	if m.speech.downloading {
		return status("%s", m.progressText())
	}
	if m.speech.playing {
		m.stopSpeech()
		return nil
	}
	return m.startSpeech()
}

// installNeural é a tecla `A`: traz o motor neural e a voz, e fala quando
// terminar. Tem tecla própria porque instalar software e criar um ambiente
// Python na máquina de alguém pede um sim explícito — diferente de baixar um
// arquivo de voz, que a issue definiu como automático.
func (m *Model) installNeural() tea.Cmd {
	if m.cfg.Speech == nil {
		return statusErr("ouvir a matéria não está disponível")
	}
	if m.speech.downloading {
		return status("%s", m.progressText())
	}
	if !m.cfg.Speech.NeuralInstallable() {
		if missing := m.cfg.Speech.NeuralMissing(); missing != "" {
			return statusErr("para voz neural, instale %s", missing)
		}
		return status("a voz neural já está pronta — `a` ouve")
	}

	m.stopSpeech()
	it := m.reading()
	m.cfg.Speech.InstallNeural(m.prefs.Voice)

	m.speech.wantID = it.ID()
	m.speech.label = speech.VoiceOr(m.prefs.Voice).Name
	m.speech.downloading, m.speech.pct = true, 0
	m.speech.step = "instalando o piper"
	// 220 MB medidos: ~160 MB do piper com o onnxruntime, ~61 MB da voz. Dizer o
	// número importa mais que o de costume, porque a tecla dispara os dois.
	return status("instalando a voz neural — uns 220 MB, uma vez só")
}

func (m *Model) startSpeech() tea.Cmd {
	engine, err := m.cfg.Speech.Engine()
	if err != nil {
		return statusErr("%s", err)
	}

	it := m.reading()
	lines := speech.Lines(it.Title, m.blocks)
	outcome, err := m.cfg.Speech.Speak(lines, m.prefs.Voice, m.prefs.SpeechRate)
	if err != nil {
		return statusErr("%s", err)
	}

	m.speech.wantID = it.ID()
	m.speech.label = engine.Label(speech.VoiceOr(m.prefs.Voice).Name)

	if outcome == speech.Fetching {
		m.speech.downloading, m.speech.pct = true, 0
		return status("baixando a voz %s", speech.VoiceOr(m.prefs.Voice).Name)
	}

	m.speech.playing = true
	if engine != speech.Piper && !m.speech.warned {
		m.speech.warned = true
		// Quando o cnnbr consegue resolver sozinho, o aviso oferece a tecla em vez
		// de mandar a pessoa ao gerenciador de pacotes.
		if m.cfg.Speech.NeuralInstallable() {
			return status("há voz neural melhor — `A` instala")
		}
		if missing := m.cfg.Speech.NeuralMissing(); missing != "" {
			return status("há voz neural melhor: instale %s", missing)
		}
	}
	return nil
}

// progressText descreve o que está sendo trazido. O download da voz tem
// percentual; a instalação do motor, não — pip e uv não dão um número que preste.
func (m Model) progressText() string {
	if m.speech.pct > 0 {
		return fmt.Sprintf("%s — %d%%", m.speech.step, m.speech.pct)
	}
	if m.speech.step != "" {
		return m.speech.step + "…"
	}
	return "baixando a voz"
}

// stopSpeech cala a fala. Falar está atado à matéria aberta: sair dela, pular
// para outra ou trocar de seção passam por aqui.
//
// O download em curso não para: ele não fala nada por conta própria, e quem já
// baixou 60 MB não deve recomeçar porque o leitor apertou esc. Mas o pedido morre
// aqui de qualquer jeito — quem saiu não deve ser surpreendido pela fala quando
// os 63 MB chegarem, nem ao voltar para a mesma matéria sem apertar `a`.
func (m *Model) stopSpeech() {
	m.speech.wantID = ""
	if m.cfg.Speech == nil || !m.speech.playing {
		return
	}
	m.cfg.Speech.Stop()
	m.speech.playing = false
}

// handleSpeechEvent trata o que o player avisou e rearma a espera.
func (m Model) handleSpeechEvent(ev speech.Event) (tea.Model, tea.Cmd) {
	again := waitSpeech(m.cfg.Speech)

	switch ev.Kind {
	case speech.Progress:
		m.speech.pct = ev.Pct
		return m, again

	case speech.Step:
		m.speech.step, m.speech.pct = ev.Text, 0
		return m, again

	case speech.Ready:
		m.speech.downloading, m.speech.step = false, ""
		// A voz ficou pronta, mas só falamos se o leitor ainda estiver na mesma
		// matéria. Saiu, a voz fica pronta para o próximo `a` — coerente com
		// "falar está atado à matéria aberta".
		if m.mode != modeReader || m.speech.wantID == "" || m.reading().ID() != m.speech.wantID {
			m.speech.wantID = ""
			return m, again
		}

		cmd := m.startSpeech()
		if m.speech.downloading {
			// A voz acabou de chegar e o player ainda a considera ausente. Não
			// tentamos de novo: um par de arquivos ruim viraria um download de
			// 63 MB em loop.
			m.speech.downloading = false
			return m, tea.Batch(again, statusErr("a voz baixada não serviu — apague %s e tente de novo",
				speech.VoicesDir(speech.Base())))
		}
		return m, tea.Batch(again, cmd)

	case speech.Done:
		m.speech.playing = false
		m.speech.wantID = ""
		return m, again

	case speech.Failed:
		m.speech.playing, m.speech.downloading = false, false
		m.speech.step, m.speech.wantID = "", ""
		return m, tea.Batch(again, statusErr("%s", ev.Err))
	}
	return m, again
}

// speechIndicator é o que aparece à direita da barra de status, ao lado do
// percentual de rolagem: o motor em uso, e nada mais.
func (m Model) speechIndicator() string {
	switch {
	case m.speech.downloading && m.speech.pct > 0:
		return fmt.Sprintf("⇣ %d%%", m.speech.pct)
	case m.speech.downloading:
		// A instalação do motor não tem percentual: pip e uv não dão um número que
		// preste, e inventar um seria pior que nomear a etapa.
		return "⇣ " + m.speech.step
	case m.speech.playing:
		return "♪ " + m.speech.label
	}
	return ""
}
