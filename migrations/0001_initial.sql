PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS memories (
  id INTEGER PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
  title,
  body,
  content='memories',
  content_rowid='id'
);

-- Created dynamically after embedding dimensions are detected:
-- CREATE VIRTUAL TABLE memory_vec USING vec0(embedding float[N] distance_metric=cosine);
