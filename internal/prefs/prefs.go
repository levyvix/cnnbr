// Package prefs persiste as preferências do leitor: as escolhas que sobrevivem
// ao encerramento, distintas das dependências de execução que o main injeta.
package prefs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Prefs são as preferências já resolvidas, prontas para uso.
type Prefs struct {
	Justify       bool          // texto justificado nas duas margens
	Pages         int           // páginas do feed a buscar
	TTL           time.Duration // validade do cache
	RetentionDays int           // dias de histórico de leitura; 0 = nunca podar
}

// Defaults são os padrões embutidos, a camada de baixo da resolução.
func Defaults() Prefs {
	return Prefs{
		Justify:       true,
		Pages:         2,
		TTL:           15 * time.Minute,
		RetentionDays: 60,
	}
}

// Partial é uma camada de preferências onde cada campo pode estar ausente.
// Ausente e "o valor zero" são coisas diferentes: `-justify=false` digitado na
// linha de comando precisa se distinguir de nenhuma flag, senão o arquivo nunca
// consegue desligar a justificação.
type Partial struct {
	Justify       *bool
	Pages         *int
	TTL           *time.Duration
	RetentionDays *int
}

// Empty informa se a camada não traz nenhum campo.
func (p Partial) Empty() bool {
	return p == Partial{}
}

// Resolve empilha as três camadas: embutido, depois arquivo, depois flags.
// É pura de propósito — o main fica só com a travessia das flags.
func Resolve(file, flags Partial) Prefs {
	p := Defaults()
	for _, layer := range []Partial{file, flags} {
		if layer.Justify != nil {
			p.Justify = *layer.Justify
		}
		if layer.Pages != nil {
			p.Pages = *layer.Pages
		}
		if layer.TTL != nil {
			p.TTL = *layer.TTL
		}
		if layer.RetentionDays != nil {
			p.RetentionDays = *layer.RetentionDays
		}
	}
	return p
}

// Retention é a idade máxima do histórico de leitura, ou zero quando a poda
// está desligada.
func (p Prefs) Retention() time.Duration {
	if p.RetentionDays <= 0 {
		return 0
	}
	return time.Duration(p.RetentionDays) * 24 * time.Hour
}

// document é o formato em disco. A duração vai como string ("15m") porque
// time.Duration em JSON serializa como nanossegundos, e a retenção vai em dias
// porque é assim que se pensa nela — não como "1440h".
type document struct {
	Justify       *bool   `json:"justify,omitempty"`
	Pages         *int    `json:"pages,omitempty"`
	TTL           *string `json:"cache_ttl,omitempty"`
	RetentionDays *int    `json:"history_days,omitempty"`
}

// DefaultPath aponta para $XDG_CONFIG_HOME/cnnbr/config.json. É deliberadamente
// separado do state.json, que guarda o que foi lido e favoritado.
func DefaultPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "cnnbr", "config.json")
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "cnnbr", "config.json")
}

// Load lê a camada do arquivo. Arquivo ausente devolve uma camada vazia sem
// erro — é o caso normal na primeira execução. Arquivo inválido devolve a
// camada vazia *e* o erro, para quem chamou avisar e seguir com os padrões.
func Load(path string) (Partial, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Partial{}, nil
	}
	if err != nil {
		return Partial{}, err
	}

	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return Partial{}, err
	}

	layer := Partial{Justify: doc.Justify, Pages: doc.Pages, RetentionDays: doc.RetentionDays}
	if doc.TTL != nil {
		ttl, err := time.ParseDuration(*doc.TTL)
		if err != nil {
			return Partial{}, fmt.Errorf("cache_ttl %q: %w", *doc.TTL, err)
		}
		layer.TTL = &ttl
	}
	return layer, nil
}

// Save grava as preferências, criando o diretório se preciso. O arquivo só
// nasce aqui: no arranque, a ausência dele é silenciosa.
func Save(path string, p Prefs) error {
	ttl := p.TTL.String()
	doc := document{Justify: &p.Justify, Pages: &p.Pages, TTL: &ttl, RetentionDays: &p.RetentionDays}

	// Indentado porque o arquivo é para ser editado à mão.
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
