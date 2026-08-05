package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	s := New(path)
	s.MarkRead("a")
	if !s.ToggleSaved("b") {
		t.Fatal("ToggleSaved deveria ativar")
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}

	again := New(path)
	if !again.IsRead("a") {
		t.Error("perdeu o registro de lida")
	}
	if !again.IsSaved("b") {
		t.Error("perdeu o favorito")
	}
	if again.IsRead("z") || again.IsSaved("z") {
		t.Error("registrou id inexistente")
	}
}

func TestToggles(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "state.json"))

	if s.ToggleRead("x") != true || s.ToggleRead("x") != false {
		t.Error("ToggleRead não alternou")
	}
	if s.ToggleSaved("y") != true || s.ToggleSaved("y") != false {
		t.Error("ToggleSaved não alternou")
	}
}

func TestSourceAwareIDsKeepLegacyState(t *testing.T) {
	const (
		legacyID  = "https://cnn.example/noticia"
		currentID = "CNN Brasil\x00" + legacyID
	)

	s := New(filepath.Join(t.TempDir(), "state.json"))
	s.MarkRead(legacyID)
	if !s.ToggleSaved(legacyID) {
		t.Fatal("ToggleSaved deveria ativar")
	}

	if !s.IsRead(currentID) {
		t.Error("o ID novo não encontrou a leitura persistida com o ID antigo")
	}
	if !s.IsSaved(currentID) {
		t.Error("o ID novo não encontrou o favorito persistido com o ID antigo")
	}
	if s.ToggleRead(currentID) {
		t.Error("ToggleRead deveria desligar a leitura antiga")
	}
	if s.ToggleSaved(currentID) {
		t.Error("ToggleSaved deveria desligar o favorito antigo")
	}
}

func TestPrunePreservaFavoritos(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := New(path)
	s.MarkRead("velha")
	s.ToggleSaved("velha")
	s.state.Read["velha"] = time.Now().Add(-90 * 24 * time.Hour)

	s.Prune(30 * 24 * time.Hour)

	if s.IsRead("velha") {
		t.Error("Prune deveria remover leitura antiga")
	}
	if !s.IsSaved("velha") {
		t.Error("Prune não deve tocar em favoritos")
	}
}

func TestFlushSemMudancaNaoEscreve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")
	s := New(path)
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush limpo deveria ser no-op: %v", err)
	}
}
