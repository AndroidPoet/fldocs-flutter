package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func runMCPServer(db *sql.DB) error {
	s := server.NewMCPServer(
		"fldocs",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	// ── search_docs ─────────────────────────────────────────────────────────
	s.AddTool(
		mcp.NewTool("search_docs",
			mcp.WithDescription("Search Flutter and Jetpack Compose documentation by keyword or concept."),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Search terms, e.g. 'ListView', 'animation', 'state management'"),
			),
			mcp.WithString("source",
				mcp.Description("Filter by source: 'flutter' or 'compose'. Leave empty to search both."),
			),
			mcp.WithNumber("limit",
				mcp.Description("Max results to return (default 10)"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args, _ := req.Params.Arguments.(map[string]any)
			query, _ := args["query"].(string)
			source, _ := args["source"].(string)
			limit := 10
			if l, ok := args["limit"].(float64); ok {
				limit = int(l)
			}

			results, err := searchDocs(db, query, source, limit)
			if err != nil {
				return mcp.NewToolResultText(fmt.Sprintf("Search error: %v", err)), nil
			}
			if len(results) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("No results for '%s'.", query)), nil
			}

			var sb strings.Builder
			fmt.Fprintf(&sb, "Found %d result(s) for '%s':\n\n", len(results), query)
			for _, r := range results {
				sec := ""
				if r.Section != "" {
					sec = fmt.Sprintf(" [%s]", r.Section)
				}
				fmt.Fprintf(&sb, "**%s**%s [%s]\n", r.Title, sec, r.Source)
				fmt.Fprintf(&sb, "slug: `%s`  |  %s\n", r.Slug, r.URL)
				fmt.Fprintf(&sb, "...%s...\n\n", r.Snippet)
			}
			return mcp.NewToolResultText(sb.String()), nil
		},
	)

	// ── get_doc ─────────────────────────────────────────────────────────────
	s.AddTool(
		mcp.NewTool("get_doc",
			mcp.WithDescription("Get the full content of a Flutter or Compose doc page by its slug."),
			mcp.WithString("slug",
				mcp.Required(),
				mcp.Description("Doc slug, e.g. 'ui/widgets/basics'. Find slugs with search_docs first."),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args, _ := req.Params.Arguments.(map[string]any)
			slug, _ := args["slug"].(string)

			doc, err := getDoc(db, slug)
			if err != nil {
				return mcp.NewToolResultText(fmt.Sprintf("Error: %v", err)), nil
			}
			if doc == nil {
				results, _ := searchDocs(db, slug, "", 3)
				if len(results) > 0 {
					slugs := make([]string, len(results))
					for i, r := range results {
						slugs[i] = fmt.Sprintf("`%s`", r.Slug)
					}
					return mcp.NewToolResultText(fmt.Sprintf(
						"No doc for '%s'. Did you mean: %s?", slug, strings.Join(slugs, ", "),
					)), nil
				}
				return mcp.NewToolResultText(fmt.Sprintf("No doc found for '%s'. Try search_docs first.", slug)), nil
			}

			sec := ""
			if doc.Section != "" {
				sec = fmt.Sprintf(" [%s]", doc.Section)
			}
			content := fmt.Sprintf("# %s%s [%s]\n\nURL: %s\n\n%s",
				doc.Title, sec, doc.Source, doc.URL, doc.Content)
			return mcp.NewToolResultText(content), nil
		},
	)

	stdio := server.NewStdioServer(s)
	return stdio.Listen(context.Background(), os.Stdin, os.Stdout)
}
