package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type KBStore struct {
	dir      string
	mu       sync.RWMutex
	docs     map[string]string
	combined string
}

func NewKBStore(dir string) (*KBStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create knowledge base directory: %w", err)
	}
	store := &KBStore{
		dir:  dir,
		docs: make(map[string]string),
	}
	if err := store.Reload(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *KBStore) Start(ctx context.Context, interval time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.Reload(); err != nil {
					logger.Warn("knowledge base reload failed", "error", err)
				}
			}
		}
	}()
}

func (s *KBStore) Reload() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("read knowledge base directory: %w", err)
	}

	docs := make(map[string]string)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return fmt.Errorf("read knowledge base file %s: %w", entry.Name(), err)
		}
		docs[entry.Name()] = string(content)
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	var builder strings.Builder
	for _, name := range names {
		builder.WriteString("## ")
		builder.WriteString(strings.TrimSuffix(name, filepath.Ext(name)))
		builder.WriteString("\n")
		builder.WriteString(strings.TrimSpace(docs[name]))
		builder.WriteString("\n\n")
	}

	s.mu.Lock()
	s.docs = docs
	s.combined = strings.TrimSpace(builder.String())
	s.mu.Unlock()

	knowledgeBaseDocuments.Set(float64(len(docs)))
	knowledgeBaseReloadUnix.SetToCurrentTime()
	return nil
}

func (s *KBStore) Snapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]string, len(s.docs))
	for name, content := range s.docs {
		result[name] = content
	}
	return result
}

func (s *KBStore) Search(keywords []string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.docs) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(normalizeText(keyword))
		if keyword != "" {
			normalized = append(normalized, keyword)
		}
	}

	names := make([]string, 0, len(s.docs))
	for name := range s.docs {
		names = append(names, name)
	}
	sort.Strings(names)

	matches := make([]string, 0, len(names))
	for _, name := range names {
		content := s.docs[name]
		combined := normalizeText(name + "\n" + content)
		if len(normalized) == 0 {
			matches = append(matches, summarizeDocument(name, content))
			continue
		}
		for _, keyword := range normalized {
			if strings.Contains(combined, keyword) {
				matches = append(matches, summarizeDocument(name, content))
				break
			}
		}
	}
	if len(matches) == 0 {
		return []string{summarizeDocument(names[0], s.docs[names[0]])}
	}
	if len(matches) > 3 {
		return matches[:3]
	}
	return matches
}

func summarizeDocument(name, content string) string {
	content = strings.TrimSpace(content)
	if len(content) > 700 {
		content = content[:700] + "..."
	}
	return fmt.Sprintf("%s\n%s", name, content)
}
