package rag

import (
	"context"
	"database/sql"
	"fmt"
	"iter"
	"strings"

	"github.com/ollama/ollama/api"

	godocrag "godoc-rag"
)

const defaultLimit = 3

type Loader struct {
	db             *sql.DB
	ollamaClient   *api.Client
	embeddingModel string
	queryModel     string
}

func NewLoader(db *sql.DB, ollamaClient *api.Client, embeddingModel, queryModel string) Loader {
	return Loader{
		db:             db,
		ollamaClient:   ollamaClient,
		embeddingModel: embeddingModel,
		queryModel:     queryModel,
	}
}

func (l Loader) SemanticSearch(ctx context.Context, query string, limit int) (iter.Seq[godocrag.Data], func() error, error) {
	req := api.EmbedRequest{
		Model: l.embeddingModel,
		Input: query,
	}

	resp, err := l.ollamaClient.Embed(ctx, &req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate embedding for query: %v", err)
	}

	// Convert query vector to Postgres array literal
	strVals := make([]string, len(resp.Embeddings[0]))
	for i, v := range resp.Embeddings[0] {
		strVals[i] = fmt.Sprintf("%f", v)
	}
	queryVector := fmt.Sprintf("[%s]", strings.Join(strVals, ","))

	// Query database for similar chunks using cosine similarity
	rows, err := l.db.QueryContext(ctx, `
		SELECT c.data, c.package, c.filename, c.symbol, c.type
		FROM comment_data c
		JOIN embeddings e ON c.id = e.id
		ORDER BY embedding <=> $1 LIMIT $2
	`, queryVector, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query similar chunks: %v", err)
	}

	var errorResult error
	return func(yield func(godocrag.Data) bool) {
			defer rows.Close()

			for rows.Next() {
				var d godocrag.Data
				if err := rows.Scan(&d.Data, &d.Package, &d.Filename, &d.Symbol, &d.Type); err != nil {
					errorResult = err
					return
				}

				if !yield(d) {
					return
				}
			}

			if err := rows.Err(); err != nil {
				errorResult = err
				return
			}
		}, func() error {
			return errorResult
		}, nil
}

func (l Loader) Prompt(query string) error {
	dataIter, getErr, err := l.SemanticSearch(context.Background(), query, defaultLimit)
	if err != nil {
		return err
	}

	var ragContext strings.Builder
	for d := range dataIter {
		ragContext.WriteString(fmt.Sprintf(`<context package=%q filename=%q symbol=%q type=%q>`, d.Package, d.Filename, d.Symbol, d.Type))
		ragContext.WriteString(d.Data)
		ragContext.WriteString("</context>\n")
	}
	if err := getErr(); err != nil {
		return err
	}

	prompt := fmt.Sprintf("<user>%s</user>\n%s", query, ragContext.String())
	fmt.Println(prompt)

	err = l.ollamaClient.Generate(context.Background(), &api.GenerateRequest{
		Model:  l.queryModel,
		Prompt: prompt,
		Stream: new(bool),
		Think:  &api.ThinkValue{Value: false},
		System: `You will receive user prompts/queries along with real context from RAG.
The user prompt will be surrounded by <user></user>
The context will be surrounded by <context source="..."></context>
Provide the user details about the source of the context that you use.
If the context doesn't contain relevant information, say "I don't have enough information to answer that question.`,
	}, func(gr api.GenerateResponse) error {
		fmt.Println(gr.Response)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to generate response: %v", err)
	}

	return nil
}
