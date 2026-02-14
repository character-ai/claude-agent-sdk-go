package claudeagent

import (
	"testing"
)

func TestBM25IndexBasicSearch(t *testing.T) {
	idx := NewBM25Index()

	_ = idx.Index("web-search", "Search the web for information", []string{"web", "search"})
	_ = idx.Index("file-reader", "Read files from the filesystem", []string{"file", "read"})
	_ = idx.Index("code-writer", "Write and generate code", []string{"code", "write"})

	results := idx.Search("search web", 2)
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if results[0].ID != "web-search" {
		t.Fatalf("expected 'web-search' as top result, got %q", results[0].ID)
	}
}

func TestBM25IndexEmptyQuery(t *testing.T) {
	idx := NewBM25Index()
	_ = idx.Index("doc1", "some content", nil)

	results := idx.Search("", 5)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty query, got %d", len(results))
	}
}

func TestBM25IndexEmptyIndex(t *testing.T) {
	idx := NewBM25Index()

	results := idx.Search("anything", 5)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty index, got %d", len(results))
	}
}

func TestBM25IndexSingleDoc(t *testing.T) {
	idx := NewBM25Index()
	_ = idx.Index("only", "the only document in the index", nil)

	results := idx.Search("document", 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "only" {
		t.Fatalf("expected 'only', got %q", results[0].ID)
	}
}

func TestBM25IndexRemove(t *testing.T) {
	idx := NewBM25Index()
	_ = idx.Index("doc1", "hello world", nil)
	_ = idx.Index("doc2", "goodbye world", nil)

	_ = idx.Remove("doc1")

	results := idx.Search("hello", 5)
	if len(results) != 0 {
		t.Fatalf("expected 0 results after removing doc1, got %d", len(results))
	}

	results = idx.Search("world", 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'world' after removal, got %d", len(results))
	}
}

func TestBM25IndexUpdate(t *testing.T) {
	idx := NewBM25Index()
	_ = idx.Index("doc1", "alpha beta", nil)
	_ = idx.Index("doc1", "gamma delta", nil) // update

	results := idx.Search("alpha", 5)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for old content 'alpha', got %d", len(results))
	}

	results = idx.Search("gamma", 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for new content 'gamma', got %d", len(results))
	}
}

func TestBM25IndexKLimit(t *testing.T) {
	idx := NewBM25Index()
	_ = idx.Index("doc1", "shared term unique1", nil)
	_ = idx.Index("doc2", "shared term unique2", nil)
	_ = idx.Index("doc3", "shared term unique3", nil)

	results := idx.Search("shared term", 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results with k=2, got %d", len(results))
	}
}

func TestBM25IndexTagsSearchable(t *testing.T) {
	idx := NewBM25Index()
	_ = idx.Index("doc1", "some description", []string{"important", "critical"})
	_ = idx.Index("doc2", "other description", []string{"minor"})

	results := idx.Search("important", 5)
	if len(results) == 0 {
		t.Fatal("expected results when searching by tag")
	}
	if results[0].ID != "doc1" {
		t.Fatalf("expected doc1 as top result, got %q", results[0].ID)
	}
}

func TestBM25IndexNoMatchingTerms(t *testing.T) {
	idx := NewBM25Index()
	_ = idx.Index("doc1", "hello world", nil)

	results := idx.Search("completely unrelated", 5)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for non-matching query, got %d", len(results))
	}
}

func TestBM25IndexRebuild(t *testing.T) {
	idx := NewBM25Index()
	_ = idx.Index("doc1", "alpha beta", nil)
	_ = idx.Index("doc2", "beta gamma", nil)

	if err := idx.Rebuild(); err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}

	// Should still work after rebuild.
	results := idx.Search("alpha", 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result after rebuild, got %d", len(results))
	}
}

func TestBM25IndexZeroK(t *testing.T) {
	idx := NewBM25Index()
	_ = idx.Index("doc1", "hello", nil)

	results := idx.Search("hello", 0)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for k=0, got %d", len(results))
	}
}

func TestBM25IndexRemoveNonexistent(t *testing.T) {
	idx := NewBM25Index()
	// Should not panic or error.
	err := idx.Remove("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBM25IndexScoreOrdering(t *testing.T) {
	idx := NewBM25Index()
	// doc1 mentions "database" twice, doc2 mentions it once.
	_ = idx.Index("doc1", "database management database optimization", nil)
	_ = idx.Index("doc2", "database query", nil)
	_ = idx.Index("doc3", "unrelated content", nil)

	results := idx.Search("database", 3)
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	// doc1 should score higher due to more term occurrences.
	if results[0].ID != "doc1" {
		t.Fatalf("expected doc1 as top result, got %q", results[0].ID)
	}
	if results[0].Score <= results[1].Score {
		t.Fatal("expected first result to have higher score than second")
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"hello world", []string{"hello", "world"}},
		{"Hello, World!", []string{"hello", "world"}},
		{"search-the-web", []string{"search", "the", "web"}},
		{"file_reader_v2", []string{"file", "reader", "v2"}},
		{"", nil},
		{"  spaces  ", []string{"spaces"}},
	}

	for _, tt := range tests {
		got := tokenize(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("tokenize(%q) = %v, want %v", tt.input, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("tokenize(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}
