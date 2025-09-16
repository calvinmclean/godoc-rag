package mcp

import (
	"iter"
	"net/http"

	godocrag "godoc-rag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Loader enables the MCP Server to do semantic searches on the embedded data
type Loader interface {
	// SemanticSearch is used to search embedded data
	SemanticSearch(query string, limit int) (iter.Seq[godocrag.Data], func() error, error)
}

// Server implements the MCP Server for RAG
type Server struct {
	loader Loader
	server *mcp.Server
}

func NewServer(loader Loader) Server {
	s := Server{
		loader: loader,
		server: mcp.NewServer(&mcp.Implementation{Name: "godoc-rag", Version: "v1.0.0"}, &mcp.ServerOptions{
			Instructions: `This MCP server provides semantic search capabilities over Go
package documentation. It parses documentation from both internal projects
and external Go modules, generates vector embeddings, and stores them in
pgvector. When queried, the server retrieves the most relevant doc snippets,
functions, or API descriptions based on semantic similarity.

Use this server to:
- Look up Go functions, types, methods, and usage examples.
- Understand external packages or internal APIs without manually browsing docs.
- Aid code generation by retrieving contextually relevant Go documentation.

The server is not a code executor or compiler; it strictly provides
semantic search results from the indexed documentation.`,
		}),
	}
	mcp.AddTool(s.server, searchTool, s.semanticSearch)
	return s
}

func (s Server) Run() error {
	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return s.server
	}, nil)

	return http.ListenAndServe(":8080", handler)
}
