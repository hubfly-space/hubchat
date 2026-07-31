package portability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	filemodule "github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/knowledgebase"
)

const maxMarkdownImportBytes = 100 << 20

type markdownImportArticle struct {
	KnowledgeBaseID string
	CollectionID    *string
	Title           string
	Slug            string
	Excerpt         string
	Body            string
	State           string
	Language        string
}

func validMarkdownFile(record *filemodule.Record) bool {
	if record == nil || record.SizeBytes <= 0 || record.SizeBytes > maxMarkdownImportBytes {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(record.Name))
	mime := strings.ToLower(strings.TrimSpace(record.MIMEType))
	return strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".markdown") || mime == "text/markdown" || mime == "text/plain"
}

func (s *Service) readMarkdownFile(ctx context.Context, workspaceID string, fileID *string) (*markdownImportArticle, error) {
	if fileID == nil || s.files == nil {
		return nil, errors.New("Markdown import file is missing")
	}
	record, opened, err := s.files.Open(ctx, workspaceID, *fileID)
	if err != nil {
		return nil, fmt.Errorf("open Markdown: %w", err)
	}
	defer opened.Close()
	if !validMarkdownFile(record) {
		return nil, errors.New("Markdown import file is not valid")
	}
	body, err := io.ReadAll(io.LimitReader(opened, maxMarkdownImportBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Markdown: %w", err)
	}
	if len(body) > maxMarkdownImportBytes {
		return nil, errors.New("Markdown import exceeds the 100 MiB limit")
	}
	return parseMarkdownImport(string(body))
}

func parseMarkdownImport(source string) (*markdownImportArticle, error) {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	lines := strings.Split(source, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return nil, errors.New("Markdown import requires YAML front matter")
	}
	end := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" || strings.TrimSpace(lines[index]) == "..." {
			end = index
			break
		}
	}
	if end < 0 {
		return nil, errors.New("Markdown front matter is not closed")
	}
	fields := make(map[string]string)
	for _, line := range lines[1:end] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid Markdown front matter line %q", line)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		fields[key] = value
	}
	body := strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	title := strings.TrimSpace(fields["title"])
	if title == "" {
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "# ") {
				title = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "# "))
				break
			}
		}
	}
	if title == "" {
		return nil, errors.New("Markdown import requires a title")
	}
	knowledgeBaseID := strings.TrimSpace(fields["knowledge_base_id"])
	if knowledgeBaseID == "" {
		return nil, errors.New("Markdown import requires knowledge_base_id")
	}
	slug := strings.TrimSpace(fields["slug"])
	if slug == "" {
		slug = markdownSlug(title)
	}
	if slug == "" {
		return nil, errors.New("Markdown import could not derive a slug")
	}
	language := strings.TrimSpace(fields["language"])
	if language == "" {
		language = "en"
	}
	state := strings.TrimSpace(fields["state"])
	if state == "" {
		state = "draft"
	}
	excerpt := strings.TrimSpace(fields["excerpt"])
	if excerpt == "" {
		excerpt = markdownExcerpt(body)
	}
	var collectionID *string
	if value := strings.TrimSpace(fields["collection_id"]); value != "" {
		collectionID = &value
	}
	return &markdownImportArticle{
		KnowledgeBaseID: knowledgeBaseID, CollectionID: collectionID, Title: title,
		Slug: slug, Excerpt: excerpt, Body: body, State: state, Language: language,
	}, nil
}

func markdownSlug(value string) string {
	var builder strings.Builder
	separator := false
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			if separator && builder.Len() > 0 {
				builder.WriteByte('-')
			}
			builder.WriteRune(character)
			separator = false
			continue
		}
		separator = true
	}
	return strings.Trim(builder.String(), "-")
}

func markdownExcerpt(body string) string {
	for _, paragraph := range strings.Split(body, "\n\n") {
		paragraph = strings.TrimSpace(paragraph)
		paragraph = strings.TrimLeft(paragraph, "# ")
		if paragraph != "" {
			return paragraph
		}
	}
	return ""
}

func (s *Service) previewMarkdownImport(ctx context.Context, workspaceID string, request *Request) ([]TableSummary, error) {
	if s.knowledgebase == nil {
		return nil, errors.New("portability: knowledge-base import service is unavailable")
	}
	article, err := s.readMarkdownFile(ctx, workspaceID, request.FileID)
	if err != nil {
		return nil, err
	}
	if _, err := s.knowledgebase.GetKnowledgeBase(ctx, workspaceID, article.KnowledgeBaseID); err != nil {
		return nil, err
	}
	existing := 0
	if _, err := s.knowledgebase.FindArticleBySlug(ctx, workspaceID, article.KnowledgeBaseID, article.Language, article.Slug); err == nil {
		existing = 1
	} else if !errors.Is(err, knowledgebase.ErrNotFound) {
		return nil, err
	}
	return []TableSummary{{Name: "articles", Rows: 1, Existing: existing, New: 1 - existing}}, nil
}

func (s *Service) runMarkdownImport(ctx context.Context, id string, request *Request) error {
	if s.knowledgebase == nil {
		return s.failImport(ctx, id, errors.New("portability: knowledge-base import service is unavailable"))
	}
	article, err := s.readMarkdownFile(ctx, request.WorkspaceID, request.FileID)
	if err != nil {
		return s.failImport(ctx, id, err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE import_requests SET total_rows=1 WHERE id=$1`, id); err != nil {
		return err
	}
	actorID := ""
	if request.RequestedBy != nil {
		actorID = *request.RequestedBy
	}
	existing, findErr := s.knowledgebase.FindArticleBySlug(ctx, request.WorkspaceID, article.KnowledgeBaseID, article.Language, article.Slug)
	if findErr != nil && !errors.Is(findErr, knowledgebase.ErrNotFound) {
		return s.failImport(ctx, id, findErr)
	}
	articleID := ""
	if existing != nil {
		articleID = existing.ID
	}
	_, err = s.knowledgebase.SaveArticle(ctx, request.WorkspaceID, actorID, articleID, knowledgebase.ArticleInput{
		KnowledgeBaseID: article.KnowledgeBaseID, CollectionID: article.CollectionID,
		Title: article.Title, Slug: article.Slug, Excerpt: article.Excerpt, Body: article.Body,
		State: article.State, Language: article.Language,
	})
	if err != nil {
		return s.failImport(ctx, id, err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE import_request_progress SET table_index=1,row_index=0,updated_at=now() WHERE import_id=$1`, id); err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE import_requests SET state='completed',processed_rows=1,failed_rows=0,errors='[]'::jsonb,completed_at=now() WHERE id=$1`, id)
	return err
}
