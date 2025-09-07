package rag

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ollama/ollama/api"
)

const defaultLimit = 3

type contextResult struct {
	Context  string
	Package  string
	Filename string
}

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

func (l Loader) loadContextFromDB(query string, limit int) ([]contextResult, error) {
	req := api.EmbedRequest{
		Model: l.embeddingModel,
		Input: query,
	}

	resp, err := l.ollamaClient.Embed(context.Background(), &req)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedding for query: %v", err)
	}

	// Convert query vector to Postgres array literal
	strVals := make([]string, len(resp.Embeddings[0]))
	for i, v := range resp.Embeddings[0] {
		strVals[i] = fmt.Sprintf("%f", v)
	}
	queryVector := fmt.Sprintf("[%s]", strings.Join(strVals, ","))

	// Query database for similar chunks using cosine similarity
	rows, err := l.db.Query(`
		SELECT c.data, c.package, c.filename
		FROM comment_data c
		JOIN embeddings e ON c.id = e.id
		ORDER BY embedding <=> $1 LIMIT $2
	`, queryVector, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query similar chunks: %v", err)
	}
	defer rows.Close()

	var results []contextResult
	for rows.Next() {
		var c contextResult
		if err := rows.Scan(&c.Context, &c.Package, &c.Filename); err != nil {
			return nil, err
		}
		results = append(results, c)
	}

	return results, nil
}

func (l Loader) Prompt(query string) error {
	// Search for relevant chunks
	queriedContext, err := l.loadContextFromDB(query, defaultLimit)
	if err != nil {
		return fmt.Errorf("failed to search chunks: %v", err)
	}

	var ragContext strings.Builder
	for _, c := range queriedContext {
		ragContext.WriteString(fmt.Sprintf(`<context package=%q filename=%q>`, c.Package, c.Filename))
		ragContext.WriteString(c.Context)
		ragContext.WriteString("</context>\n")
	}

	prompt := fmt.Sprintf("<user>%s</user>\n%s", query, ragContext.String())

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
