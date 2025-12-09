// main.go
package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type Article struct {
	Title   string `json:"title"`
	Author  string `json:"author,omitempty"`
	Content string `json:"content"`
}

var (
	// simple in-memory API keys (for MVP). Replace by DB later
	validKeys = map[string]bool{
		"demo-key-123": true,
	}

	// rate limiting: key -> (count, resetTime)
	rateMu sync.Mutex
	limits = map[string]int{}
	maxFreeRequestsPerHour = 100
)

func main() {
	http.HandleFunc("/extract", extractHandler)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request){
		w.Write([]byte("Clean Article Extractor — ready"))
	})

	// reset limits every hour
	go func() {
		for {
			time.Sleep(time.Until(time.Now().Truncate(time.Hour).Add(time.Hour)))
			rateMu.Lock()
			limits = map[string]int{}
			rateMu.Unlock()
		}
	}()

	addr := ":8080"
	log.Println("listening on", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func extractHandler(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		apiKey = r.URL.Query().Get("key")
	}
	if !checkKey(apiKey) {
		http.Error(w, "invalid api key", http.StatusUnauthorized)
		return
	}

	if !checkRateLimit(apiKey) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	url := r.URL.Query().Get("url")
	if url == "" {
		http.Error(w, "missing url param", http.StatusBadRequest)
		return
	}

	art, err := fetchAndExtract(url)
	if err != nil {
		http.Error(w, "failed to extract: "+err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, art)
}

func checkKey(k string) bool {
	// In MVP accept empty key as demo limited access
	if k == "" {
		return true
	}
	_, ok := validKeys[k]
	return ok
}

func checkRateLimit(k string) bool {
	key := k
	if key == "" {
		key = "anon"
	}
	rateMu.Lock()
	defer rateMu.Unlock()
	c := limits[key]
	if c >= maxFreeRequestsPerHour {
		return false
	}
	limits[key] = c + 1
	return true
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func fetchAndExtract(url string) (*Article, error) {
	// basic validation
	if !strings.HasPrefix(url, "http") {
		url = "http://" + url
	}

	// simple HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	req, _ := http.NewRequest("GET", url, nil)
	// user-agent
	req.Header.Set("User-Agent", "clean-article-extractor/1.0 (+https://example.com)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, errors.New("bad upstream status")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5_000_000)) // limit 5MB
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	title := extractTitle(doc)
	author := extractAuthor(doc)
	content := extractMainText(doc)

	// fallback: if no content
	if strings.TrimSpace(content) == "" {
		return nil, errors.New("no article content detected")
	}

	return &Article{
		Title:   title,
		Author:  author,
		Content: content,
	}, nil
}

func extractTitle(doc *goquery.Document) string {
	if t := strings.TrimSpace(doc.Find("meta[property='og:title']").AttrOr("content", "")); t != "" {
		return t
	}
	if t := strings.TrimSpace(doc.Find("title").Text()); t != "" {
		return t
	}
	return ""
}

func extractAuthor(doc *goquery.Document) string {
	if a := strings.TrimSpace(doc.Find("meta[name='author']").AttrOr("content", "")); a != "" {
		return a
	}
	// try common selectors
	sels := []string{".author", ".byline", "[rel=author]"}
	for _, s := range sels {
		if a := strings.TrimSpace(doc.Find(s).First().Text()); a != "" {
			return a
		}
	}
	return ""
}

func extractMainText(doc *goquery.Document) string {
	// Priority: <article>, main, div[id*=content] / div[class*=content], .post, .entry-content
	candidates := []string{"article", "main", "div[id*='content']", "div[class*='content']", ".post", ".entry-content", ".article-body", ".post-body"}
	best := ""
	bestLen := 0
	for _, sel := range candidates {
		selection := doc.Find(sel)
		if selection.Length() == 0 {
			continue
		}
		text := gatherText(selection)
		l := len(strings.TrimSpace(text))
		if l > bestLen {
			best = text
			bestLen = l
		}
	}
	// if none found, take the largest <div>
	if bestLen == 0 {
		doc.Find("div").Each(func(i int, s *goquery.Selection) {
			t := gatherText(s)
			if len(strings.TrimSpace(t)) > bestLen {
				best = t
				bestLen = len(strings.TrimSpace(t))
			}
		})
	}
	// final fallback: all <p>
	if bestLen == 0 {
		best = gatherText(doc.Selection)
	}
	// clean repeated whitespace
	return cleanText(best)
}

func gatherText(s *goquery.Selection) string {
	var parts []string
	s.Find("p, h1, h2, h3").Each(func(i int, p *goquery.Selection) {
		t := strings.TrimSpace(p.Text())
		if t != "" {
			parts = append(parts, t)
		}
	})
	return strings.Join(parts, "\n\n")
}

func cleanText(s string) string {
	// basic cleaning
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSpace(s)
	// collapse multiple newlines
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}
