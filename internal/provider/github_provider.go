package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"go.uber.org/zap"
)

// GitHubProvider downloads logos from GitHub repos that store stock ticker icons.
// Supports repos like davidepalazzo/ticker-logos and nvstly/icons which store
// PNGs at ticker_icons/{SYMBOL}.png.
type GitHubProvider struct {
	repos  []string // e.g., ["davidepalazzo/ticker-logos", "nvstly/icons"]
	client *http.Client
	logger *zap.Logger
}

// NewGitHubProvider creates a provider for the given GitHub repos.
func NewGitHubProvider(repos []string, logger *zap.Logger) *GitHubProvider {
	return &GitHubProvider{
		repos: repos,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

func (g *GitHubProvider) Name() string {
	return "github"
}

// githubTreeEntry represents a single file in a GitHub git tree response.
// The GitHub API returns a flat list of all files when recursive=1.
type githubTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" for files, "tree" for directories
	URL  string `json:"url"`  // API URL (not raw content)
	Size int    `json:"size"`
}

type githubTreeResponse struct {
	SHA       string            `json:"sha"`
	Tree      []githubTreeEntry `json:"tree"`
	Truncated bool              `json:"truncated"`
}

// GetLogo downloads a single logo from the first repo that has it.
func (g *GitHubProvider) GetLogo(ctx context.Context, symbol string) (*LogoResult, error) {
	symbol = strings.ToUpper(symbol)

	for _, repo := range g.repos {
		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/main/ticker_icons/%s.png", repo, symbol)

		data, err := g.downloadFile(ctx, rawURL)
		if err != nil {
			g.logger.Debug("logo not found in repo",
				zap.String("repo", repo),
				zap.String("symbol", symbol),
				zap.Error(err),
			)
			continue
		}

		return &LogoResult{
			Symbol:      symbol,
			ImageData:   data,
			Source:      "github:" + repo,
			OriginalURL: rawURL,
		}, nil
	}

	return nil, fmt.Errorf("logo for %s not found in any GitHub repo", symbol)
}

// BulkImport iterates over all ticker_icons in each repo and calls the callback
// for each logo found. Uses the GitHub Git Trees API to list files efficiently
// (one API call per repo instead of paginated directory listings).
//
// The callback pattern is a common Go idiom for processing large datasets —
// instead of returning a huge slice, you call a function for each item.
// This keeps memory usage constant regardless of how many logos exist.
func (g *GitHubProvider) BulkImport(ctx context.Context, callback func(result *LogoResult) error) (*ImportStats, error) {
	stats := &ImportStats{}

	for _, repo := range g.repos {
		g.logger.Info("importing from GitHub repo", zap.String("repo", repo))

		repoStats, err := g.importFromRepo(ctx, repo, callback)
		if err != nil {
			g.logger.Error("repo import failed", zap.String("repo", repo), zap.Error(err))
			stats.Errors = append(stats.Errors, fmt.Sprintf("%s: %v", repo, err))
			continue
		}

		stats.Total += repoStats.Total
		stats.Imported += repoStats.Imported
		stats.Skipped += repoStats.Skipped
		stats.Failed += repoStats.Failed
		stats.Errors = append(stats.Errors, repoStats.Errors...)
	}

	return stats, nil
}

func (g *GitHubProvider) importFromRepo(ctx context.Context, repo string, callback func(result *LogoResult) error) (*ImportStats, error) {
	stats := &ImportStats{}

	// Use the Git Trees API to list all files in one request.
	// recursive=1 returns every file in the repo as a flat list.
	treeURL := fmt.Sprintf("https://api.github.com/repos/%s/git/trees/main?recursive=1", repo)
	entries, err := g.fetchTree(ctx, treeURL)
	if err != nil {
		return stats, fmt.Errorf("fetching tree: %w", err)
	}

	// Filter to ticker_icons/*.png files
	for _, entry := range entries {
		if entry.Type != "blob" {
			continue
		}
		if !strings.HasPrefix(entry.Path, "ticker_icons/") {
			continue
		}
		if !strings.HasSuffix(entry.Path, ".png") {
			continue
		}

		// Extract symbol from path: "ticker_icons/AAPL.png" → "AAPL"
		filename := path.Base(entry.Path)
		symbol := strings.TrimSuffix(filename, ".png")
		symbol = strings.ToUpper(symbol)

		stats.Total++

		// Check for context cancellation (allows graceful shutdown during import)
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		// Download the raw file
		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/main/%s", repo, entry.Path)
		data, err := g.downloadFile(ctx, rawURL)
		if err != nil {
			stats.Failed++
			stats.Errors = append(stats.Errors, fmt.Sprintf("%s: download failed: %v", symbol, err))
			continue
		}

		result := &LogoResult{
			Symbol:      symbol,
			ImageData:   data,
			Source:      "github:" + repo,
			OriginalURL: rawURL,
		}

		if err := callback(result); err != nil {
			// If callback returns an error with "already exists", count as skipped
			if strings.Contains(err.Error(), "already exists") {
				stats.Skipped++
			} else {
				stats.Failed++
				stats.Errors = append(stats.Errors, fmt.Sprintf("%s: %v", symbol, err))
			}
			continue
		}

		stats.Imported++

		// Log progress every 100 logos
		if stats.Imported%100 == 0 {
			g.logger.Info("import progress",
				zap.String("repo", repo),
				zap.Int("imported", stats.Imported),
				zap.Int("total_seen", stats.Total),
			)
		}
	}

	g.logger.Info("repo import complete",
		zap.String("repo", repo),
		zap.Int("total", stats.Total),
		zap.Int("imported", stats.Imported),
		zap.Int("skipped", stats.Skipped),
		zap.Int("failed", stats.Failed),
	)

	return stats, nil
}

func (g *GitHubProvider) fetchTree(ctx context.Context, url string) ([]githubTreeEntry, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "logo-service/1.0")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching tree: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var tree githubTreeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, fmt.Errorf("decoding tree: %w", err)
	}

	if tree.Truncated {
		g.logger.Warn("GitHub tree response was truncated — some files may be missing")
	}

	return tree.Tree, nil
}

func (g *GitHubProvider) downloadFile(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "logo-service/1.0")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	// Limit read to 10MB to prevent memory issues from unexpectedly large files.
	// io.LimitReader wraps a reader with a max byte count — a common safety pattern.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	return data, nil
}
