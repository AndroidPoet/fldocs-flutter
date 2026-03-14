"""
fldocs scraper — builds data/docs.db from Flutter and Compose docs.
Uses Scrapling for HTTP fetching and HTML parsing.

Usage:
    python3 scraper/scraper.py                  # scrape both
    python3 scraper/scraper.py --source flutter  # Flutter only
    python3 scraper/scraper.py --source compose  # Compose only
"""

import argparse
import re
import sqlite3
import threading
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from typing import Optional

from scrapling.fetchers import Fetcher

DB_PATH = Path(__file__).parent.parent / "data" / "docs.db"

SOURCES = {
    "flutter": {
        "sitemap": "https://docs.flutter.dev/sitemap.xml",
        "url_pattern": r"https://docs\.flutter\.dev/.+",
        "url_exclude": r"(docs\.flutter\.dev/$)",
        "content_sel": "main#site-content-title, article, main",
        "title_sel": "h1",
        "breadcrumb_sel": "nav.breadcrumbs a, .breadcrumb a",
    },
    "compose": {
        "sitemap_index": "https://developer.android.com/sitemap.xml",
        "url_pattern": r"https://developer\.android\.com/develop/ui/compose/.+",
        "content_sel": "article, main#main-content, main",
        "title_sel": "h1, devsite-page-title",
        "breadcrumb_sel": ".devsite-breadcrumb-item a, nav[aria-label='breadcrumb'] a",
    },
}


# ── Database ──────────────────────────────────────────────────────────────────

def init_db(conn: sqlite3.Connection) -> None:
    conn.executescript("""
        CREATE TABLE IF NOT EXISTS docs (
            id        INTEGER PRIMARY KEY AUTOINCREMENT,
            slug      TEXT    UNIQUE NOT NULL,
            title     TEXT    NOT NULL,
            content   TEXT    NOT NULL,
            section   TEXT,
            source    TEXT    NOT NULL,
            url       TEXT    NOT NULL,
            synced_at TEXT    DEFAULT (datetime('now'))
        );
        CREATE VIRTUAL TABLE IF NOT EXISTS docs_fts USING fts5(
            title,
            content,
            content='docs',
            content_rowid='id',
            tokenize='porter unicode61'
        );
        CREATE TRIGGER IF NOT EXISTS docs_ai AFTER INSERT ON docs BEGIN
            INSERT INTO docs_fts(rowid, title, content)
            VALUES (new.id, new.title, new.content);
        END;
        CREATE TRIGGER IF NOT EXISTS docs_au AFTER UPDATE ON docs BEGIN
            INSERT INTO docs_fts(docs_fts, rowid, title, content)
            VALUES ('delete', old.id, old.title, old.content);
            INSERT INTO docs_fts(rowid, title, content)
            VALUES (new.id, new.title, new.content);
        END;
        CREATE TRIGGER IF NOT EXISTS docs_ad AFTER DELETE ON docs BEGIN
            INSERT INTO docs_fts(docs_fts, rowid, title, content)
            VALUES ('delete', old.id, old.title, old.content);
        END;
    """)
    conn.commit()


def upsert(conn, slug, title, content, section, source, url):
    conn.execute("""
        INSERT INTO docs (slug, title, content, section, source, url, synced_at)
        VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
        ON CONFLICT(slug) DO UPDATE SET
            title=excluded.title, content=excluded.content,
            section=excluded.section, source=excluded.source,
            url=excluded.url, synced_at=excluded.synced_at
    """, (slug, title, content, section, source, url))
    conn.commit()


# ── URL Discovery ─────────────────────────────────────────────────────────────

def get_flutter_urls() -> list[str]:
    with urllib.request.urlopen(SOURCES["flutter"]["sitemap"]) as r:
        content = r.read().decode()
    urls = re.findall(r"https://docs\.flutter\.dev/[^\s<]+", content)
    return sorted({u for u in urls if not u.endswith("docs.flutter.dev/")})


def get_compose_urls() -> list[str]:
    with urllib.request.urlopen(SOURCES["compose"]["sitemap_index"]) as r:
        index = r.read().decode()

    sitemap_files = re.findall(r"https://developer\.android\.com/sitemap_\d+_of_\d+\.xml", index)
    urls = set()
    for sm in sitemap_files:
        try:
            with urllib.request.urlopen(sm) as r:
                content = r.read().decode()
            found = re.findall(r"https://developer\.android\.com/develop/ui/compose/[a-z0-9/_-]+", content)
            # No query params, no localized versions, no trailing slashes
            for u in found:
                u = u.rstrip("/")
                if u and "?" not in u and "#" not in u:
                    urls.add(u)
        except Exception:
            pass
    return sorted(urls)


# ── Scraping ──────────────────────────────────────────────────────────────────

def slug_from_url(url: str) -> str:
    # flutter: https://docs.flutter.dev/ui/widgets/basics → ui/widgets/basics
    # compose: https://developer.android.com/develop/ui/compose/layouts → compose/layouts
    url = url.rstrip("/")
    if "docs.flutter.dev/" in url:
        return url.split("docs.flutter.dev/")[-1]
    if "developer.android.com/develop/ui/compose" in url:
        return "compose/" + url.split("/develop/ui/compose/")[-1]
    return url.split("/")[-1]


def scrape_page(url: str, source: str, fetcher: Fetcher) -> Optional[tuple]:
    try:
        page = fetcher.get(url)
    except Exception:
        return None

    cfg = SOURCES[source]

    # Title
    for sel in cfg["title_sel"].split(", "):
        el = page.css(sel.strip()).first
        if el:
            title = el.get_all_text().strip()
            break
    else:
        title = slug_from_url(url).replace("-", " ").replace("/", " › ").title()

    # Section from breadcrumbs
    section = None
    for sel in cfg["breadcrumb_sel"].split(", "):
        crumbs = list(page.css(sel.strip()))
        if len(crumbs) >= 2:
            section = crumbs[-2].get_all_text().strip()
            break

    # Content
    container = None
    for sel in cfg["content_sel"].split(", "):
        container = page.css(sel.strip()).first
        if container:
            break

    content = container.get_all_text().strip() if container else ""
    content = re.sub(r"[ \t]+", " ", content)
    content = re.sub(r"\n{3,}", "\n\n", content)

    slug = slug_from_url(url)
    return slug, title, section, content


def sync_source(source: str, conn: sqlite3.Connection, workers: int = 8) -> tuple[int, int]:
    print(f"\nFetching {source} URLs...")
    if source == "flutter":
        urls = get_flutter_urls()
    else:
        urls = get_compose_urls()

    # Skip already-scraped pages
    existing = {row[0] for row in conn.execute("SELECT slug FROM docs WHERE source=?", (source,))}
    urls = [u for u in urls if slug_from_url(u) not in existing]

    if not urls:
        print(f"  All {source} pages already up to date.")
        return 0, 0

    total = len(urls)
    print(f"Found {total} pages to scrape (using {workers} workers)...\n")

    lock = threading.Lock()
    success, errors = 0, 0
    done = 0

    def fetch_one(url):
        fetcher = Fetcher()
        return url, scrape_page(url, source, fetcher)

    with ThreadPoolExecutor(max_workers=workers) as pool:
        futures = {pool.submit(fetch_one, url): url for url in urls}
        for future in as_completed(futures):
            nonlocal_done = None
            url, result = future.result()
            with lock:
                done += 1
                if result:
                    slug, title, section, content = result
                    upsert(conn, slug, title, content, section, source, url)
                    success += 1
                    sec = f" [{section}]" if section else ""
                    print(f"  [{done:3}/{total}]{sec} {title[:60]}")
                else:
                    errors += 1
                    print(f"  [{done:3}/{total}] ERROR: {url}")

    return success, errors


# ── Main ──────────────────────────────────────────────────────────────────────

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Scrape Flutter + Compose docs into SQLite")
    parser.add_argument("--source", choices=["flutter", "compose"], default=None,
                        help="Scrape only one source (default: both)")
    args = parser.parse_args()

    DB_PATH.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(DB_PATH)
    init_db(conn)

    sources = [args.source] if args.source else ["flutter", "compose"]
    total_ok, total_err = 0, 0

    for src in sources:
        ok, err = sync_source(src, conn)
        total_ok += ok
        total_err += err

    conn.close()
    print(f"\nDone. {total_ok} pages stored, {total_err} errors.")
    print(f"DB: {DB_PATH}")
