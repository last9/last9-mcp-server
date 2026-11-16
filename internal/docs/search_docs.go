package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	IndexURL      = "https://last9.io/docs-index.json"
	maxIndexBytes = 16 << 20
)

type Page struct {
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Headings    []string  `json:"headings"`
	Sections    []Section `json:"sections"`
}

type Section struct {
	Heading string `json:"heading"`
	Anchor  string `json:"anchor"`
	Text    string `json:"text"`
}

type Index struct {
	GeneratedAt string `json:"generated_at"`
	Pages       []Page `json:"pages"`
}

type Hit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Excerpt string `json:"excerpt"`
}

type SearchDocsArgs struct {
	Query string `json:"query" jsonschema:"(Required) Search terms; must not be empty"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum pages to return; defaults to 5 and is clamped to 1..5"`
}

func NewSearchDocsHandler(fetcher *Fetcher) func(context.Context, *mcp.CallToolRequest, SearchDocsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args SearchDocsArgs) (*mcp.CallToolResult, any, error) {
		limit := args.Limit
		if limit == 0 {
			limit = 5
		}
		hits, err := fetcher.Search(ctx, args.Query, limit)
		if err != nil {
			return nil, nil, err
		}
		if len(hits) == 0 {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "no matching Last9 documentation page"}}}, nil, nil
		}
		body, err := json.Marshal(struct {
			Results []Hit `json:"results"`
		}{Results: hits})
		if err != nil {
			return nil, nil, fmt.Errorf("encode docs search results: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	}
}

// ponytail: in-memory only; embed a snapshot if egress from restricted pods needs it
type Fetcher struct {
	client *http.Client
	url    string
	ttl    time.Duration

	mu         sync.Mutex
	index      *Index
	etag       string
	fetchedAt  time.Time
	refreshing chan struct{}
}

func NewFetcher(client *http.Client, url string) *Fetcher {
	c := *client
	c.Timeout = 10 * time.Second
	return &Fetcher{client: &c, url: url, ttl: time.Hour}
}

func (f *Fetcher) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query must not be empty")
	}
	if limit < 1 {
		limit = 1
	} else if limit > 5 {
		limit = 5
	}
	index, err := f.load(ctx)
	if err != nil {
		return nil, err
	}
	return search(index, query, limit), nil
}

func (f *Fetcher) load(ctx context.Context) (*Index, error) {
	f.mu.Lock()
	if f.index != nil && utils.Now().Sub(f.fetchedAt) < f.ttl {
		index := f.index
		f.mu.Unlock()
		return index, nil
	}
	if wait := f.refreshing; wait != nil {
		f.mu.Unlock()
		select {
		case <-wait:
			return f.load(ctx)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.refreshing = make(chan struct{})
	etag := f.etag
	f.mu.Unlock()

	index, nextETag, notModified, err := f.fetch(ctx, etag)
	f.mu.Lock()
	defer f.mu.Unlock()
	defer close(f.refreshing)
	f.refreshing = nil
	if err == nil {
		if notModified && f.index == nil {
			err = fmt.Errorf("fetch docs index: HTTP 304 before an index was loaded")
		} else if !notModified {
			f.index = index
			f.etag = nextETag
		}
		if err == nil {
			f.fetchedAt = utils.Now()
		}
	}
	if f.index != nil {
		return f.index, nil
	}
	return nil, err
}

func (f *Fetcher) fetch(ctx context.Context, etag string) (*Index, string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return nil, "", false, err
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, "", false, fmt.Errorf("fetch docs index: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil, etag, true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", false, fmt.Errorf("fetch docs index: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxIndexBytes {
		return nil, "", false, fmt.Errorf("fetch docs index: response exceeds %d bytes", maxIndexBytes)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexBytes+1))
	if err != nil {
		return nil, "", false, fmt.Errorf("read docs index: %w", err)
	}
	if len(body) > maxIndexBytes {
		return nil, "", false, fmt.Errorf("fetch docs index: response exceeds %d bytes", maxIndexBytes)
	}
	var index Index
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, "", false, fmt.Errorf("decode docs index: %w", err)
	}
	return &index, resp.Header.Get("ETag"), false, nil
}

type scoredHit struct {
	Hit
	score float64
}

func search(index *Index, query string, limit int) []Hit {
	terms := expand(tokenize(query))
	type document struct {
		page    Page
		section Section
		terms   []string
	}
	var docs []document
	for _, page := range index.Pages {
		for _, section := range page.Sections {
			weighted := strings.Repeat(page.Title+" ", 3) + strings.Repeat(section.Heading+" ", 2) + page.Description + " " + section.Text
			docs = append(docs, document{page: page, section: section, terms: tokenize(weighted)})
		}
	}
	if len(docs) == 0 {
		return nil
	}
	df := make(map[string]int)
	totalLength := 0
	for _, doc := range docs {
		totalLength += len(doc.terms)
		seen := make(map[string]bool)
		for _, term := range doc.terms {
			if !seen[term] {
				df[term]++
				seen[term] = true
			}
		}
	}
	avgLength := float64(totalLength) / float64(len(docs))
	best := make(map[string]scoredHit)
	for _, doc := range docs {
		counts := make(map[string]int)
		for _, term := range doc.terms {
			counts[term]++
		}
		score := 0.0
		for _, term := range terms {
			tf := float64(counts[term])
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (float64(len(docs)-df[term])+0.5)/(float64(df[term])+0.5))
			score += idf * tf * 2.2 / (tf + 1.2*(0.25+0.75*float64(len(doc.terms))/avgLength))
		}
		if score == 0 {
			continue
		}
		url := doc.page.URL
		if doc.section.Anchor != "" {
			url += "#" + doc.section.Anchor
		}
		hit := scoredHit{Hit: Hit{Title: doc.page.Title, URL: url, Excerpt: excerpt(doc.section.Text)}, score: score}
		if old, ok := best[doc.page.URL]; !ok || hit.score > old.score {
			best[doc.page.URL] = hit
		}
	}
	ranked := make([]scoredHit, 0, len(best))
	for _, hit := range best {
		ranked = append(ranked, hit)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].URL < ranked[j].URL
		}
		return ranked[i].score > ranked[j].score
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	hits := make([]Hit, len(ranked))
	for i := range ranked {
		hits[i] = ranked[i].Hit
	}
	return hits
}

func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func expand(terms []string) []string {
	synonyms := map[string][]string{
		"k8s": {"kubernetes"}, "kubernetes": {"k8s"},
		"otel": {"opentelemetry"}, "otlp": {"opentelemetry"},
		"promql": {"prometheus"}, "pods": {"pod"},
	}
	out := append([]string(nil), terms...)
	for _, term := range terms {
		out = append(out, synonyms[term]...)
	}
	return out
}

func excerpt(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= 800 {
		return text
	}
	cut := strings.LastIndex(string(runes[:801]), " ")
	if cut <= 0 {
		return string(runes[:800])
	}
	return string(runes)[:cut]
}
