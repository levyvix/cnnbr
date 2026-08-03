package speech

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// Kind é o que aconteceu com a fala.
type Kind int

const (
	Progress Kind = iota // percentual do download da voz
	Ready                // voz baixada; quem pediu decide se ainda quer falar
	Done                 // a fala terminou sozinha
	Failed
)

// Event chega à UI por um canal, lido por um tea.Cmd que bloqueia nele.
// Deliberadamente não é tea.ExecProcess: aquilo suspende a TUI inteira.
type Event struct {
	Kind Kind
	Pct  int
	Err  error
}

// Outcome é o que Speak conseguiu fazer.
type Outcome int

const (
	Speaking Outcome = iota // a fala começou
	Fetching                // a voz está sendo baixada; ainda não há fala
)

// Player fala a matéria aberta.
//
// É sempre usado por ponteiro, e tem que ser: o bubbletea copia o Model a cada
// Update, e um player por valor perderia os handles dos processos — Stop mataria
// uma cópia e o espeak continuaria falando.
type Player struct {
	dir    string
	client *http.Client
	goos   string

	mu    sync.Mutex
	procs []*exec.Cmd
	// gen distingue as sessões de fala: o que sobrou de uma sessão parada não
	// avisa a UI que "terminou".
	gen     int
	loading bool   // download em curso
	help    string // saída do `piper --help`, lida uma vez por execução
	gotHelp bool

	events chan Event
}

// New cria o player. O diretório é onde as vozes ficam, e o cliente é só para
// baixá-las — o main injeta um separado do dos feeds, porque o Timeout do
// http.Client cobre a leitura do corpo inteiro e 63 MB não caberiam nos 30 s.
func New(dir string, client *http.Client) *Player {
	return &Player{
		dir:    dir,
		client: client,
		goos:   runtime.GOOS,
		// Com folga para o download não travar entre dois Updates da UI.
		events: make(chan Event, 32),
	}
}

// Events é por onde o progresso e o fim da fala chegam à UI.
func (p *Player) Events() <-chan Event { return p.events }

// Engine é o motor desta execução, ou o erro que explica por que não há nenhum.
func (p *Player) Engine() (Engine, error) { return Detect(p.goos) }

// NeuralMissing nomeia o que falta para haver voz neural, ou "" quando nada
// falta.
func (p *Player) NeuralMissing() string { return NeuralMissing() }

// Speak fala as linhas, parando antes o que estivesse falando. Sem a voz neural
// em disco, dispara o download e devolve Fetching sem falar nada: quem chamou
// decide, no Ready, se ainda quer ouvir esta matéria.
func (p *Player) Speak(lines []string, voice string, rate int) (Outcome, error) {
	if len(lines) == 0 {
		return 0, errors.New("esta matéria não tem texto para falar")
	}
	p.Stop()

	engine, err := p.Engine()
	if err != nil {
		return 0, err
	}

	setup := Setup{Engine: engine}
	switch engine {
	case Piper:
		if setup.Bin, err = exec.LookPath(piperBin); err != nil {
			return 0, err
		}
		if setup.RawPlayer, err = findRawPlayer(); err != nil {
			return 0, err
		}
		setup.ScaleFlag = scaleFlag(p.piperHelp(setup.Bin))

		setup.Model, err = Find(p.dir, VoiceOr(voice))
		if errors.Is(err, ErrMissing) {
			p.download(VoiceOr(voice))
			return Fetching, nil
		}
		if err != nil {
			return 0, err
		}

	case ESpeak:
		if setup.Bin, err = exec.LookPath(espeakBin); err != nil {
			return 0, err
		}
	case Say:
		if setup.Bin, err = exec.LookPath(sayBin); err != nil {
			return 0, err
		}
	}

	synth, player := Commands(setup, rate)
	if err := p.start(lines, synth, player); err != nil {
		return 0, err
	}
	return Speaking, nil
}

// start sobe um motor por sessão de fala e escreve todos os blocos no stdin de
// uma vez. O buffer do pipe absorve, e o motor processa a linha seguinte
// enquanto a anterior toca — é daí que vem a latência baixa.
//
// Não troque isto por "um processo por bloco", por dois motivos:
//
//  1. O piper recarregaria a voz de 63 MB a cada bloco.
//  2. Reiniciar o piper no meio de um aplay compartilhado desalinha o stream de
//     PCM cru em um byte, e todo o resto do áudio vira ruído branco. Não há
//     framing para o player ressincronizar.
func (p *Player) start(lines, synth, player []string) error {
	synthCmd := exec.Command(synth[0], synth[1:]...)
	group(synthCmd)

	stdin, err := synthCmd.StdinPipe()
	if err != nil {
		return err
	}

	// last é quem, ao sair, marca o fim natural da fala. Com piper é o player,
	// que só termina quando o buffer de áudio drena; sem player, o próprio
	// motor.
	cmds := []*exec.Cmd{synthCmd}
	last := synthCmd

	if len(player) > 0 {
		// os.Pipe em vez de StdoutPipe: com as duas pontas sendo *os.File, o
		// exec passa os descritores direto aos processos, sem goroutine de cópia
		// no meio que possa correr com o Wait.
		r, w, err := os.Pipe()
		if err != nil {
			stdin.Close()
			return err
		}
		playerCmd := exec.Command(player[0], player[1:]...)
		group(playerCmd)
		synthCmd.Stdout = w
		playerCmd.Stdin = r

		if err := synthCmd.Start(); err != nil {
			stdin.Close()
			r.Close()
			w.Close()
			return err
		}
		if err := playerCmd.Start(); err != nil {
			stdin.Close()
			r.Close()
			w.Close()
			kill(synthCmd)
			_ = synthCmd.Wait()
			return err
		}
		// As pontas agora são dos filhos: sem fechar as nossas, o player nunca
		// veria o fim do stream e ficaria esperando para sempre.
		r.Close()
		w.Close()

		cmds = append(cmds, playerCmd)
		last = playerCmd
	} else if err := synthCmd.Start(); err != nil {
		stdin.Close()
		return err
	}

	p.mu.Lock()
	p.gen++
	gen := p.gen
	p.procs = cmds
	p.mu.Unlock()

	// Numa goroutine porque o pipe enche: o motor só drena enquanto fala.
	go func() {
		_, _ = stdin.Write([]byte(strings.Join(lines, "\n") + "\n"))
		_ = stdin.Close()
	}()

	go func() {
		for _, c := range cmds {
			if c != last {
				_ = c.Wait()
			}
		}
		_ = last.Wait()
		p.finish(gen)
	}()
	return nil
}

// finish avisa que a fala acabou, a menos que esta sessão já tenha sido parada —
// nesse caso quem parou já sabe, e um "terminou" atrasado apagaria o indicador
// de uma fala nova.
func (p *Player) finish(gen int) {
	p.mu.Lock()
	stale := gen != p.gen
	if !stale {
		p.procs = nil
	}
	p.mu.Unlock()
	if !stale {
		p.emit(Event{Kind: Done})
	}
}

// Stop cala a fala. Falar está atado à matéria aberta, então sair dela, pular
// para outra ou fechar o programa passam por aqui.
func (p *Player) Stop() {
	p.mu.Lock()
	p.gen++
	procs := p.procs
	p.procs = nil
	p.mu.Unlock()

	for _, c := range procs {
		kill(c)
	}
}

// download baixa a voz em segundo plano. Um `a` durante o download não cancela
// nem empilha um segundo: jogar fora 20 MB já baixados num toque de tecla é caro
// demais para um gesto tão fácil de fazer sem querer.
func (p *Player) download(v Voice) {
	p.mu.Lock()
	if p.loading {
		p.mu.Unlock()
		return
	}
	p.loading = true
	p.mu.Unlock()

	go func() {
		err := Download(context.Background(), p.client, p.dir, v, func(pct int) {
			p.emit(Event{Kind: Progress, Pct: pct})
		})

		p.mu.Lock()
		p.loading = false
		p.mu.Unlock()

		if err != nil {
			p.emit(Event{Kind: Failed, Err: err})
			return
		}
		p.emit(Event{Kind: Ready})
	}()
}

// emit entrega o evento à UI. O progresso é descartável e vai sem bloquear: se a
// UI ainda não drenou, o percentual seguinte serve igual, e travar a goroutine do
// download num canal cheio seria pior. Os eventos de fim, não: perder um deixaria
// o indicador aceso para sempre.
func (p *Player) emit(e Event) {
	if e.Kind == Progress {
		select {
		case p.events <- e:
		default:
		}
		return
	}
	p.events <- e
}

// piperHelp lê o `--help` uma vez por execução, para descobrir como este piper
// chama o length_scale. O binário sai com código de erro em algumas versões, e o
// texto às vezes vai para o stderr; só o conteúdo interessa.
func (p *Player) piperHelp(bin string) string {
	p.mu.Lock()
	if p.gotHelp {
		defer p.mu.Unlock()
		return p.help
	}
	p.mu.Unlock()

	out, _ := exec.Command(bin, "--help").CombinedOutput()

	p.mu.Lock()
	p.help, p.gotHelp = string(out), true
	p.mu.Unlock()
	return string(out)
}
