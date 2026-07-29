package prefs

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSaveLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := Prefs{
		Justify: false, Pages: 4, TTL: 30 * time.Minute, RetentionDays: 7,
		Sections: []Section{{"home", true}, {"pop", false}, {"esportes", true}},
	}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if resolved := Resolve(got, Partial{}); !reflect.DeepEqual(resolved, want) {
		t.Errorf("roundtrip = %+v, quero %+v", resolved, want)
	}
}

func TestReconcileSections(t *testing.T) {
	known := []string{"home", "politica", "economia"}

	tests := []struct {
		name     string
		fromFile []Section
		want     []Section
	}{
		{
			name: "sem arquivo, todas visíveis na ordem do binário",
			want: []Section{{"home", true}, {"politica", true}, {"economia", true}},
		},
		{
			name:     "a ordem do arquivo é respeitada",
			fromFile: []Section{{"economia", true}, {"home", true}, {"politica", true}},
			want:     []Section{{"economia", true}, {"home", true}, {"politica", true}},
		},
		{
			name:     "o marcador de visibilidade é respeitado",
			fromFile: []Section{{"home", true}, {"politica", false}, {"economia", true}},
			want:     []Section{{"home", true}, {"politica", false}, {"economia", true}},
		},
		{
			// Seção que a CNN aposentou e este binário não conhece mais.
			name:     "slug desconhecido é ignorado",
			fromFile: []Section{{"blogs", true}, {"home", true}, {"politica", true}, {"economia", true}},
			want:     []Section{{"home", true}, {"politica", true}, {"economia", true}},
		},
		{
			// Sem isso, uma seção nova nasceria oculta e ninguém descobriria que
			// ela existe.
			name:     "seção nova entra visível no fim",
			fromFile: []Section{{"economia", false}, {"home", true}},
			want:     []Section{{"economia", false}, {"home", true}, {"politica", true}},
		},
		{
			name:     "slug repetido entra uma vez só",
			fromFile: []Section{{"home", false}, {"home", true}},
			want:     []Section{{"home", false}, {"politica", true}, {"economia", true}},
		},
		{
			// Um arquivo editado à mão pode ocultar tudo; sem abas não há o que
			// desenhar.
			name:     "tudo oculto reexibe a primeira",
			fromFile: []Section{{"economia", false}, {"home", false}, {"politica", false}},
			want:     []Section{{"economia", true}, {"home", false}, {"politica", false}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReconcileSections(tc.fromFile, known); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ReconcileSections = %+v, quero %+v", got, tc.want)
			}
		})
	}
}

// A duração vai como string para o arquivo ser editável à mão: "15m", não
// 900000000000.
func TestSaveWritesReadableDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Prefs{TTL: 15 * time.Minute, Pages: 2, RetentionDays: 60}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if want := `"cache_ttl": "15m0s"`; !strings.Contains(string(data), want) {
		t.Errorf("arquivo não contém %s:\n%s", want, data)
	}
	if want := `"history_days": 60`; !strings.Contains(string(data), want) {
		t.Errorf("arquivo não contém %s:\n%s", want, data)
	}
}

func TestLoadMissingFileIsSilent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nao-existe.json")

	got, err := Load(path)
	if err != nil {
		t.Fatalf("arquivo ausente não é erro, veio %v", err)
	}
	if resolved := Resolve(got, Partial{}); !reflect.DeepEqual(resolved, Defaults()) {
		t.Errorf("sem arquivo = %+v, quero os padrões embutidos %+v", resolved, Defaults())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Load criou o arquivo; ele só deve nascer ao gravar")
	}
}

func TestLoadInvalidFileReportsAndFallsBack(t *testing.T) {
	tests := map[string]string{
		"json quebrado":    `{"pages": `,
		"tipo errado":      `{"pages": "duas"}`,
		"duração inválida": `{"cache_ttl": "quinze minutos"}`,
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}

			got, err := Load(path)
			if err == nil {
				t.Error("arquivo inválido deveria reportar erro")
			}
			if resolved := Resolve(got, Partial{}); !reflect.DeepEqual(resolved, Defaults()) {
				t.Errorf("= %+v, quero os padrões embutidos %+v", resolved, Defaults())
			}
		})
	}
}

// Precedência: flag > arquivo > embutido.
func TestResolvePrecedence(t *testing.T) {
	boolp := func(b bool) *bool { return &b }
	intp := func(n int) *int { return &n }
	durp := func(d time.Duration) *time.Duration { return &d }

	tests := []struct {
		name  string
		file  Partial
		flags Partial
		want  Prefs
	}{
		{
			name: "sem arquivo nem flags, os padrões embutidos",
			want: Defaults(),
		},
		{
			name: "o arquivo vence o embutido",
			file: Partial{Justify: boolp(false), Pages: intp(5)},
			want: Prefs{Justify: false, Pages: 5, TTL: Defaults().TTL, RetentionDays: Defaults().RetentionDays},
		},
		{
			name:  "a flag vence o arquivo",
			file:  Partial{Pages: intp(5), TTL: durp(time.Hour)},
			flags: Partial{Pages: intp(1)},
			want:  Prefs{Justify: Defaults().Justify, Pages: 1, TTL: time.Hour, RetentionDays: Defaults().RetentionDays},
		},
		{
			name:  "a flag vence o embutido",
			flags: Partial{TTL: durp(5 * time.Minute)},
			want:  Prefs{Justify: Defaults().Justify, Pages: Defaults().Pages, TTL: 5 * time.Minute, RetentionDays: Defaults().RetentionDays},
		},
		{
			name: "flag ausente não sobrepõe o arquivo, mesmo com o valor zero",
			file: Partial{Justify: boolp(false), RetentionDays: intp(0)},
			want: Prefs{Justify: false, Pages: Defaults().Pages, TTL: Defaults().TTL, RetentionDays: 0},
		},
		{
			name:  "a flag desliga o que o arquivo ligou",
			file:  Partial{Justify: boolp(true)},
			flags: Partial{Justify: boolp(false)},
			want:  Prefs{Justify: false, Pages: Defaults().Pages, TTL: Defaults().TTL, RetentionDays: Defaults().RetentionDays},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Resolve(tc.file, tc.flags); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Resolve = %+v, quero %+v", got, tc.want)
			}
		})
	}
}

// Empty enumera os campos à mão, porque uma camada com lista de seções não é
// comparável com ==. Este teste é a rede: um campo novo esquecido lá faria a
// preferência deixar de ser gravada, em silêncio.
func TestEmptySeesEveryField(t *testing.T) {
	if !(Partial{}).Empty() {
		t.Fatal("a camada vazia deveria ser Empty")
	}

	full := reflect.ValueOf(Partial{
		Justify:       new(bool),
		Pages:         new(int),
		TTL:           new(time.Duration),
		RetentionDays: new(int),
		Sections:      []Section{},
	})
	for i := range full.NumField() {
		name := full.Type().Field(i).Name
		if full.Field(i).IsNil() {
			t.Fatalf("o teste não preencheu o campo %s", name)
		}

		var one Partial
		reflect.ValueOf(&one).Elem().Field(i).Set(full.Field(i))
		if one.Empty() {
			t.Errorf("uma camada só com %s foi tratada como vazia — ela não seria gravada", name)
		}
	}
}

func TestDefaultJustifyIsOn(t *testing.T) {
	if !Defaults().Justify {
		t.Error("o padrão embutido de justificação é sim")
	}
}
