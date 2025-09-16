package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var searchTool = &mcp.Tool{
	Name:        "search",
	Description: "Perform a semantic search on the Go documentation data", // TODO: improve
}

type Input struct {
	Query string `json:"query" jsonschema:"input to use for semantic search"`
	Limit int    `json:"limit" jsonschema:"max number of results to get from the search"`
}

type Data struct {
	Type     string `jsonschema:"type of the symbol (function, struct, package, etc."`
	Symbol   string `jsonschema:"name of the symbol"`
	Data     string `jsonschema:"relevant context data (i.e. comment text)"`
	Package  string `jsonschema:"name of the Go package"`
	Filename string `jsonschema:"filename for the data"`
}

type Output struct {
	Data []Data `jsonschema:"array of context data from the semantic search"`
}

func (s Server) semanticSearch(ctx context.Context, req *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, Output, error) {
	dataIter, getErr, err := s.loader.SemanticSearch(input.Query, input.Limit)
	if err != nil {
		return nil, Output{}, fmt.Errorf("error performing search: %w", err)
	}

	output := Output{}
	for d := range dataIter {
		output.Data = append(output.Data, Data{
			Type:     d.Type,
			Symbol:   d.Symbol,
			Data:     d.Data,
			Package:  d.Package,
			Filename: d.Filename,
		})
	}
	if err := getErr(); err != nil {
		return nil, Output{}, fmt.Errorf("error parsing search data: %w", err)
	}

	return nil, output, nil
}
