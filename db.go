package main

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type Doc struct {
	Slug     string
	Title    string
	Content  string
	Section  string
	Source   string
	URL      string
	SyncedAt string
}

type SearchResult struct {
	Slug    string
	Title   string
	Section string
	Source  string
	URL     string
	Snippet string
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("cannot open database at %s: %w\nRun: fldocs sync", path, err)
	}
	return db, nil
}

func searchDocs(db *sql.DB, query, source string, limit int) ([]SearchResult, error) {
	q := `
		SELECT d.slug, d.title, COALESCE(d.section,''), d.source, d.url,
		       snippet(docs_fts, 1, '[', ']', '...', 40) AS snippet
		FROM docs_fts
		JOIN docs d ON d.id = docs_fts.rowid
		WHERE docs_fts MATCH ?
		  AND (? = '' OR d.source = ?)
		ORDER BY rank
		LIMIT ?`
	rows, err := db.Query(q, query, source, source, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Slug, &r.Title, &r.Section, &r.Source, &r.URL, &r.Snippet); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

func getDoc(db *sql.DB, slug string) (*Doc, error) {
	row := db.QueryRow(
		`SELECT slug, title, content, COALESCE(section,''), source, url, synced_at
		 FROM docs WHERE slug = ?`, slug)
	var d Doc
	if err := row.Scan(&d.Slug, &d.Title, &d.Content, &d.Section, &d.Source, &d.URL, &d.SyncedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func listDocs(db *sql.DB, source string) ([]SearchResult, error) {
	q := `SELECT slug, title, COALESCE(section,''), source, url FROM docs
		  WHERE (? = '' OR source = ?)
		  ORDER BY source, section, title`
	rows, err := db.Query(q, source, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Slug, &r.Title, &r.Section, &r.Source, &r.URL); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

func docCount(db *sql.DB) (int, int, error) {
	var flutter, compose int
	db.QueryRow(`SELECT COUNT(*) FROM docs WHERE source='flutter'`).Scan(&flutter)
	db.QueryRow(`SELECT COUNT(*) FROM docs WHERE source='compose'`).Scan(&compose)
	return flutter, compose, nil
}
