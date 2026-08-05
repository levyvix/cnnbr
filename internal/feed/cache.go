package feed

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Cache guarda o feed de cada fonte e seção em disco para que a troca de aba
// seja instantânea e o leitor funcione offline.
type Cache struct {
	Dir string
	TTL time.Duration
}

// cacheVersion invalida o cache quando o formato de Item muda — sem isso, um
// campo novo só apareceria depois que o TTL expirasse.
const cacheVersion = 3

const cacheRetention = 7 * 24 * time.Hour

type cacheFile struct {
	Version   int       `json:"version"`
	FetchedAt time.Time `json:"fetched_at"`
	Items     []Item    `json:"items"`
}

type SourceHealthStatus string

const (
	SourceOK          SourceHealthStatus = "ok"
	SourceFailed      SourceHealthStatus = "failed"
	SourceNeverLoaded SourceHealthStatus = "never_loaded"
)

// SourceHealth descreve a última saúde conhecida de uma fonte RSS.
type SourceHealth struct {
	SourceID    string             `json:"source_id"`
	SourceName  string             `json:"source_name"`
	Status      SourceHealthStatus `json:"status"`
	LastSuccess time.Time          `json:"last_success,omitempty"`
	LastErrorAt time.Time          `json:"last_error_at,omitempty"`
	LastError   string             `json:"last_error,omitempty"`
}

type sourceHealthFile struct {
	Sources map[string]SourceHealth `json:"sources"`
}

var sourceHealthMu sync.Mutex

// DefaultCache aponta para $XDG_CACHE_HOME/cnnbr.
func DefaultCache(ttl time.Duration) Cache {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return Cache{Dir: filepath.Join(dir, "cnnbr"), TTL: ttl}
}

func (c Cache) path(source, slug string) string {
	return filepath.Join(c.Dir, "feed-"+cacheKey(source)+"-"+cacheKey(slug)+".json")
}

func (c Cache) sourceHealthPath() string {
	return filepath.Join(c.Dir, "source-health.json")
}

// Load devolve os itens em cache de uma fonte e seção e quando foram buscados.
func (c Cache) Load(source, slug string) ([]Item, time.Time, error) {
	data, err := os.ReadFile(c.path(source, slug))
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
	return retainRecentItems(cf.Items, cf.FetchedAt, nowFn()), cf.FetchedAt, nil
}

// Save grava os itens de uma fonte e seção no cache, retendo no máximo sete
// dias de notícias.
func (c Cache) Save(source, slug string, items []Item, at time.Time) error {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(cacheFile{Version: cacheVersion, FetchedAt: at, Items: retainRecentItems(items, at, at)})
	if err != nil {
		return err
	}
	tmp := c.path(source, slug) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path(source, slug))
}

func cacheKey(value string) string {
	if value == "" {
		return "unknown"
	}
	return hex.EncodeToString([]byte(value))
}

func retainRecentItems(items []Item, fetchedAt, now time.Time) []Item {
	cutoff := now.Add(-cacheRetention)
	kept := make([]Item, 0, len(items))
	for _, item := range items {
		published := item.Published
		if published.IsZero() {
			published = fetchedAt
		}
		if !published.IsZero() && !published.Before(cutoff) {
			kept = append(kept, item)
		}
	}
	return kept
}

// LoadSourceHealth devolve a saúde conhecida das fontes, sem consultar a rede.
func (c Cache) LoadSourceHealth(sources []Source) []SourceHealth {
	sourceHealthMu.Lock()
	defer sourceHealthMu.Unlock()

	known := c.loadSourceHealthLocked()
	health := make([]SourceHealth, 0, len(sources))
	for _, source := range sources {
		key := source.key()
		h, ok := known[key]
		if !ok {
			h = SourceHealth{SourceID: key, SourceName: source.Name, Status: SourceNeverLoaded}
		}
		h.SourceID = key
		h.SourceName = source.Name
		if h.Status == "" {
			h.Status = SourceNeverLoaded
		}
		health = append(health, h)
	}
	return health
}

func (c Cache) recordSourceSuccess(source Source, at time.Time) SourceHealth {
	return c.recordSourceHealth(source, func(h *SourceHealth) {
		h.Status = SourceOK
		h.LastSuccess = at
	})
}

func (c Cache) recordSourceFailure(source Source, err error, at time.Time) SourceHealth {
	return c.recordSourceHealth(source, func(h *SourceHealth) {
		h.Status = SourceFailed
		h.LastErrorAt = at
		if err != nil {
			h.LastError = err.Error()
		}
	})
}

func (c Cache) ensureSourceSuccess(source Source, at time.Time) SourceHealth {
	return c.recordSourceHealth(source, func(h *SourceHealth) {
		if h.LastSuccess.IsZero() || h.LastSuccess.Before(at) {
			h.LastSuccess = at
		}
		if h.Status == SourceNeverLoaded || h.Status == "" {
			h.Status = SourceOK
		}
	})
}

func (c Cache) recordSourceHealth(source Source, update func(*SourceHealth)) SourceHealth {
	sourceHealthMu.Lock()
	defer sourceHealthMu.Unlock()

	known := c.loadSourceHealthLocked()
	key := source.key()
	h := known[key]
	h.SourceID = key
	h.SourceName = source.Name
	update(&h)
	known[key] = h
	_ = c.saveSourceHealthLocked(known)
	return h
}

func (c Cache) loadSourceHealthLocked() map[string]SourceHealth {
	data, err := os.ReadFile(c.sourceHealthPath())
	if err != nil {
		return map[string]SourceHealth{}
	}
	var doc sourceHealthFile
	if err := json.Unmarshal(data, &doc); err != nil || doc.Sources == nil {
		return map[string]SourceHealth{}
	}
	return doc.Sources
}

func (c Cache) saveSourceHealthLocked(sources map[string]SourceHealth) error {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(sourceHealthFile{Sources: sources})
	if err != nil {
		return err
	}
	tmp := c.sourceHealthPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.sourceHealthPath())
}

// Result é o desfecho de um Get: itens, quando foram buscados e se vieram do
// cache. Err traz o motivo de a rede ter falhado quando o cache salvou o dia.
type Result struct {
	Section   Section
	Items     []Item
	FetchedAt time.Time
	FromCache bool
	Err       error
	Health    []SourceHealth
}

// Get devolve o feed de uma seção, preferindo o cache enquanto ele estiver
// dentro do TTL. Com force=true a rede é sempre consultada, mas um erro cai de
// volta no cache.
func Get(ctx context.Context, client *http.Client, c Cache, s Section, pages int, force bool) Result {
	return GetSource(ctx, client, c, CNNBrasilSource, s, pages, force)
}

// GetSources carrega em paralelo as fontes de uma seção e mistura os itens em
// ordem de publicação. Uma fonte sem resposta não invalida as outras; o erro
// só chega ao chamador quando nenhuma fonte conseguiu devolver itens.
func GetSources(ctx context.Context, client *http.Client, c Cache, sources []Source, s Section, pages int, force bool) Result {
	if len(sources) == 0 {
		return Result{Section: s}
	}

	results := make(chan Result, len(sources))
	var wg sync.WaitGroup
	for _, source := range sources {
		wg.Add(1)
		go func(source Source) {
			defer wg.Done()
			results <- GetSource(ctx, client, c, source, s, pages, force)
		}(source)
	}
	wg.Wait()
	close(results)

	var (
		items     []Item
		health    []SourceHealth
		firstErr  error
		fetchedAt time.Time
		fromCache = true
	)
	for result := range results {
		items = append(items, result.Items...)
		health = append(health, result.Health...)
		if result.Err != nil && firstErr == nil {
			firstErr = result.Err
		}
		if result.FetchedAt.After(fetchedAt) {
			fetchedAt = result.FetchedAt
		}
		if !result.FromCache {
			fromCache = false
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Published.After(items[j].Published)
	})
	result := Result{Section: s, Items: items, FetchedAt: fetchedAt, FromCache: fromCache, Health: health}
	if len(items) == 0 {
		result.Err = firstErr
	}
	return result
}

// GetSource devolve o feed de uma fonte e seção, preferindo o cache enquanto
// ele estiver dentro do TTL.
func GetSource(ctx context.Context, client *http.Client, c Cache, source Source, s Section, pages int, force bool) Result {
	cached, fetchedAt, cacheErr := c.Load(source.key(), s.Slug)
	fresh := cacheErr == nil && len(cached) > 0 && time.Since(fetchedAt) < c.TTL

	if !force && fresh {
		health := c.ensureSourceSuccess(source, fetchedAt)
		return Result{Section: s, Items: cached, FetchedAt: fetchedAt, FromCache: true, Health: []SourceHealth{health}}
	}

	items, err := FetchSource(ctx, client, source, s.Cat, pages)
	if err != nil {
		health := c.recordSourceFailure(source, err, nowFn())
		if cacheErr == nil && len(cached) > 0 {
			return Result{Section: s, Items: cached, FetchedAt: fetchedAt, FromCache: true, Err: err, Health: []SourceHealth{health}}
		}
		return Result{Section: s, Err: err, Health: []SourceHealth{health}}
	}

	now := nowFn()
	health := c.recordSourceSuccess(source, now)
	// Falha ao gravar o cache não impede a leitura.
	_ = c.Save(source.key(), s.Slug, items, now)
	return Result{Section: s, Items: items, FetchedAt: now, Health: []SourceHealth{health}}
}

// errStaleCache marca um arquivo de cache gravado por uma versão anterior.
var errStaleCache = errors.New("cache de formato antigo")

// nowFn existe para permitir teste determinístico.
var nowFn = time.Now
