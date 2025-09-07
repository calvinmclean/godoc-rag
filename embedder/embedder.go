package embedder

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"godoc-rag/detail"

	"github.com/ollama/ollama/api"
)

type Parser interface {
	Parse() <-chan detail.Detail
	Error() error
}

type Embedder struct {
	p            Parser
	db           *sql.DB
	ollamaClient *api.Client
	model        string
}

func New(db *sql.DB, ollamaClient *api.Client, p Parser, model string) Embedder {
	return Embedder{
		db:           db,
		ollamaClient: ollamaClient,
		p:            p,
		model:        model,
	}
}

func (e Embedder) Embed() error {
	for detail := range e.p.Parse() {
		// Store chunks and get their IDs
		id, err := e.storeChunk(detail)
		if err != nil {
			return fmt.Errorf("error storing chunks: %w", err)
		}

		// Get embeddings for each chunk and store them
		err = e.processChunkEmbedding(id, detail)
		if err != nil {
			return fmt.Errorf("error processing chunks: %w", err)
		}
	}

	return e.p.Error()
}

func (e Embedder) storeChunk(detail detail.Detail) (int, error) {
	var id int
	err := e.db.QueryRow(
		`INSERT INTO comment_data (data, package, filename, symbol)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (package, filename, symbol)
			DO UPDATE SET
				package = EXCLUDED.package,
				filename = EXCLUDED.filename,
				symbol = EXCLUDED.symbol
			RETURNING id`,
		detail.String(), detail.Package, detail.Filename, detail.Symbol,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to insert chunk: %v", err)
	}

	return id, nil
}

func (e Embedder) processChunkEmbedding(chunkID int, detail detail.Detail) error {
	req := api.EmbedRequest{
		Model: e.model,
		Input: detail.String(),
	}

	resp, err := e.ollamaClient.Embed(context.Background(), &req)
	if err != nil {
		return fmt.Errorf("failed to generate embedding for chunk: %w", err)
	}

	if len(resp.Embeddings) != 1 {
		return fmt.Errorf("unexpected number of embeddings returned for chunk")
	}

	err = e.insertEmbedding(chunkID, resp.Embeddings[0])
	if err != nil {
		return fmt.Errorf("failed to store embedding for chunk: %w", err)
	}

	return nil
}

func (e Embedder) insertEmbedding(chunkID int, vector []float32) error {
	// Convert vector to Postgres array literal
	strVals := make([]string, len(vector))
	for i, v := range vector {
		strVals[i] = fmt.Sprintf("%f", v)
	}
	arrayLiteral := fmt.Sprintf("[%s]", strings.Join(strVals, ","))

	_, err := e.db.Exec(
		`INSERT INTO embeddings (id, embedding)
		VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET
			embedding = EXCLUDED.embedding`,
		chunkID, arrayLiteral,
	)
	return err
}
