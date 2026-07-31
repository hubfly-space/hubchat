package portability

import (
	"strings"
	"testing"
)

func TestParseMarkdownImportFrontMatterAndDerivedFields(t *testing.T) {
	article, err := parseMarkdownImport("---\nknowledge_base_id: kb_1\nstate: draft\n---\n# Getting Started\n\nUse Hubchat to help customers.\n")
	if err != nil {
		t.Fatalf("parse Markdown returned error: %v", err)
	}
	if article.Title != "Getting Started" || article.Slug != "getting-started" || article.Excerpt != "Getting Started" || article.KnowledgeBaseID != "kb_1" {
		t.Fatalf("unexpected Markdown article: %+v", article)
	}
	if !strings.Contains(article.Body, "Use Hubchat") {
		t.Fatalf("body did not preserve Markdown: %q", article.Body)
	}
}

func TestParseMarkdownImportRequiresTargetAndFrontMatter(t *testing.T) {
	if _, err := parseMarkdownImport("# No front matter\n"); err == nil {
		t.Fatal("Markdown without front matter was accepted")
	}
	if _, err := parseMarkdownImport("---\ntitle: Missing target\n---\nBody\n"); err == nil || !strings.Contains(err.Error(), "knowledge_base_id") {
		t.Fatalf("expected missing knowledge base error, got %v", err)
	}
}
