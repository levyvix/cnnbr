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
	Sections      []Section     // as seções na ordem escolhida; nil = a ordem do binário
}

// Section é a escolha do leitor sobre uma seção: a posição dela na lista é a
// posição na barra de abas, e o marcador diz se ela aparece.
//
// A lista é sempre completa — todas as seções, não só as visíveis. Ver
// docs/adr/0002.
type Section struct {
	Slug    string `json:"slug"`
	Visible bool   `json:"visible"`
}

// ReconcileSections cruza a lista que veio do arquivo com os slugs que este
// binário conhece. O arquivo manda na ordem e na visibilidade; o binário manda
// em quais seções existem.
func ReconcileSections(fromFile []Section, known []string) []Section {
	exists := make(map[string]bool, len(known))
	for _, slug := range known {
		exists[slug] = true
	}

	out := make([]Section, 0, len(known))
	taken := make(map[string]bool, len(known))
	for _, s := range fromFile {
		if !exists[s.Slug] || taken[s.Slug] {
			continue
		}
		taken[s.Slug] = true
		out = append(out, s)
	}
	// Seção que o binário conhece e o arquivo não menciona entra visível: com uma
	// allowlist estrita ela nasceria oculta e ninguém descobriria que existe.
	for _, slug := range known {
		if !taken[slug] {
			out = append(out, Section{Slug: slug, Visible: true})
		}
	}

	// Um arquivo editado à mão pode ocultar todas; sem abas não há o que desenhar.
	if len(out) > 0 && !anyVisible(out) {
		out[0].Visible = true
	}
	return out
}

func anyVisible(sections []Section) bool {
	for _, s := range sections {
		if s.Visible {
			return true
		}
	}
	return false
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
	Sections      []Section // nil = ausente; a lista, quando presente, é completa
}

// Empty informa se a camada não traz nenhum campo.
func (p Partial) Empty() bool {
	return p.Justify == nil && p.Pages == nil && p.TTL == nil &&
		p.RetentionDays == nil && p.Sections == nil
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
		if layer.Sections != nil {
			p.Sections = layer.Sections
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
	Justify       *bool     `json:"justify,omitempty"`
	Pages         *int      `json:"pages,omitempty"`
	TTL           *string   `json:"cache_ttl,omitempty"`
	RetentionDays *int      `json:"history_days,omitempty"`
	Sections      []Section `json:"sections,omitempty"`
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

	layer := Partial{
		Justify:       doc.Justify,
		Pages:         doc.Pages,
		RetentionDays: doc.RetentionDays,
		Sections:      doc.Sections,
	}
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
	doc := document{
		Justify:       &p.Justify,
		Pages:         &p.Pages,
		TTL:           &ttl,
		RetentionDays: &p.RetentionDays,
		Sections:      p.Sections,
	}

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
