package speech

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Engine é o motor em uso nesta execução.
type Engine string

const (
	Piper  Engine = "piper"
	ESpeak Engine = "espeak"
	Say    Engine = "say"
)

const (
	piperBin  = "piper"
	espeakBin = "espeak-ng"
	sayBin    = "say"
)

// Não há sentinel para "sem motor": nada no programa se ramifica nesse erro, só
// a barra de status o mostra. E as três causas pedem frases diferentes — "sua
// plataforma não serve" e "falta instalar" são coisas distintas para quem lê, e
// juntá-las num prefixo comum foi o que fez a mensagem soar como se o Linux não
// tivesse suporte.

// Detect escolhe o motor por auto-detecção no PATH: no Linux, o piper com voz
// neural na frente do espeak-ng; no macOS, o `say` que já vem no sistema; no
// Windows, nada.
//
// Não exigimos a voz em disco — ela é baixada na hora do primeiro `a`, e
// exigi-la aqui faria o espeak ganhar para sempre de quem acabou de instalar o
// piper.
func Detect(goos, base string) (Engine, error) {
	switch goos {
	case "windows":
		return "", errors.New("ouvir a matéria não tem suporte no Windows")
	case "darwin":
		if _, err := exec.LookPath(sayBin); err == nil {
			return Say, nil
		}
		return "", errors.New("não achei o say para ouvir a matéria")
	}

	if NeuralMissing(base) == "" {
		return Piper, nil
	}
	if _, err := exec.LookPath(espeakBin); err == nil {
		return ESpeak, nil
	}
	// Não há como falar. A mensagem nomeia o caminho mais curto para sair disso, e
	// deixa claro que a fala depende de um motor externo: a *voz* do piper baixa
	// sozinha, o motor não.
	switch {
	case CanInstall():
		return "", errors.New("aperte A para instalar a voz neural, ou instale espeak-ng")
	case PiperPath(base) != "":
		// O piper está aqui; falta o que toca o PCM que ele produz.
		return "", fmt.Errorf("instale %s para tocar a voz neural, ou espeak-ng", NeuralMissing(base))
	default:
		return "", errors.New("instale piper (voz neural) ou espeak-ng para ouvir a matéria")
	}
}

// PiperPath é o piper a usar: o do sistema na frente do que o cnnbr instalou,
// para respeitar quem já mantém o seu. Vazio quando não há nenhum.
func PiperPath(base string) string {
	if path, err := exec.LookPath(piperBin); err == nil {
		return path
	}
	return managedPiper(PiperDir(base))
}

// NeuralMissing nomeia o que o *leitor* precisa instalar para haver voz neural,
// ou "" quando nada falta do lado dele. Sustenta a escolha do motor e o aviso de
// uma vez por execução para quem está ouvindo pelo espeak-ng ou pelo `say`.
//
// O piper ausente não conta como falta quando dá para instalá-lo sozinho — nesse
// caso quem responde é NeuralInstallable, e a barra oferece a tecla em vez de
// mandar a pessoa ao gerenciador de pacotes.
//
// O piper entrega PCM sem cabeçalho, então ele sozinho não fala: sem aplay nem
// paplay, quem tem espeak-ng instalado tem de ouvir por ele em vez de levar um
// erro.
func NeuralMissing(base string) string {
	if PiperPath(base) == "" {
		if CanInstall() {
			return ""
		}
		return "uv ou python3 (para o cnnbr baixar o piper) — ou o piper direto"
	}
	if _, err := findRawPlayer(); err != nil {
		return "alsa-utils (aplay) ou pulseaudio-utils (paplay)"
	}
	return ""
}

// NeuralInstallable diz se falta só apertar a tecla: o piper não está aqui, mas
// dá para o cnnbr instalá-lo, e há como tocar o que ele produzir.
func NeuralInstallable(base string) bool {
	if PiperPath(base) != "" || !CanInstall() {
		return false
	}
	_, err := findRawPlayer()
	return err == nil
}

// Label é o que o indicador da barra de status mostra: no piper, a voz em uso,
// que é a informação que muda; nos outros, o nome do motor.
func (e Engine) Label(voice string) string {
	if e == Piper {
		return voice
	}
	return string(e)
}

// rawPlayers tocam PCM cru na saída padrão; o primeiro presente ganha.
var rawPlayers = []string{"aplay", "paplay"}

func findRawPlayer() (string, error) {
	for _, bin := range rawPlayers {
		if path, err := exec.LookPath(bin); err == nil {
			return path, nil
		}
	}
	return "", errors.New("instale alsa-utils (aplay) ou pulseaudio-utils (paplay) para tocar a voz neural")
}

// Setup é o que a descoberta do ambiente achou. Separar isto da montagem do
// comando é o que deixa a montagem pura, e portanto testável sem piper
// instalado.
type Setup struct {
	Engine Engine
	Bin    string // caminho do motor

	// ScaleFlag é como o piper instalado chama o length_scale, ou vazio quando
	// não dá para saber. Ver scaleFlag.
	ScaleFlag string

	Model     Model  // a voz em disco, só no piper
	RawPlayer string // aplay/paplay, só no piper
}

// baseWPM são as palavras por minuto que o espeak-ng e o `say` usam por padrão,
// e portanto o que 1× significa neles.
const baseWPM = 175

// wpm traduz a velocidade percentual do painel para palavras por minuto.
func wpm(rate int) int { return baseWPM * rate / 100 }

// lengthScale é a mesma velocidade no piper, onde o botão é o inverso: esticar
// a duração da fala é ir mais devagar. 1× é 1,0; 2,5× é 0,4.
func lengthScale(rate int) float64 { return 100 / float64(rate) }

// Commands monta o motor e, quando o áudio sai cru, o player que o toca.
// O player vem vazio nos motores que falam pela própria conta.
func Commands(s Setup, rate int) (synth, player []string) {
	if rate <= 0 {
		rate = 100
	}

	switch s.Engine {
	case Piper:
		synth = []string{s.Bin, "-m", s.Model.Path, "--output-raw"}
		// Em 1× não passamos nada: o length_scale da própria voz já é o certo, e
		// a flag é o ponto onde as duas linhagens de piper divergem.
		if rate != 100 && s.ScaleFlag != "" {
			synth = append(synth, s.ScaleFlag, strconv.FormatFloat(lengthScale(rate), 'f', 2, 64))
		}
		return synth, rawPlayerArgs(s.RawPlayer, s.Model.Rate)

	case ESpeak:
		return []string{s.Bin, "-v", "pt-br", "-s", strconv.Itoa(wpm(rate))}, nil

	case Say:
		// `-f -` porque o `say` só lê o stdin quando mandado; sem isso ele espera
		// o texto nos argumentos.
		return []string{s.Bin, "-v", "Luciana", "-r", strconv.Itoa(wpm(rate)), "-f", "-"}, nil
	}
	return nil, nil
}

// rawPlayerArgs monta o player de PCM cru. A taxa vem sempre da voz: as vozes
// `high` são 24000, e fixar 22050 dá voz de esquilo ou de trator.
func rawPlayerArgs(bin string, rate int) []string {
	if filepath.Base(bin) == "paplay" {
		return []string{bin, "--raw", "--format=s16le", fmt.Sprintf("--rate=%d", rate), "--channels=1"}
	}
	return []string{bin, "-q", "-f", "S16_LE", "-t", "raw", "-r", strconv.Itoa(rate), "-"}
}

// scaleFlag acha, na saída do `--help`, como o piper instalado chama o
// length_scale.
//
// Existem duas linhagens: o binário C++ arquivado (rhasspy/piper) usa
// `--length_scale`, e o piper1-gpl atual usa `--length-scale`. Nenhum aceita a
// flag do outro, então lemos o help em vez de chutar — e sem resposta preferimos
// falar em 1× a não falar.
func scaleFlag(help string) string {
	switch {
	case strings.Contains(help, "--length-scale"):
		return "--length-scale"
	case strings.Contains(help, "--length_scale"):
		return "--length_scale"
	}
	return ""
}
