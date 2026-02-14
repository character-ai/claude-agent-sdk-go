package claudeagent

import (
	"math"
	"strings"
	"sync"
	"unicode"
)

// SkillIndex provides semantic/keyword lookup over skills and tools.
type SkillIndex interface {
	// Index adds a document (skill or tool description) to the index.
	Index(id string, text string, tags []string) error
	// Remove removes a document from the index.
	Remove(id string) error
	// Search returns top-k matching document IDs with scores.
	Search(query string, k int) []SearchResult
	// Rebuild re-indexes everything from scratch.
	Rebuild() error
}

// SearchResult holds a document ID and its relevance score.
type SearchResult struct {
	ID    string
	Score float64
}

// bm25Doc represents a tokenized document in the BM25 index.
type bm25Doc struct {
	id     string
	tokens []string
	tf     map[string]float64 // term -> frequency in this doc
	length int                // number of tokens
}

// BM25Index is a zero-dependency BM25 implementation for keyword search.
type BM25Index struct {
	docs   map[string]*bm25Doc // id -> doc
	df     map[string]int      // term -> number of docs containing term
	avgLen float64
	k1     float64
	b      float64
	mu     sync.RWMutex
}

// NewBM25Index creates a new BM25 index with standard parameters.
func NewBM25Index() *BM25Index {
	return &BM25Index{
		docs: make(map[string]*bm25Doc),
		df:   make(map[string]int),
		k1:   1.2,
		b:    0.75,
	}
}

// Index adds or updates a document in the index.
func (idx *BM25Index) Index(id string, text string, tags []string) error {
	// Combine text and tags into a single token stream.
	combined := text
	if len(tags) > 0 {
		combined += " " + strings.Join(tags, " ")
	}
	tokens := tokenize(combined)

	doc := &bm25Doc{
		id:     id,
		tokens: tokens,
		tf:     termFrequencies(tokens),
		length: len(tokens),
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// If updating, remove old DF contributions first.
	if old, exists := idx.docs[id]; exists {
		for term := range old.tf {
			idx.df[term]--
			if idx.df[term] <= 0 {
				delete(idx.df, term)
			}
		}
	}

	idx.docs[id] = doc

	// Update document frequencies.
	for term := range doc.tf {
		idx.df[term]++
	}

	// Recompute average document length.
	idx.recomputeAvgLen()

	return nil
}

// Remove removes a document from the index.
func (idx *BM25Index) Remove(id string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	doc, exists := idx.docs[id]
	if !exists {
		return nil
	}

	for term := range doc.tf {
		idx.df[term]--
		if idx.df[term] <= 0 {
			delete(idx.df, term)
		}
	}

	delete(idx.docs, id)
	idx.recomputeAvgLen()

	return nil
}

// Search returns the top-k documents matching the query, sorted by score descending.
func (idx *BM25Index) Search(query string, k int) []SearchResult {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 || k <= 0 {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	n := float64(len(idx.docs))
	if n == 0 {
		return nil
	}

	type scored struct {
		id    string
		score float64
	}

	var results []scored

	for _, doc := range idx.docs {
		score := 0.0
		for _, term := range queryTokens {
			tf := doc.tf[term]
			if tf == 0 {
				continue
			}
			dfVal := float64(idx.df[term])

			// IDF: log((N - df + 0.5) / (df + 0.5) + 1)
			idf := math.Log((n-dfVal+0.5)/(dfVal+0.5) + 1)

			// BM25 score for this term
			numerator := tf * (idx.k1 + 1)
			denominator := tf + idx.k1*(1-idx.b+idx.b*float64(doc.length)/idx.avgLen)
			score += idf * (numerator / denominator)
		}
		if score > 0 {
			results = append(results, scored{id: doc.id, score: score})
		}
	}

	// Sort by score descending (insertion sort for small k).
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].score > results[j-1].score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}

	if k > len(results) {
		k = len(results)
	}

	out := make([]SearchResult, k)
	for i := 0; i < k; i++ {
		out[i] = SearchResult{ID: results[i].id, Score: results[i].score}
	}
	return out
}

// Rebuild clears and rebuilds the index. This is a no-op for BM25Index
// since re-indexing all docs would require external data. It recomputes
// internal statistics.
func (idx *BM25Index) Rebuild() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Recompute all DFs from scratch.
	idx.df = make(map[string]int)
	for _, doc := range idx.docs {
		for term := range doc.tf {
			idx.df[term]++
		}
	}
	idx.recomputeAvgLen()
	return nil
}

// recomputeAvgLen recalculates the average document length. Must be called with lock held.
func (idx *BM25Index) recomputeAvgLen() {
	if len(idx.docs) == 0 {
		idx.avgLen = 0
		return
	}
	total := 0
	for _, doc := range idx.docs {
		total += doc.length
	}
	idx.avgLen = float64(total) / float64(len(idx.docs))
}

// tokenize splits text into lowercase tokens, splitting on whitespace and punctuation.
func tokenize(text string) []string {
	lower := strings.ToLower(text)
	var tokens []string
	var current strings.Builder

	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// termFrequencies computes term frequency for each token.
func termFrequencies(tokens []string) map[string]float64 {
	counts := make(map[string]int)
	for _, t := range tokens {
		counts[t]++
	}
	tf := make(map[string]float64, len(counts))
	for term, count := range counts {
		tf[term] = float64(count)
	}
	return tf
}
