package main

import (
	"database/sql"
	"godoc-rag/embedder"
	"godoc-rag/mcp"
	"godoc-rag/parser"
	"godoc-rag/rag"
	"log"
	"os"

	_ "github.com/lib/pq"
	"github.com/ollama/ollama/api"
)

// TODO: improve package organization and use CLI flags

const (
	embeddingModel = "nomic-embed-text:latest"
	queryModel     = "qwen3:8b"
)

func main() {
	connStr := "postgres://postgres:postgres@localhost:5432/embeddings?sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Unable to connect:", err)
	}
	defer db.Close()

	client, err := api.ClientFromEnvironment()
	if err != nil {
		log.Fatal(err)
	}

	mode := "mcp"
	// mode := "chunks"
	// mode := "prompt"

	switch mode {
	case "chunks":
		if len(os.Args) < 2 {
			log.Fatal("Usage: go run main.go <directory>")
			return
		}

		rootDir := os.Args[1]
		p := parser.New(rootDir)
		e := embedder.New(db, client, p, embeddingModel)
		err := e.Embed()
		if err != nil {
			log.Fatalf("error processing files: %v", err)
		}

		log.Print("finished embedding chunks")
	case "prompt":
		// prompt := "I am designing another package that needs to update a user's email. Any advice?"
		prompt := "I am designing another package that needs to update a user's email. Which files should I look at first?"
		l := rag.NewLoader(db, client, embeddingModel, queryModel)
		err := l.Prompt(prompt)
		if err != nil {
			log.Fatal(err)
		}
	case "mcp":
		l := rag.NewLoader(db, client, embeddingModel, queryModel)
		s := mcp.NewServer(l)

		err := s.Run()
		if err != nil {
			log.Fatal(err)
		}
	}
}
