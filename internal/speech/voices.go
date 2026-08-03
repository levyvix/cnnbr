package speech

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Voice é uma voz pt-BR do piper: o nome e a qualidade em que ela existe no
// repositório. Só estas quatro existem, então a tabela vai no binário.
type Voice struct {
	Name    string
	Quality string
}

// Voices são as vozes na ordem em que o painel as percorre. As `medium` são
// ~63 MB cada; a `low`, bem menos.
var Voices = []Voice{
	{"cadu", "medium"},
	{"faber", "medium"},
	{"jeff", "medium"},
	{"edresson", "low"},
}

// DefaultVoice é o padrão embutido, e o que uma preferência com nome
// desconhecido resolve para.
const DefaultVoice = "faber"

// VoiceOr acha a voz pelo nome, caindo no padrão quando o nome não existe — um
// config.json editado à mão não deve calar o programa.
func VoiceOr(name string) Voice {
	for _, v := range Voices {
		if v.Name == name {
			return v
		}
	}
	for _, v := range Voices {
		if v.Name == DefaultVoice {
			return v
		}
	}
	return Voices[0]
}

// Model é o par de arquivos de uma voz já em disco, com a taxa de amostragem que
// o .onnx.json declara.
type Model struct {
	Path string // o .onnx
	Rate int    // audio.sample_rate
}

// ErrMissing é a voz que ainda não está em disco. Não é falha: é o sinal para
// baixar.
var ErrMissing = errors.New("a voz não está em disco")

// Dir é onde as vozes ficam: $XDG_DATA_HOME/cnnbr/voices.
func Dir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "cnnbr", "voices")
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "cnnbr", "voices")
}

// paths são os dois arquivos da voz dentro do diretório.
func paths(dir string, v Voice) (model, config string) {
	name := fmt.Sprintf("pt_BR-%s-%s.onnx", v.Name, v.Quality)
	return filepath.Join(dir, name), filepath.Join(dir, name+".json")
}

// hfBase é a raiz das vozes pt-BR no repositório do piper. As URLs abaixo dele
// são previsíveis, e é por isso que não precisamos de índice nem de API.
const hfBase = "https://huggingface.co/rhasspy/piper-voices/resolve/main/pt/pt_BR"

// URLs são as duas URLs da voz no repositório do piper.
func URLs(v Voice) (model, config string) { return urls(hfBase, v) }

func urls(base string, v Voice) (model, config string) {
	name := fmt.Sprintf("pt_BR-%s-%s.onnx", v.Name, v.Quality)
	model = fmt.Sprintf("%s/%s/%s/%s", base, v.Name, v.Quality, name)
	return model, model + ".json"
}

// Find acha a voz em disco. Procura o *par*, não o .onnx solto: o piper não sobe
// sem o .onnx.json, que é de onde ele lê o sample rate, o mapa de fonemas e o
// length_scale.
func Find(dir string, v Voice) (Model, error) {
	model, config := paths(dir, v)
	for _, path := range []string{model, config} {
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			return Model{}, fmt.Errorf("%w: %s", ErrMissing, filepath.Base(path))
		}
	}

	rate, err := sampleRate(config)
	if err != nil {
		return Model{}, err
	}
	return Model{Path: model, Rate: rate}, nil
}

// sampleRate lê audio.sample_rate do .onnx.json.
func sampleRate(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var doc struct {
		Audio struct {
			SampleRate int `json:"sample_rate"`
		} `json:"audio"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return 0, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if doc.Audio.SampleRate <= 0 {
		return 0, fmt.Errorf("%s: sem audio.sample_rate", filepath.Base(path))
	}
	return doc.Audio.SampleRate, nil
}

// Download busca o par de arquivos da voz e o grava em dir, chamando onProgress
// com o percentual do .onnx — que é 63 MB contra 5 kB do .onnx.json, e portanto
// é o download que o leitor está esperando.
//
// Não há verificação de checksum: um download corrompido vira um erro do piper,
// não uma mensagem nossa. Decisão consciente.
//
// Também não usamos o baixador do próprio piper
// (`python3 -m piper.download_voices`): ele só existe no pacote Python novo, e
// descobrir *qual* python é o do piper é frágil quando ele veio de pipx, de um
// venv ou do pacote da distro.
func Download(ctx context.Context, client *http.Client, dir string, v Voice, onProgress func(pct int)) error {
	return download(ctx, client, dir, v, onProgress, hfBase)
}

// download é o Download com a raiz das URLs à mostra, para o teste apontar para
// um httptest em vez do HuggingFace.
func download(ctx context.Context, client *http.Client, dir string, v Voice, onProgress func(pct int), base string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	modelURL, configURL := urls(base, v)
	modelPath, configPath := paths(dir, v)

	// O .onnx.json primeiro, e sem progresso: são 5 kB, e assim o par nunca fica
	// pela metade do lado que importa — um .onnx sem o .json não serve para nada.
	if err := fetch(ctx, client, configURL, configPath, nil); err != nil {
		return err
	}
	return fetch(ctx, client, modelURL, modelPath, onProgress)
}

// fetch grava a URL num `.tmp` e renomeia no fim, como prefs.Save faz: um
// download interrompido não deixa um arquivo pela metade no lugar do bom.
func fetch(ctx context.Context, client *http.Client, url, path string, onProgress func(pct int)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", filepath.Base(path), res.Status)
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	var src io.Reader = res.Body
	if onProgress != nil && res.ContentLength > 0 {
		src = &progressReader{src: res.Body, total: res.ContentLength, report: onProgress}
	}
	if _, err := io.Copy(f, src); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// progressReader relata o percentual a cada ponto inteiro. A granularidade não é
// estética: sem ela, 63 MB rendem milhares de avisos para uma barra que só tem
// cem estados.
type progressReader struct {
	src    io.Reader
	total  int64
	done   int64
	last   int
	report func(pct int)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	r.done += int64(n)
	if pct := int(r.done * 100 / r.total); pct != r.last {
		r.last = pct
		r.report(pct)
	}
	return n, err
}
