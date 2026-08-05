package feed

import (
	"sort"
	"strings"
	"time"
)

const HeadlinesSlug = "manchetes"

const headlineSimilarityThreshold = 0.30

// CoverageGroup é uma mesma pauta coberta por várias fontes. Items são índices
// para a lista original; as matérias não são fundidas nem alteradas.
type CoverageGroup struct {
	Title     string
	Items     []int
	Published time.Time
}

func (g CoverageGroup) SourceCount(items []Item) int {
	seen := map[string]bool{}
	for _, idx := range g.Items {
		if idx < 0 || idx >= len(items) {
			continue
		}
		seen[items[idx].IDSource()] = true
	}
	return len(seen)
}

// CoverageGroups agrupa pautas com pelo menos três fontes, considerando
// similaridade textual e janela de até 24 horas. Quando os feeds atuais não têm
// nenhuma pauta com três fontes, devolve os agrupamentos fortes com duas fontes
// para a seção não nascer vazia.
func CoverageGroups(items []Item, sources []Source) []CoverageGroup {
	var groups []CoverageGroup
	for i, item := range items {
		if item.Title == "" {
			continue
		}
		tokens := headlineTokens(item)
		if len(tokens) == 0 {
			continue
		}

		placed := false
		for g := range groups {
			if outsideWindow(item.Published, groups[g].Published) {
				continue
			}
			if !similarToGroup(tokens, groups[g], items) {
				continue
			}
			groups[g].Items = append(groups[g].Items, i)
			if item.Published.After(groups[g].Published) {
				groups[g].Published = item.Published
				groups[g].Title = item.Title
			}
			placed = true
			break
		}
		if !placed {
			groups = append(groups, CoverageGroup{Title: item.Title, Items: []int{i}, Published: item.Published})
		}
	}

	sourceRank := make(map[string]int, len(sources))
	for i, source := range sources {
		sourceRank[source.key()] = i
	}

	filtered := filterCoverageGroups(groups, items, sourceRank, 3)
	if len(filtered) == 0 {
		filtered = filterCoverageGroups(groups, items, sourceRank, 2)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Published.After(filtered[j].Published)
	})
	return filtered
}

func filterCoverageGroups(groups []CoverageGroup, items []Item, sourceRank map[string]int, minSources int) []CoverageGroup {
	filtered := make([]CoverageGroup, 0, len(groups))
	for _, group := range groups {
		group.Items = sortGroupItems(group.Items, items, sourceRank)
		if group.SourceCount(items) < minSources {
			continue
		}
		filtered = append(filtered, group)
	}
	return filtered
}

func (i Item) IDSource() string {
	if i.SourceID != "" {
		return i.SourceID
	}
	if i.Source != "" {
		return i.Source
	}
	return SourceCNNBrasilID
}

func outsideWindow(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	if a.After(b) {
		return a.Sub(b) > 24*time.Hour
	}
	return b.Sub(a) > 24*time.Hour
}

func similarToGroup(tokens map[string]bool, group CoverageGroup, items []Item) bool {
	for _, idx := range group.Items {
		if idx < 0 || idx >= len(items) {
			continue
		}
		if similarity(tokens, headlineTokens(items[idx])) >= headlineSimilarityThreshold {
			return true
		}
	}
	return false
}

func sortGroupItems(indices []int, items []Item, sourceRank map[string]int) []int {
	out := append([]int(nil), indices...)
	sort.SliceStable(out, func(i, j int) bool {
		leftItem, rightItem := items[out[i]], items[out[j]]
		left, lok := sourceRank[leftItem.IDSource()]
		right, rok := sourceRank[rightItem.IDSource()]
		if lok && rok && left != right {
			return left < right
		}
		if lok != rok {
			return lok
		}
		return leftItem.Published.After(rightItem.Published)
	})
	return out
}

func headlineTokens(item Item) map[string]bool {
	text := strings.ToLower(item.Title + " " + item.Summary)
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= 'á' && r <= 'ú' || r == 'ç')
	})
	tokens := map[string]bool{}
	for _, word := range words {
		word = slugify(word)
		if len(word) < 4 || headlineStopWords[word] {
			continue
		}
		tokens[word] = true
	}
	return tokens
}

func similarity(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for token := range a {
		if b[token] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

var headlineStopWords = map[string]bool{
	"para": true, "como": true, "apos": true, "sobre": true, "entre": true,
	"pela": true, "pelo": true, "mais": true, "esta": true,
	"este": true, "essa": true, "esse": true, "sera": true, "contra": true,
	"brasil": true,
}
