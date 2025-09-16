package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	"godoc-rag/embedder"
	"godoc-rag/mcp"
	"godoc-rag/parser"
	"godoc-rag/rag"

	_ "github.com/lib/pq"
	"github.com/ollama/ollama/api"
	"github.com/urfave/cli/v3"
)

const (
	defaultEmbeddingModel = "nomic-embed-text:latest"
	defaultQueryModel     = "qwen3:8b"
	defaultConnStr        = "postgres://postgres:postgres@localhost:5432/embeddings?sslmode=disable"
	defaultPrompt         = "I am designing another package that needs to update a user's email. Which files should I look at first?"
)

func main() {
	var dbConnStr, embeddingModel, queryModel string
	var db *sql.DB
	var client *api.Client
	rootCmd := &cli.Command{
		Name:        "godoc-rag",
		Usage:       "RAG tools for Go documentation",
		Description: "A CLI for embedding, querying, and serving Go documentation using RAG.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "db",
				Usage:       "Postgres connection string",
				Value:       defaultConnStr,
				Destination: &dbConnStr,
				Sources:     cli.ValueSourceChain{Chain: []cli.ValueSource{cli.EnvVar("GODOC_RAG_DB")}},
			},
			&cli.StringFlag{
				Name:        "embedding-model",
				Usage:       "Embedding model name",
				Value:       defaultEmbeddingModel,
				Destination: &embeddingModel,
			},
			&cli.StringFlag{
				Name:        "query-model",
				Usage:       "Query model name",
				Value:       defaultQueryModel,
				Destination: &queryModel,
			},
		},
		Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
			var err error
			db, err = sql.Open("postgres", dbConnStr)
			if err != nil {
				return nil, fmt.Errorf("unable to connect: %w", err)
			}

			client, err = api.ClientFromEnvironment()
			if err != nil {
				return nil, err
			}

			return ctx, nil
		},
		After: func(ctx context.Context, c *cli.Command) error {
			return db.Close()
		},
		Commands: []*cli.Command{
			{
				Name:  "embed",
				Usage: "Embed chunks from a directory",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "dir",
						Usage:    "Root directory to parse",
						Required: true,
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					rootDir := cmd.String("dir")
					p := parser.New(rootDir)
					e := embedder.New(db, client, p, embeddingModel)
					if err := e.Embed(ctx); err != nil {
						return fmt.Errorf("error processing files: %w", err)
					}
					log.Print("finished embedding chunks")
					return nil
				},
			},
			{
				Name:  "prompt",
				Usage: "Query with a prompt",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "prompt",
						Usage: "Prompt to query",
						Value: defaultPrompt,
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					l := rag.NewLoader(db, client, embeddingModel, queryModel)
					if err := l.Prompt(cmd.String("prompt")); err != nil {
						return err
					}
					return nil
				},
			},
			{
				Name:  "mcp",
				Usage: "Run MCP server",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "addr",
						Usage:   "Address to bind MCP server (e.g. :8080)",
						Value:   ":8080",
						Sources: cli.ValueSourceChain{Chain: []cli.ValueSource{cli.EnvVar("ADDR")}},
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					l := rag.NewLoader(db, client, embeddingModel, queryModel)
					s := mcp.NewServer(l, cmd.String("addr"))
					return s.Run()
				},
			},
		},
	}

	if err := rootCmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
