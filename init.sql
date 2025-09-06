CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS comment_data (
    id       SERIAL PRIMARY KEY,
    data     TEXT, -- actual comment data
    package  TEXT, -- package that this is contained by
    filename TEXT, -- filename where this came from
    symbol TEXT,   -- symbol describing the resource (function name, struct name, etc)
    UNIQUE(package, filename, symbol)
);

CREATE TABLE IF NOT EXISTS embeddings (
    id INTEGER PRIMARY KEY REFERENCES comment_data(id),
    embedding vector(768)
);
