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
	"strings"

	_ "github.com/lib/pq"
	"github.com/ollama/ollama/api"
)

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

	// mode := "chunks"
	mode := "prompt"

	switch mode {
	case "chunks":
		if len(os.Args) < 2 {
			fmt.Println("Usage: go run main.go <file.go>")
			return
		}

		filename := os.Args[1]
		details, err := parseGoFile(filename)
		if err != nil {
			fmt.Println(err)
			return
		}

		for _, detail := range details {
			fmt.Println("-----")
			fmt.Println(detail)
		}

		// Store chunks and get their IDs
		details, err = storeChunks(db, details)
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("stored all %d chunks", len(details))

		// Get embeddings for each chunk and store them
		err = processChunkEmbeddings(db, client, details)
		if err != nil {
			log.Fatal(err)
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

func parseGoFile(filename string) ([]Detail, error) {
	fset := token.NewFileSet()

	// Parse with comments
	node, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("error parsing file: %w", err)
	}

	var result []Detail

	packageName := node.Name.Name

	// Package doc
	if node.Doc != nil {
		result = append(result, Detail{
			Type:     "package",
			Symbol:   packageName,
			Comment:  node.Doc.Text(),
			Package:  packageName,
			Filename: filename,
		})
	}

	// Walk through declarations
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					details := getTypeDetail(s, d)
					details.Package = packageName
					details.Filename = filename
					result = append(result, details)
				}
			}

		case *ast.FuncDecl:
			if d.Doc != nil {
				symbol := d.Name.Name
				if d.Recv != nil {
					recName := getTypeName(d.Recv.List[0].Type)
					symbol = recName + "." + symbol
				}
				result = append(result, Detail{
					Type:     "function",
					Symbol:   symbol,
					Comment:  d.Doc.Text(),
					Package:  packageName,
					Filename: filename,
				})
			}
		}
	}

	return result, nil
}

func storeChunks(db *sql.DB, details []Detail) ([]Detail, error) {
	for i, c := range details {
		var id int
		err := db.QueryRow("INSERT INTO comment_data (data, package, filename) VALUES ($1, $2, $3) RETURNING id", c.String(), c.Package, c.Filename).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("failed to insert chunk %d: %v", i, err)
		}
		details[i].id = id
		log.Printf("Successfully stored chunk %d with ID %d", i, id)
	}

	return details, nil
}

func processChunkEmbeddings(db *sql.DB, client *api.Client, details []Detail) error {
	for i, c := range details {
		// Get embedding for the chunk
		req := api.EmbedRequest{
			Model: model,
			Input: c.String(),
		}

		resp, err := client.Embed(context.Background(), &req)
		if err != nil {
			return fmt.Errorf("failed to generate embedding for chunk %d: %v", i, err)
		}

		if len(resp.Embeddings) != 1 {
			return fmt.Errorf("unexpected number of embeddings returned for chunk %d", i)
		}

		// Store the embedding
		err = insertEmbedding(db, c.id, resp.Embeddings[0])
		if err != nil {
			return fmt.Errorf("failed to store embedding for chunk %d: %v", i, err)
		}

		log.Printf("Successfully processed and stored embedding for chunk %d", i)
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

	_, err := db.Exec("INSERT INTO embeddings (id, embedding) VALUES ($1, $2)",
		chunkID, arrayLiteral)
	return err
}

func getTypeName(t ast.Expr) string {
	var recvType string
	if typeIdent, ok := t.(*ast.Ident); ok {
		recvType = typeIdent.Name
	} else if starExpr, ok := t.(*ast.StarExpr); ok {
		if typeIdent, ok := starExpr.X.(*ast.Ident); ok {
			recvType = "*" + typeIdent.Name
		}
	}
	return recvType
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
