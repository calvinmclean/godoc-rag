package embedder

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ollama/ollama/api"

	godocrag "godoc-rag"
)

type Parser interface {
	Parse() <-chan godocrag.Data
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
	for data := range e.p.Parse() {
		// Store chunks and get their IDs
		id, err := e.storeChunk(data)
		if err != nil {
			return fmt.Errorf("error storing chunks: %w", err)
		}

		// Get embeddings for each chunk and store them
		err = e.processChunkEmbedding(id, data)
		if err != nil {
			return fmt.Errorf("error processing chunks: %w", err)
		}
	}

	return e.p.Error()
}

func (e Embedder) storeChunk(data godocrag.Data) (int, error) {
	var id int
	err := e.db.QueryRow(
		`INSERT INTO comment_data (data, package, filename, symbol, type)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (package, filename, symbol, type)
			DO UPDATE SET
				package = EXCLUDED.package,
				filename = EXCLUDED.filename,
				symbol = EXCLUDED.symbol,
				type = EXCLUDED.type
			RETURNING id`,
		data.String(), data.Package, data.Filename, data.Symbol, data.Type,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to insert chunk: %v", err)
	}

	return id, nil
}

func (e Embedder) processChunkEmbedding(chunkID int, data godocrag.Data) error {
	req := api.EmbedRequest{
		Model: e.model,
		Input: data.String(),
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
