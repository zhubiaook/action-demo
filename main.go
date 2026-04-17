package main

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	sitemapURL = "https://www.anthropic.com/sitemap.xml"
	reportsDir = "reports"
)

// allowedCategories defines the topics we care about
var allowedCategories = map[string]bool{
	"news":        true,
	"research":    true,
	"engineering": true,
	"learn":       true,
}

// URLSet represents the root element of a sitemap XML
type URLSet struct {
	XMLName xml.Name `xml:"urlset"`
	URLs    []URL    `xml:"url"`
}

// URL represents a single URL entry in the sitemap
type URL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
}

// Article holds parsed article info
type Article struct {
	URL      string
	LastMod  time.Time
	Category string
	Slug     string
}

func main() {
	fmt.Println("Fetching sitemap from", sitemapURL)

	urlset, err := fetchSitemap(sitemapURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching sitemap: %v\n", err)
		os.Exit(1)
	}

	now := time.Now().UTC()
	oneWeekAgo := now.AddDate(0, 0, -7)

	fmt.Printf("Filtering articles updated after: %s\n", oneWeekAgo.Format(time.RFC3339))

	articles := filterRecentArticles(urlset.URLs, oneWeekAgo)

	fmt.Printf("Found %d articles updated in the last 7 days\n", len(articles))

	if err := os.MkdirAll(reportsDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating reports directory: %v\n", err)
		os.Exit(1)
	}

	reportFile := filepath.Join(reportsDir, fmt.Sprintf("anthropic-weekly-%s.md", now.Format("2006-01-02")))
	if err := writeMarkdownReport(reportFile, articles, now, oneWeekAgo); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Report written to: %s\n", reportFile)
}

// fetchSitemap downloads and parses the sitemap XML
func fetchSitemap(url string) (*URLSet, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var urlset URLSet
	if err := xml.NewDecoder(resp.Body).Decode(&urlset); err != nil {
		return nil, fmt.Errorf("XML decode failed: %w", err)
	}

	return &urlset, nil
}

// filterRecentArticles filters URLs updated after the cutoff time.
// Only URLs under allowed categories WITH a sub-path are included
// (e.g. /learn/xxx is included, but /learn alone is not).
func filterRecentArticles(urls []URL, cutoff time.Time) []Article {
	var articles []Article

	for _, u := range urls {
		// Must belong to one of the allowed categories and have a sub-path
		cat, hasSubPath := extractCategoryAndSubPath(u.Loc)
		if !allowedCategories[cat] || !hasSubPath {
			continue
		}

		if u.LastMod == "" {
			continue
		}

		lastMod, err := time.Parse(time.RFC3339, strings.TrimSpace(u.LastMod))
		if err != nil {
			// Try alternative format
			lastMod, err = time.Parse("2006-01-02T15:04:05.000Z", strings.TrimSpace(u.LastMod))
			if err != nil {
				continue
			}
		}

		if lastMod.After(cutoff) {
			articles = append(articles, Article{
				URL:      u.Loc,
				LastMod:  lastMod,
				Category: cat,
				Slug:     extractSlug(u.Loc),
			})
		}
	}

	// Sort by LastMod descending (newest first)
	sort.Slice(articles, func(i, j int) bool {
		return articles[i].LastMod.After(articles[j].LastMod)
	})

	return articles
}

// extractCategoryAndSubPath returns the category and whether the URL has a
// non-empty sub-path beneath it.
// e.g. https://www.anthropic.com/news/my-post -> ("news", true)
//
//	https://www.anthropic.com/news            -> ("news", false)
func extractCategoryAndSubPath(rawURL string) (category string, hasSubPath bool) {
	path := strings.TrimPrefix(rawURL, "https://www.anthropic.com")
	path = strings.TrimPrefix(path, "http://www.anthropic.com")
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")

	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", false
	}
	category = parts[0]
	hasSubPath = len(parts) == 2 && parts[1] != ""
	return
}

// extractSlug extracts a human-readable name from the URL
func extractSlug(rawURL string) string {
	parts := strings.Split(strings.TrimRight(rawURL, "/"), "/")
	if len(parts) == 0 {
		return rawURL
	}
	slug := parts[len(parts)-1]
	// Convert dashes to spaces and title-case
	words := strings.Split(slug, "-")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// writeMarkdownReport writes the filtered articles to a Markdown file
func writeMarkdownReport(path string, articles []Article, now, cutoff time.Time) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Group articles by category
	grouped := make(map[string][]Article)
	var categories []string
	for _, a := range articles {
		if _, exists := grouped[a.Category]; !exists {
			categories = append(categories, a.Category)
		}
		grouped[a.Category] = append(grouped[a.Category], a)
	}
	sort.Strings(categories)

	fmt.Fprintf(f, "# Anthropic Weekly Update Report\n\n")
	fmt.Fprintf(f, "> **Generated:** %s  \n", now.Format(time.DateTime))
	fmt.Fprintf(f, "> **Period:** %s -- %s\n", cutoff.Format(time.DateOnly), now.Format(time.DateOnly))

	if len(articles) == 0 {
		fmt.Fprintf(f, "_No pages were updated in the last 7 days._\n")
		return nil
	}

	fmt.Fprintf(f, "> **Total pages:** %d\n\n", len(articles))
	fmt.Fprintf(f, "---\n\n")

	for _, cat := range categories {
		arts := grouped[cat]
		title := strings.ToUpper(cat[:1]) + cat[1:]
		fmt.Fprintf(f, "## %s\n\n", title)
		fmt.Fprintf(f, "| Title | URL | Last Updated |\n")
		fmt.Fprintf(f, "|-------|-----|-------------|\n")
		for _, a := range arts {
			title := a.Slug
			fmt.Fprintf(f, "| %s | [link](%s) | %s |\n",
				title,
				a.URL,
				a.LastMod.Format("2006-01-02 15:04 UTC"),
			)
		}
		fmt.Fprintf(f, "\n")
	}

	fmt.Fprintf(f, "---\n\n")
	fmt.Fprintf(f, "_Source: [anthropic.com/sitemap.xml](%s)_\n", sitemapURL)

	return nil
}
