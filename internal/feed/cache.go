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

// Cache guarda o feed de cada seção em disco para que a troca de aba seja
// instantânea e o leitor funcione offline.
type Cache struct {
	Dir string
	TTL time.Duration
}

// cacheVersion invalida o cache quando o formato de Item muda — sem isso, um
// campo novo só apareceria depois que o TTL expirasse.
const cacheVersion = 2

type cacheFile struct {
	Version   int       `json:"version"`
	FetchedAt time.Time `json:"fetched_at"`
	Items     []Item    `json:"items"`
}

// DefaultCache aponta para $XDG_CACHE_HOME/cnnbr.
func DefaultCache(ttl time.Duration) Cache {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return Cache{Dir: filepath.Join(dir, "cnnbr"), TTL: ttl}
}

func (c Cache) path(slug string) string {
	return filepath.Join(c.Dir, "feed-"+slug+".json")
}

// Load devolve os itens em cache de uma seção e quando foram buscados.
func (c Cache) Load(slug string) ([]Item, time.Time, error) {
	data, err := os.ReadFile(c.path(slug))
	if err != nil {
		return nil, time.Time{}, err
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, time.Time{}, err
	}
	if cf.Version != cacheVersion {
		return nil, time.Time{}, errStaleCache
	}
	return cf.Items, cf.FetchedAt, nil
}

// Save grava os itens de uma seção no cache.
func (c Cache) Save(slug string, items []Item, at time.Time) error {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(cacheFile{Version: cacheVersion, FetchedAt: at, Items: items})
	if err != nil {
		return err
	}
	tmp := c.path(slug) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path(slug))
}

// Result é o desfecho de um Get: itens, quando foram buscados e se vieram do
// cache. Err traz o motivo de a rede ter falhado quando o cache salvou o dia.
type Result struct {
	Section   Section
	Items     []Item
	FetchedAt time.Time
	FromCache bool
	Err       error
}

// Get devolve o feed de uma seção, preferindo o cache enquanto ele estiver
// dentro do TTL. Com force=true a rede é sempre consultada, mas um erro cai de
// volta no cache.
func Get(ctx context.Context, client *http.Client, c Cache, s Section, pages int, force bool) Result {
	cached, fetchedAt, cacheErr := c.Load(s.Slug)
	fresh := cacheErr == nil && len(cached) > 0 && time.Since(fetchedAt) < c.TTL

	if !force && fresh {
		return Result{Section: s, Items: cached, FetchedAt: fetchedAt, FromCache: true}
	}

	items, err := Fetch(ctx, client, s.Cat, pages)
	if err != nil {
		if cacheErr == nil && len(cached) > 0 {
			return Result{Section: s, Items: cached, FetchedAt: fetchedAt, FromCache: true, Err: err}
		}
		return Result{Section: s, Err: err}
	}

	now := nowFn()
	// Falha ao gravar o cache não impede a leitura.
	_ = c.Save(s.Slug, items, now)
	return Result{Section: s, Items: items, FetchedAt: now}
}

// errStaleCache marca um arquivo de cache gravado por uma versão anterior.
var errStaleCache = errors.New("cache de formato antigo")

// nowFn existe para permitir teste determinístico.
var nowFn = time.Now
