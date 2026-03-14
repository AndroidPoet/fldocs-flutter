package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

//go:embed data/docs.db
var embeddedData embed.FS

var dbPath = filepath.Join(os.Getenv("HOME"), ".fldocs-flutter", "docs.db")

func getDB() (*sql.DB, error) {
	// Extract bundled DB on first run
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if err := extractBundledDB(); err != nil {
			return nil, fmt.Errorf("failed to extract bundled database: %w", err)
		}
	}
	return openDB(dbPath)
}

func extractBundledDB() error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return err
	}
	data, err := fs.ReadFile(embeddedData, "data/docs.db")
	if err != nil {
		return err
	}
	return os.WriteFile(dbPath, data, 0644)
}

func main() {
	root := &cobra.Command{
		Use:   "fldocs-flutter",
		Short: "Flutter docs, offline.",
	}

	// ── search ──────────────────────────────────────────────────────────────
	var searchSource string
	var searchLimit int
	var searchJSON bool

	searchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search Flutter docs.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := getDB()
			if err != nil {
				return err
			}
			defer db.Close()

			query := args[0]
			results, err := searchDocs(db, query, searchSource, searchLimit)
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Fprintf(os.Stderr, "No results for '%s'\n", query)
				os.Exit(1)
			}

			if searchJSON {
				return json.NewEncoder(os.Stdout).Encode(results)
			}

			fmt.Printf("\n%d result(s) for '%s':\n\n", len(results), query)
			for _, r := range results {
				src := colorize(r.Source)
				sec := ""
				if r.Section != "" {
					sec = fmt.Sprintf("  [%s]", r.Section)
				}
				fmt.Printf("  \033[1;36m%s\033[0m%s %s\n", r.Title, sec, src)
				fmt.Printf("  slug: %s  →  %s\n", r.Slug, r.URL)
				fmt.Printf("  ...%s...\n\n", r.Snippet)
			}
			return nil
		},
	}
	searchCmd.Flags().StringVarP(&searchSource, "source", "s", "", "Filter by source: flutter or compose")
	searchCmd.Flags().IntVarP(&searchLimit, "limit", "n", 10, "Max results")
	searchCmd.Flags().BoolVar(&searchJSON, "json", false, "Output as JSON")

	// ── get ─────────────────────────────────────────────────────────────────
	var getJSON bool

	getCmd := &cobra.Command{
		Use:   "get <slug>",
		Short: "Get full content of a doc page by slug.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := getDB()
			if err != nil {
				return err
			}
			defer db.Close()

			doc, err := getDoc(db, args[0])
			if err != nil {
				return err
			}
			if doc == nil {
				fmt.Fprintf(os.Stderr, "No doc found for '%s'. Try: fldocs search %s\n", args[0], args[0])
				os.Exit(1)
			}

			if getJSON {
				return json.NewEncoder(os.Stdout).Encode(doc)
			}

			src := colorize(doc.Source)
			sec := ""
			if doc.Section != "" {
				sec = fmt.Sprintf(" [%s]", doc.Section)
			}
			fmt.Printf("\n\033[1;36m# %s\033[0m%s %s\n", doc.Title, sec, src)
			fmt.Printf("\033[90mURL: %s\033[0m\n", doc.URL)
			fmt.Printf("\033[90mSynced: %s\033[0m\n\n", doc.SyncedAt)
			fmt.Println(doc.Content)
			return nil
		},
	}
	getCmd.Flags().BoolVar(&getJSON, "json", false, "Output as JSON")

	// ── ls ──────────────────────────────────────────────────────────────────
	var lsSource string

	lsCmd := &cobra.Command{
		Use:   "ls",
		Short: "List all stored doc pages.",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := getDB()
			if err != nil {
				return err
			}
			defer db.Close()

			docs, err := listDocs(db, lsSource)
			if err != nil {
				return err
			}
			if len(docs) == 0 {
				fmt.Fprintln(os.Stderr, "No docs found.")
				os.Exit(1)
			}

			currentSection := ""
			for _, d := range docs {
				key := d.Source + "::" + d.Section
				if key != currentSection {
					currentSection = key
					src := colorize(d.Source)
					sec := d.Section
					if sec == "" {
						sec = "General"
					}
					fmt.Printf("\n\033[1;33m%s\033[0m %s\n", sec, src)
				}
				fmt.Printf("  %-50s %s\n", d.Slug, d.Title)
			}
			return nil
		},
	}
	lsCmd.Flags().StringVarP(&lsSource, "source", "s", "", "Filter by source: flutter or compose")

	// ── stats ────────────────────────────────────────────────────────────────
	statsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show database stats.",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := getDB()
			if err != nil {
				return err
			}
			defer db.Close()

			flutter, compose, err := docCount(db)
			if err != nil {
				return err
			}
			fmt.Printf("Flutter docs:  %d pages\n", flutter)
			fmt.Printf("Compose docs:  %d pages\n", compose)
			fmt.Printf("Total:         %d pages\n", flutter+compose)
			fmt.Printf("Database:      %s\n", dbPath)
			return nil
		},
	}

	// ── mcp ──────────────────────────────────────────────────────────────────
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP server for Claude Code integration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := getDB()
			if err != nil {
				return err
			}
			return runMCPServer(db)
		},
	}

	root.AddCommand(searchCmd, getCmd, lsCmd, statsCmd, mcpCmd)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func colorize(source string) string {
	switch source {
	case "flutter":
		return "\033[34m[Flutter]\033[0m"
	case "compose":
		return "\033[32m[Compose]\033[0m"
	default:
		return ""
	}
}
