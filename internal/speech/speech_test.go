package speech

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/levyvix/cnnbr/internal/article"
)

func TestLinesSpeakOnlyTheNarrativeBlocks(t *testing.T) {
	blocks := []article.Block{
		{Kind: article.Paragraph, Text: "o primeiro parágrafo"},
		{Kind: article.ListItem, Text: "TV: Premiere"},
		{Kind: article.Heading, Text: "Um intertítulo"},
		{Kind: article.Caption, Text: "Foto • Instagram/Luciana Gimenez"},
		{Kind: article.Subheading, Text: "Um subtítulo"},
		{Kind: article.Embed, Text: "vídeo"},
		{Kind: article.Quote, Text: "uma citação"},
		{Kind: article.Related, Text: "Leia também"},
		{Kind: article.Rule},
		{Kind: article.Paragraph, Text: ""},
	}

	want := []string{
		"A manchete",
		"o primeiro parágrafo",
		"Um intertítulo",
		"Um subtítulo",
		"uma citação",
	}
	if got := Lines("A manchete", blocks); !reflect.DeepEqual(got, want) {
		t.Errorf("Lines = %q, quero %q", got, want)
	}
}

func TestLinesWithoutTitle(t *testing.T) {
	got := Lines("", []article.Block{{Kind: article.Paragraph, Text: "só o corpo"}})
	if want := []string{"só o corpo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Lines = %q, quero %q", got, want)
	}
}

// fakePATH deixa no PATH só os binários pedidos, para a auto-detecção rodar sem
// depender do que está instalado na máquina do teste.
func fakePATH(t *testing.T, bins ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, bin := range bins {
		path := filepath.Join(dir, bin)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestDetectPrefersNeuralOverESpeak(t *testing.T) {
	tests := []struct {
		name string
		goos string
		bins []string
		want Engine
		err  bool
	}{
		{"linux com os dois prefere o piper", "linux", []string{"piper", "aplay", "espeak-ng"}, Piper, false},
		{"linux só com piper", "linux", []string{"piper", "aplay"}, Piper, false},
		{"paplay serve igual", "linux", []string{"piper", "paplay"}, Piper, false},
		{"linux sem piper cai no espeak", "linux", []string{"espeak-ng"}, ESpeak, false},
		{
			// O caso que faltava: dá para instalar o piper, mas ele não está aqui
			// *ainda*. Escolher Piper agora daria "não achei o piper" na hora de
			// falar; quem fala é o espeak até o `A` rodar.
			"instalável não é instalado", "linux",
			[]string{"espeak-ng", "aplay", "uv"}, ESpeak, false,
		},
		{
			"instalável com python3 em vez de uv", "linux",
			[]string{"espeak-ng", "aplay", "python3"}, ESpeak, false,
		},
		{
			// Instalável e sem espeak: não há motor agora, e o erro tem de oferecer
			// a tecla em vez de fingir que há piper.
			"instalável e sem rede de segurança", "linux",
			[]string{"aplay", "uv"}, "", true,
		},
		{
			// O piper entrega PCM sem cabeçalho: sem player, ele não fala. Quem tem
			// espeak-ng tem de ouvir por ele em vez de levar um erro.
			"piper sem player cai no espeak", "linux", []string{"piper", "espeak-ng"}, ESpeak, false,
		},
		{"piper sem player e sem espeak", "linux", []string{"piper"}, "", true},
		{"linux sem nenhum dos dois", "linux", nil, "", true},
		{"macOS usa o say do sistema", "darwin", []string{"say"}, Say, false},
		{"windows não tem suporte", "windows", []string{"piper", "espeak-ng"}, "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakePATH(t, tc.bins...)
			got, err := Detect(tc.goos, t.TempDir())
			if tc.err {
				if err == nil {
					t.Fatalf("Detect = %q, quero erro", got)
				}
				// "não tem suporte" e "falta instalar" são coisas diferentes para
				// quem lê: num Linux que só precisa de um pacote, dizer que o
				// sistema não tem suporte manda a pessoa embora sem motivo.
				if tc.goos != "windows" && strings.Contains(err.Error(), "não tem suporte") {
					t.Errorf("erro = %q; falta instalar não é falta de suporte", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if got != tc.want {
				t.Errorf("Detect = %q, quero %q", got, tc.want)
			}
		})
	}
}

// Sem piper e sem espeak-ng o erro tem que nomear os dois instaláveis: uma tecla
// que não faz nada não ensina nada.
func TestDetectErrorNamesBothInstalls(t *testing.T) {
	fakePATH(t)
	_, err := Detect("linux", t.TempDir())
	if err == nil {
		t.Fatal("quero erro sem nenhum motor instalado")
	}
	for _, want := range []string{"piper", "espeak-ng"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("o erro %q não nomeia %q", err, want)
		}
	}
}

// NeuralMissing nomeia só o que o *leitor* tem de instalar. O piper ausente não
// conta quando o cnnbr consegue buscá-lo sozinho — aí quem responde é
// NeuralInstallable, e a barra oferece a tecla.
func TestNeuralMissingNamesOnlyWhatTheReaderMustInstall(t *testing.T) {
	base := t.TempDir()

	t.Run("sem piper e sem como instalá-lo", func(t *testing.T) {
		fakePATH(t, "espeak-ng", "aplay")
		got := NeuralMissing(base)
		if !strings.Contains(got, "python3") {
			t.Errorf("NeuralMissing = %q, quero nomear o que permitiria instalar", got)
		}
		if NeuralInstallable(base) {
			t.Error("sem uv nem python3 não dá para instalar nada")
		}
	})

	t.Run("sem piper, mas instalável", func(t *testing.T) {
		fakePATH(t, "espeak-ng", "aplay", "python3")
		if got := NeuralMissing(base); got != "" {
			t.Errorf("NeuralMissing = %q, quero vazio: o cnnbr resolve sozinho", got)
		}
		if !NeuralInstallable(base) {
			t.Error("com python3 e aplay, `A` deveria dar conta")
		}
	})

	t.Run("piper sem player", func(t *testing.T) {
		fakePATH(t, "piper", "python3")
		if got := NeuralMissing(base); !strings.Contains(got, "aplay") {
			t.Errorf("NeuralMissing = %q, quero nomear o aplay", got)
		}
		if NeuralInstallable(base) {
			t.Error("instalar o piper não resolve a falta de player")
		}
	})

	t.Run("tudo pronto", func(t *testing.T) {
		fakePATH(t, "piper", "aplay")
		if got := NeuralMissing(base); got != "" {
			t.Errorf("NeuralMissing = %q, quero vazio", got)
		}
		if NeuralInstallable(base) {
			t.Error("não há o que instalar quando o piper já está aqui")
		}
	})
}

// A regressão que passou: Detect decidia por NeuralMissing, que devolve vazio
// também quando o piper está só *instalável*. Resultado: escolhia Piper numa
// máquina sem piper, e a fala morria em "não achei o piper".
func TestDetectNeverPicksPiperWithoutAPiper(t *testing.T) {
	base := t.TempDir()

	for _, bins := range [][]string{
		{"aplay", "uv"},
		{"aplay", "python3"},
		{"aplay", "uv", "espeak-ng"},
		{"paplay", "python3", "espeak-ng"},
	} {
		t.Run(strings.Join(bins, "+"), func(t *testing.T) {
			fakePATH(t, bins...)
			if !NeuralInstallable(base) {
				t.Fatal("o teste queria um cenário instalável")
			}

			engine, err := Detect("linux", base)
			if engine == Piper {
				t.Error("Detect escolheu o piper sem haver piper")
			}
			if err == nil && engine != ESpeak {
				t.Errorf("Detect = %q, quero espeak ou erro", engine)
			}
			if err != nil && !strings.Contains(err.Error(), "A") {
				t.Errorf("sem motor mas instalável, o erro %q deveria oferecer a tecla", err)
			}
		})
	}
}

// Um piper que o cnnbr instalou vale como piper, e o do sistema ganha dele.
func TestPiperPathPrefersTheSystem(t *testing.T) {
	base := t.TempDir()
	venv := filepath.Join(PiperDir(base), venvBin)
	if err := os.MkdirAll(venv, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(venv, "piper")
	if err := os.WriteFile(managed, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	fakePATH(t, "aplay")
	if got := PiperPath(base); got != managed {
		t.Errorf("PiperPath = %q, quero o piper gerenciado %q", got, managed)
	}
	if got, err := Detect("linux", base); err != nil || got != Piper {
		t.Errorf("Detect = %q (%v), quero o piper gerenciado", got, err)
	}

	fakePATH(t, "aplay", "piper")
	if got := PiperPath(base); got == managed {
		t.Error("o piper do sistema deveria ganhar do que o cnnbr instalou")
	}
}

// Um .onnx sem bit de execução não é um piper utilizável.
func TestManagedPiperNeedsToBeExecutable(t *testing.T) {
	base := t.TempDir()
	venv := filepath.Join(PiperDir(base), venvBin)
	if err := os.MkdirAll(venv, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(venv, "piper"), []byte("nada"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakePATH(t, "aplay")
	if got := PiperPath(base); got != "" {
		t.Errorf("PiperPath = %q, quero vazio: o arquivo não é executável", got)
	}
}

func TestInstallNeedsUvOrPython(t *testing.T) {
	fakePATH(t)
	err := Install(context.Background(), filepath.Join(t.TempDir(), "piper"), nil)
	if err == nil {
		t.Fatal("sem uv nem python3, instalar tem de falhar")
	}
	for _, want := range []string{"uv", "python3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("o erro %q não nomeia %q", err, want)
		}
	}
}

// Um pip que falha tem de explicar o motivo, não só "falhou".
func TestInstallReportsTheToolOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("o python falso é um script de shell")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'ERROR: No matching distribution found' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "python3"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	var steps []string
	err := Install(context.Background(), filepath.Join(dir, "venv"), func(s string) { steps = append(steps, s) })
	if err == nil {
		t.Fatal("quero erro")
	}
	if !strings.Contains(err.Error(), "No matching distribution") {
		t.Errorf("o erro %q não traz a saída da ferramenta", err)
	}
	if len(steps) == 0 {
		t.Error("a instalação deveria relatar a etapa em curso")
	}
}

func TestEngineLabel(t *testing.T) {
	if got := Piper.Label("faber"); got != "faber" {
		t.Errorf("o piper se mostra pela voz: = %q, quero %q", got, "faber")
	}
	if got := ESpeak.Label("faber"); got != "espeak" {
		t.Errorf("Label = %q, quero %q", got, "espeak")
	}
	if got := Say.Label("faber"); got != "say" {
		t.Errorf("Label = %q, quero %q", got, "say")
	}
}

func TestCommandsPerEngineAndRate(t *testing.T) {
	piper := Setup{
		Engine: Piper, Bin: "/usr/bin/piper", ScaleFlag: "--length-scale",
		Model: Model{Path: "/voices/pt_BR-faber-medium.onnx", Rate: 22050}, RawPlayer: "/usr/bin/aplay",
	}

	tests := []struct {
		name       string
		setup      Setup
		rate       int
		wantSynth  []string
		wantPlayer []string
	}{
		{
			// Em 1× nada de length_scale: o valor da própria voz já é o certo.
			name: "piper em 1×", setup: piper, rate: 100,
			wantSynth:  []string{"/usr/bin/piper", "-m", "/voices/pt_BR-faber-medium.onnx", "--output-raw"},
			wantPlayer: []string{"/usr/bin/aplay", "-q", "-f", "S16_LE", "-t", "raw", "-r", "22050", "-"},
		},
		{
			name: "piper em 2,5× estica ao inverso", setup: piper, rate: 250,
			wantSynth:  []string{"/usr/bin/piper", "-m", "/voices/pt_BR-faber-medium.onnx", "--output-raw", "--length-scale", "0.40"},
			wantPlayer: []string{"/usr/bin/aplay", "-q", "-f", "S16_LE", "-t", "raw", "-r", "22050", "-"},
		},
		{
			name:  "piper do C++ usa a outra grafia da flag",
			setup: func() Setup { s := piper; s.ScaleFlag = "--length_scale"; return s }(),
			rate:  200,
			wantSynth: []string{"/usr/bin/piper", "-m", "/voices/pt_BR-faber-medium.onnx", "--output-raw",
				"--length_scale", "0.50"},
			wantPlayer: []string{"/usr/bin/aplay", "-q", "-f", "S16_LE", "-t", "raw", "-r", "22050", "-"},
		},
		{
			// Sem saber a grafia, falamos em 1× em vez de não falar.
			name:       "piper sem flag conhecida ignora a velocidade",
			setup:      func() Setup { s := piper; s.ScaleFlag = ""; return s }(),
			rate:       250,
			wantSynth:  []string{"/usr/bin/piper", "-m", "/voices/pt_BR-faber-medium.onnx", "--output-raw"},
			wantPlayer: []string{"/usr/bin/aplay", "-q", "-f", "S16_LE", "-t", "raw", "-r", "22050", "-"},
		},
		{
			// A taxa é a da voz: uma `high` é 24000, e fixar 22050 dá voz de trator.
			name: "a taxa do player vem da voz",
			setup: func() Setup {
				s := piper
				s.Model = Model{Path: "/voices/pt_BR-jeff-high.onnx", Rate: 24000}
				return s
			}(),
			rate:       100,
			wantSynth:  []string{"/usr/bin/piper", "-m", "/voices/pt_BR-jeff-high.onnx", "--output-raw"},
			wantPlayer: []string{"/usr/bin/aplay", "-q", "-f", "S16_LE", "-t", "raw", "-r", "24000", "-"},
		},
		{
			name:       "paplay tem as próprias flags",
			setup:      func() Setup { s := piper; s.RawPlayer = "/usr/bin/paplay"; return s }(),
			rate:       100,
			wantSynth:  []string{"/usr/bin/piper", "-m", "/voices/pt_BR-faber-medium.onnx", "--output-raw"},
			wantPlayer: []string{"/usr/bin/paplay", "--raw", "--format=s16le", "--rate=22050", "--channels=1"},
		},
		{
			name: "espeak-ng em 1×", setup: Setup{Engine: ESpeak, Bin: "/usr/bin/espeak-ng"}, rate: 100,
			wantSynth: []string{"/usr/bin/espeak-ng", "-v", "pt-br", "-s", "175"},
		},
		{
			name: "espeak-ng em 2,5×", setup: Setup{Engine: ESpeak, Bin: "/usr/bin/espeak-ng"}, rate: 250,
			wantSynth: []string{"/usr/bin/espeak-ng", "-v", "pt-br", "-s", "437"},
		},
		{
			name: "say em 1×", setup: Setup{Engine: Say, Bin: "/usr/bin/say"}, rate: 100,
			wantSynth: []string{"/usr/bin/say", "-v", "Luciana", "-r", "175", "-f", "-"},
		},
		{
			name: "say em 1,5×", setup: Setup{Engine: Say, Bin: "/usr/bin/say"}, rate: 150,
			wantSynth: []string{"/usr/bin/say", "-v", "Luciana", "-r", "262", "-f", "-"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			synth, player := Commands(tc.setup, tc.rate)
			if !reflect.DeepEqual(synth, tc.wantSynth) {
				t.Errorf("motor = %q, quero %q", synth, tc.wantSynth)
			}
			if !reflect.DeepEqual(player, tc.wantPlayer) {
				t.Errorf("player = %q, quero %q", player, tc.wantPlayer)
			}
		})
	}
}

func TestScaleFlagReadsTheInstalledPiper(t *testing.T) {
	tests := []struct {
		name string
		help string
		want string
	}{
		{"piper1-gpl", "usage: piper [-h] [-m MODEL] [--length-scale LENGTH_SCALE]", "--length-scale"},
		{"piper C++", "  --length_scale  NUM   phoneme length", "--length_scale"},
		{"piper que não diz nada", "usage: piper [-h]", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := scaleFlag(tc.help); got != tc.want {
				t.Errorf("scaleFlag = %q, quero %q", got, tc.want)
			}
		})
	}
}

func TestVoiceOrFallsBackToTheDefault(t *testing.T) {
	if got := VoiceOr("jeff"); got.Name != "jeff" || got.Quality != "medium" {
		t.Errorf("VoiceOr(jeff) = %+v", got)
	}
	if got := VoiceOr("edresson"); got.Quality != "low" {
		t.Errorf("a edresson só existe em low: %+v", got)
	}
	// Um config.json editado à mão não deve calar o programa.
	if got := VoiceOr("luciana"); got.Name != DefaultVoice {
		t.Errorf("VoiceOr de nome desconhecido = %q, quero %q", got.Name, DefaultVoice)
	}
}

func TestURLsAreThePredictableHuggingFacePair(t *testing.T) {
	model, config := URLs(Voice{"faber", "medium"})
	wantModel := "https://huggingface.co/rhasspy/piper-voices/resolve/main/pt/pt_BR/faber/medium/pt_BR-faber-medium.onnx"
	if model != wantModel {
		t.Errorf("URL da voz = %q, quero %q", model, wantModel)
	}
	if config != wantModel+".json" {
		t.Errorf("URL da config = %q, quero %q", config, wantModel+".json")
	}
}

// writeVoice grava um par de fixture com a taxa pedida.
func writeVoice(t *testing.T, dir string, v Voice, rate int, withConfig bool) {
	t.Helper()
	model, config := paths(dir, v)
	if err := os.WriteFile(model, []byte("onnx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !withConfig {
		return
	}
	body := `{"audio":{"sample_rate":` + strconv.Itoa(rate) + `},"espeak":{"voice":"pt-br"}}`
	if err := os.WriteFile(config, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindNeedsThePairAndReadsTheSampleRate(t *testing.T) {
	faber := Voice{"faber", "medium"}

	t.Run("voz ausente", func(t *testing.T) {
		if _, err := Find(t.TempDir(), faber); !errors.Is(err, ErrMissing) {
			t.Errorf("erro = %v, quero ErrMissing", err)
		}
	})

	// O piper não sobe sem o .onnx.json: é de lá que ele lê o sample rate, o mapa
	// de fonemas e o length_scale.
	t.Run(".onnx solto não serve", func(t *testing.T) {
		dir := t.TempDir()
		writeVoice(t, dir, faber, 22050, false)
		if _, err := Find(dir, faber); !errors.Is(err, ErrMissing) {
			t.Errorf("erro = %v, quero ErrMissing — falta o .onnx.json", err)
		}
	})

	t.Run("o par completo", func(t *testing.T) {
		dir := t.TempDir()
		writeVoice(t, dir, faber, 24000, true)
		got, err := Find(dir, faber)
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if want := filepath.Join(dir, "pt_BR-faber-medium.onnx"); got.Path != want {
			t.Errorf("caminho = %q, quero %q", got.Path, want)
		}
		if got.Rate != 24000 {
			t.Errorf("taxa = %d, quero a do .onnx.json (24000)", got.Rate)
		}
	})

	t.Run(".onnx.json sem sample rate", func(t *testing.T) {
		dir := t.TempDir()
		writeVoice(t, dir, faber, 22050, false)
		_, config := paths(dir, faber)
		if err := os.WriteFile(config, []byte(`{"audio":{}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Find(dir, faber); err == nil {
			t.Error("sem audio.sample_rate a voz não dá para usar")
		}
	})
}

func TestDownloadWritesThePairAtomically(t *testing.T) {
	const config = `{"audio":{"sample_rate":22050}}`
	// Pequeno o bastante para o httptest declarar Content-Length em vez de
	// mandar em chunks — sem ele não há percentual a relatar.
	model := strings.Repeat("x", 1024)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".json") {
			_, _ = w.Write([]byte(config))
			return
		}
		_, _ = w.Write([]byte(model))
	}))
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "voices")
	var pcts []int
	err := download(context.Background(), srv.Client(), dir, Voice{"faber", "medium"},
		func(pct int) { pcts = append(pcts, pct) }, srv.URL)
	if err != nil {
		t.Fatalf("download: %v", err)
	}

	got, err := Find(dir, Voice{"faber", "medium"})
	if err != nil {
		t.Fatalf("a voz baixada não foi encontrada: %v", err)
	}
	if got.Rate != 22050 {
		t.Errorf("taxa = %d, quero 22050", got.Rate)
	}
	if len(pcts) == 0 || pcts[len(pcts)-1] != 100 {
		t.Errorf("progresso = %v, quero terminar em 100", pcts)
	}

	// `.tmp` + rename: nenhum resto no diretório.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("sobrou %q no diretório", e.Name())
		}
	}
	if len(entries) != 2 {
		t.Errorf("o diretório tem %d arquivos, quero o par", len(entries))
	}
}

// fakeEngine põe no PATH um espeak-ng de mentira, rodando o script indicado. O
// PATH fica só com ele: assim a auto-detecção não encontra um piper de verdade
// que esteja instalado na máquina do teste, e o script só pode usar builtins do
// shell ou caminhos absolutos.
func fakeEngine(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "espeak-ng"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return dir
}

func TestSpeakFeedsOneProcessAndReportsTheEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("o motor falso é um script de shell")
	}
	spoken := filepath.Join(t.TempDir(), "falado.txt")
	// Só builtins: `echo` e `read` não dependem do PATH, que aqui tem um binário
	// só.
	dir := fakeEngine(t, "echo '--' >> "+spoken+"\n"+
		"while IFS= read -r line; do echo \"$line\" >> "+spoken+"; done\n")

	p := New(dir, http.DefaultClient)
	p.goos = "linux"

	if out, err := p.Speak([]string{"primeira", "segunda", "terceira"}, "faber", 100); err != nil {
		t.Fatalf("Speak: %v", err)
	} else if out != Speaking {
		t.Fatalf("Speak = %v, quero Speaking", out)
	}

	select {
	case ev := <-p.Events():
		if ev.Kind != Done {
			t.Fatalf("evento = %+v, quero Done", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a fala não avisou que terminou")
	}

	data, err := os.ReadFile(spoken)
	if err != nil {
		t.Fatal(err)
	}
	if want := "--\nprimeira\nsegunda\nterceira\n"; string(data) != want {
		t.Errorf("o motor recebeu %q, quero %q", data, want)
	}
	// Um único `--`: um processo por sessão de fala, não um por bloco.
	if n := strings.Count(string(data), "--"); n != 1 {
		t.Errorf("o motor foi invocado %d vezes, quero 1 por sessão de fala", n)
	}
}

func TestStopKillsWithoutAnnouncingTheEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("o motor falso é um script de shell")
	}
	// O caminho do sleep sai do PATH de verdade, antes de o falso entrar.
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sem sleep para fazer o motor falso demorar")
	}
	dir := fakeEngine(t, "exec "+sleep+" 30\n")

	p := New(dir, http.DefaultClient)
	p.goos = "linux"
	if _, err := p.Speak([]string{"uma frase"}, "faber", 100); err != nil {
		t.Fatalf("Speak: %v", err)
	}

	p.Stop()

	// Quem parou já sabe: um Done atrasado apagaria o indicador de uma fala nova.
	select {
	case ev := <-p.Events():
		t.Fatalf("Stop emitiu %+v, quero silêncio", ev)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSpeakWithoutTextIsAnError(t *testing.T) {
	p := New(t.TempDir(), http.DefaultClient)
	if _, err := p.Speak(nil, "faber", 100); err == nil {
		t.Error("uma matéria sem texto para falar tem que virar erro")
	}
}

func TestDownloadFailsWithoutLeavingHalfAVoice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "voices")
	err := download(context.Background(), srv.Client(), dir, Voice{"faber", "medium"}, nil, srv.URL)
	if err == nil {
		t.Fatal("um 404 tem que virar erro")
	}
	if _, err := Find(dir, Voice{"faber", "medium"}); !errors.Is(err, ErrMissing) {
		t.Errorf("depois de falhar, Find = %v, quero ErrMissing", err)
	}
}
