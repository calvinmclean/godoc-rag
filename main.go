package main

import (
	"context"
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/lib/pq"
	"github.com/ollama/ollama/api"
)

// TODO: improve package organization and use CLI flags
// TODO: Add MCP for use with coding agents

const (
	model = "nomic-embed-text:latest"
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

	mode := "chunks"
	// mode := "prompt"

	switch mode {
	case "chunks":
		if len(os.Args) < 2 {
			fmt.Println("Usage: go run main.go <directory>")
			return
		}

		rootDir := os.Args[1]
		err := processFiles(db, client, rootDir)
		if err != nil {
			fmt.Println("error processing files:", err)
			return
		}
	case "prompt":
		// prompt := "I am designing another package that needs to update a user's email. Any advice?"
		prompt := "I am designing another package that needs to update a user's email. Which files should I look at first?"
		err := queryAndGenerate(db, client, prompt)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func processFiles(db *sql.DB, client *api.Client, path string) error {
	detailChan := make(chan Detail)
	errChan := make(chan error, 1)

	go func() {
		for detail := range detailChan {
			// Store chunks and get their IDs
			id, err := storeChunk(db, detail)
			if err != nil {
				errChan <- fmt.Errorf("error storing chunks: %w", err)
				return
			}
			detail.id = id

			// Get embeddings for each chunk and store them
			err = processChunkEmbedding(db, client, detail)
			if err != nil {
				errChan <- fmt.Errorf("error processing chunks: %w", err)
				return
			}
		}

		errChan <- nil
	}()

	err := parseGoDirectory(path, detailChan)
	if err != nil {
		return fmt.Errorf("error parsing directory: %w", err)
	}
	close(detailChan)

	// errChan is a buffered channel of size 1 and is always used by the goroutine.
	// This ensures correct synchronization
	return <-errChan
}

type Detail struct {
	Type     string // package, function, struct, interface
	Symbol   string // type name like Person or method like Person.Greet
	Comment  string
	Package  string
	Filename string

	Children []Detail

	id int
}

func (d Detail) StringIndent(indent string) string {
	var sb strings.Builder
	sb.WriteString(indent)
	sb.WriteString(d.Type)
	sb.WriteRune(' ')
	sb.WriteString(d.Symbol)
	sb.WriteRune(':')
	sb.WriteRune(' ')
	sb.WriteString(strings.TrimSpace(strings.ReplaceAll(d.Comment, "\n", " ")))

	for _, child := range d.Children {
		sb.WriteRune('\n')
		sb.WriteString(child.StringIndent(indent + "  "))
	}

	return sb.String()
}

func (d Detail) String() string {
	return d.StringIndent("")
}

func parseGoDirectory(rootDir string, detailChan chan<- Detail) error {
	fset := token.NewFileSet()
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}

		// Parse all Go files in this directory as a package
		pkgs, err := parser.ParseDir(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil
		}

		// Process each package
		for pkgName, pkg := range pkgs {
			// Process each file in the package
			for filename, file := range pkg.Files {
				parseAstFile(file, pkgName, filename, detailChan)
			}
		}
		return nil
	})
	return err
}

func parseAstFile(node *ast.File, packageName, filename string, detailChan chan<- Detail) {
	// Package doc
	if node.Doc != nil {
		detailChan <- Detail{
			Type:     "package",
			Symbol:   packageName,
			Comment:  node.Doc.Text(),
			Package:  packageName,
			Filename: filename,
		}
	}

	// Walk through declarations
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				s, ok := spec.(*ast.TypeSpec)
				if !ok || !s.Name.IsExported() {
					continue
				}
				details := getTypeDetail(s, d)
				details.Package = packageName
				details.Filename = filename
				detailChan <- details
			}

		case *ast.FuncDecl:
			if d.Doc == nil || !d.Name.IsExported() {
				continue
			}
			symbol := d.Name.Name
			if d.Recv != nil {
				recName := getTypeName(d.Recv.List[0].Type)
				symbol = recName + "." + symbol
			}
			detailChan <- Detail{
				Type:     "function",
				Symbol:   symbol,
				Comment:  d.Doc.Text(),
				Package:  packageName,
				Filename: filename,
			}
		}
	}
}

func storeChunk(db *sql.DB, detail Detail) (int, error) {
	var id int
	err := db.QueryRow(
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

func processChunkEmbedding(db *sql.DB, client *api.Client, detail Detail) error {
	req := api.EmbedRequest{
		Model: model,
		Input: detail.String(),
	}

	resp, err := client.Embed(context.Background(), &req)
	if err != nil {
		return fmt.Errorf("failed to generate embedding for chunk: %w", err)
	}

	if len(resp.Embeddings) != 1 {
		return fmt.Errorf("unexpected number of embeddings returned for chunk")
	}

	err = insertEmbedding(db, detail.id, resp.Embeddings[0])
	if err != nil {
		return fmt.Errorf("failed to store embedding for chunk: %w", err)
	}

	return nil
}

func insertEmbedding(db *sql.DB, chunkID int, vector []float32) error {
	// Convert vector to Postgres array literal
	strVals := make([]string, len(vector))
	for i, v := range vector {
		strVals[i] = fmt.Sprintf("%f", v)
	}
	arrayLiteral := fmt.Sprintf("[%s]", strings.Join(strVals, ","))

	_, err := db.Exec(
		`INSERT INTO embeddings (id, embedding)
		VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET
			embedding = EXCLUDED.embedding`,
		chunkID, arrayLiteral,
	)
	return err
}

func getTypeName(t ast.Expr) string {
	switch r := t.(type) {
	case *ast.Ident:
		return r.Name
	case *ast.StarExpr:
		if typeIdent, ok := r.X.(*ast.Ident); ok {
			return "*" + typeIdent.Name
		}
	}
	return ""
}

func getTypeDetail(s *ast.TypeSpec, g *ast.GenDecl) Detail {
	var details Detail

	typeKind := "type"
	switch t := s.Type.(type) {
	case *ast.StructType:
		typeKind = "struct"
	case *ast.InterfaceType:
		typeKind = "interface"
	case *ast.ArrayType:
		typeKind = "array"
	case *ast.MapType:
		typeKind = "map"
	case *ast.ChanType:
		typeKind = "channel"
	case *ast.FuncType:
		typeKind = "function"
	case *ast.Ident:
		typeKind = fmt.Sprintf("alias for %s", t.Name)
	}
	details.Type = typeKind
	details.Symbol = s.Name.Name

	if g.Doc != nil {
		details.Comment = g.Doc.Text()
	}
	switch t := s.Type.(type) {
	case *ast.StructType:
		for _, field := range t.Fields.List {
			names := []string{}
			for _, n := range field.Names {
				names = append(names, n.Name)
			}
			fieldName := strings.Join(names, ", ")
			if fieldName == "" { // embedded struct
				fieldName = fmt.Sprintf("embedded %s", fmt.Sprint(field.Type))
			}

			// Print field type
			fieldType := fmt.Sprint(field.Type)

			var comment string
			if field.Doc != nil {
				comment += field.Doc.Text()
			}
			if field.Comment != nil {
				comment += field.Comment.Text()
			}

			details.Children = append(details.Children, Detail{
				Type:    fieldType,
				Symbol:  fmt.Sprintf("%s.%s", details.Symbol, fieldName),
				Comment: comment,
			})
		}
	case *ast.InterfaceType:
		for _, method := range t.Methods.List {
			names := []string{}
			for _, n := range method.Names {
				names = append(names, n.Name)
			}
			methodName := strings.Join(names, ", ")
			if methodName == "" { // embedded interface
				methodName = fmt.Sprintf("embedded %s", fmt.Sprint(method.Type))
			}
			var comment string
			if method.Doc != nil {
				comment += method.Doc.Text()
			}
			if method.Comment != nil {
				comment += method.Comment.Text()
			}

			details.Children = append(details.Children, Detail{
				Type:    "method",
				Symbol:  fmt.Sprintf("%s.%s", details.Symbol, methodName),
				Comment: comment,
			})
		}
	}

	return details
}

type QueriedContext struct {
	Context  string
	Package  string
	Filename string
}

func searchSimilarChunks(db *sql.DB, client *api.Client, query string, limit int) ([]QueriedContext, error) {
	req := api.EmbedRequest{
		Model: model,
		Input: query,
	}

	resp, err := client.Embed(context.Background(), &req)
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
	rows, err := db.Query(`
		SELECT c.data, c.package, c.filename
		FROM comment_data c
		JOIN embeddings e ON c.id = e.id
		ORDER BY embedding <=> $1 LIMIT $2
	`, queryVector, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query similar chunks: %v", err)
	}
	defer rows.Close()

	var results []QueriedContext
	for rows.Next() {
		var c QueriedContext
		if err := rows.Scan(&c.Context, &c.Package, &c.Filename); err != nil {
			return nil, err
		}
		results = append(results, c)
	}

	return results, nil
}

func queryAndGenerate(db *sql.DB, client *api.Client, query string) error {
	// Search for relevant chunks
	queriedContext, err := searchSimilarChunks(db, client, query, 3)
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

	err = client.Generate(context.Background(), &api.GenerateRequest{
		Model:  "qwen3:8b",
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
