// Package store persiste o que já foi lido e o que foi salvo para depois.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// State é o estado persistido do leitor.
type State struct {
	Read  map[string]time.Time `json:"read"`
	Saved map[string]time.Time `json:"saved"`
}

// Store lê e grava o State em disco.
type Store struct {
	path  string
	mu    sync.Mutex
	state State
	dirty bool // se o cache atual em memoria esta "desatualuzado" do cache salvo em disco
}

// Default aponta para $XDG_DATA_HOME/cnnbr/state.json.
func Default() *Store {
	dir, err := os.UserHomeDir()
	base := filepath.Join(dir, ".local", "share")
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		base = v
	}
	if err != nil {
		base = os.TempDir()
	}
	return New(filepath.Join(base, "cnnbr", "state.json"))
}

// New cria um Store no caminho indicado e carrega o que existir.
func New(path string) *Store {
	s := &Store{path: path, state: State{Read: map[string]time.Time{}, Saved: map[string]time.Time{}}}
	s.load()
	return s
}

func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return
	}
	if st.Read != nil {
		s.state.Read = st.Read
	}
	if st.Saved != nil {
		s.state.Saved = st.Saved
	}
}

// Flush grava o estado se houver mudança pendente.
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

// IsRead informa se a matéria já foi aberta.
func (s *Store) IsRead(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.state.Read[id]
	if !ok {
		_, ok = s.state.Read[legacyID(id)]
	}
	return ok
}

// MarkRead registra a matéria como lida.
func (s *Store) MarkRead(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.Read[id]; ok {
		return
	}
	if legacy := legacyID(id); legacy != id {
		if at, ok := s.state.Read[legacy]; ok {
			delete(s.state.Read, legacy)
			s.state.Read[id] = at
			s.dirty = true
			return
		}
	}
	s.state.Read[id] = time.Now()
	s.dirty = true
}

// ToggleRead alterna o estado de leitura e devolve o novo valor.
func (s *Store) ToggleRead(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.Read[id]; ok {
		delete(s.state.Read, id)
		s.dirty = true
		return false
	}
	if legacy := legacyID(id); legacy != id {
		if _, ok := s.state.Read[legacy]; ok {
			delete(s.state.Read, legacy)
			s.dirty = true
			return false
		}
	}
	s.state.Read[id] = time.Now()
	s.dirty = true
	return true
}

// IsSaved informa se a matéria está nos favoritos.
func (s *Store) IsSaved(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.state.Saved[id]
	if !ok {
		_, ok = s.state.Saved[legacyID(id)]
	}
	return ok
}

// ToggleSaved alterna o favorito e devolve o novo valor.
func (s *Store) ToggleSaved(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.Saved[id]; ok {
		delete(s.state.Saved, id)
		s.dirty = true
		return false
	}
	if legacy := legacyID(id); legacy != id {
		if _, ok := s.state.Saved[legacy]; ok {
			delete(s.state.Saved, legacy)
			s.dirty = true
			return false
		}
	}
	s.state.Saved[id] = time.Now()
	s.dirty = true
	return true
}

func legacyID(id string) string {
	if sourceEnd := strings.IndexByte(id, 0); sourceEnd >= 0 {
		return id[sourceEnd+1:]
	}
	return id
}

// Prune remove registros de matérias que saíram do feed há mais de `maxAge`,
// para o arquivo de estado não crescer indefinidamente. Favoritos nunca são
// removidos.
func (s *Store) Prune(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for id, at := range s.state.Read {
		if at.Before(cutoff) {
			delete(s.state.Read, id)
			s.dirty = true
		}
	}
}
