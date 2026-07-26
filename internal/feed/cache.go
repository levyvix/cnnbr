package feed

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Cache guarda o último feed em disco para que a abertura seja instantânea e o
// leitor funcione offline.
type Cache struct {
	Path string
	TTL  time.Duration
}

type cacheFile struct {
	FetchedAt time.Time `json:"fetched_at"`
	Items     []Item    `json:"items"`
}

// DefaultCache aponta para $XDG_CACHE_HOME/cnnbr/feed.json.
func DefaultCache(ttl time.Duration) Cache {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return Cache{Path: filepath.Join(dir, "cnnbr", "feed.json"), TTL: ttl}
}

// Load devolve os itens em cache e quando foram buscados.
func (c Cache) Load() ([]Item, time.Time, error) {
	data, err := os.ReadFile(c.Path)
	if err != nil {
		return nil, time.Time{}, err
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, time.Time{}, err
	}
	return cf.Items, cf.FetchedAt, nil
}

// Save grava os itens no cache.
func (c Cache) Save(items []Item, at time.Time) error {
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(cacheFile{FetchedAt: at, Items: items})
	if err != nil {
		return err
	}
	tmp := c.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.Path)
}

// Result é o desfecho de um Get: itens, quando foram buscados e se vieram do
// cache. Err traz o motivo de a rede ter falhado quando o cache salvou o dia.
type Result struct {
	Items     []Item
	FetchedAt time.Time
	FromCache bool
	Err       error
}

// Get devolve o feed, preferindo o cache enquanto ele estiver dentro do TTL.
// Com force=true a rede é sempre consultada, mas um erro cai de volta no cache.
func Get(ctx context.Context, client *http.Client, c Cache, pages int, force bool) Result {
	cached, fetchedAt, cacheErr := c.Load()
	fresh := cacheErr == nil && len(cached) > 0 && time.Since(fetchedAt) < c.TTL

	if !force && fresh {
		return Result{Items: cached, FetchedAt: fetchedAt, FromCache: true}
	}

	items, err := Fetch(ctx, client, pages)
	if err != nil {
		if cacheErr == nil && len(cached) > 0 {
			return Result{Items: cached, FetchedAt: fetchedAt, FromCache: true, Err: err}
		}
		return Result{Err: err}
	}

	now := nowFn()
	if saveErr := c.Save(items, now); saveErr != nil && !errors.Is(saveErr, os.ErrPermission) {
		// Falha ao gravar cache não impede a leitura.
		_ = saveErr
	}
	return Result{Items: items, FetchedAt: now}
}

// nowFn existe para permitir teste determinístico.
var nowFn = time.Now
