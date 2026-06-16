package mallow

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/galihanggara68/mallow/pkg/compiler"
	"github.com/galihanggara68/mallow/pkg/ir"
	"github.com/galihanggara68/mallow/pkg/translator"
)

// Engine is the central point for executing Mallow queries.
type Engine struct {
	dialect compiler.Dialect
	db      *sql.DB
}

// New creates a new Mallow engine instance.
func New(dialect compiler.Dialect, db *sql.DB) *Engine {
	if dialect == nil {
		dialect = &compiler.PostgresDialect{}
	}
	return &Engine{
		dialect: dialect,
		db:      db,
	}
}

// Session represents a loaded Mallow file or string.
type Session struct {
	engine  *Engine
	content string
	err     error
}

// FromString creates a new session from a string containing Mallow source.
func (e *Engine) FromString(content string) *Session {
	return &Session{
		engine:  e,
		content: content,
	}
}

// FromFile creates a new session by reading a Mallow file.
func (e *Engine) FromFile(path string) *Session {
	content, err := os.ReadFile(path)
	return &Session{
		engine:  e,
		content: string(content),
		err:     err,
	}
}

// Compile is an alias for FromFile to match PRD if necessary, but FromFile is clearer.
func (e *Engine) FromSource(path string) *Session {
	return e.FromFile(path)
}

// GetSQL returns the compiled SQL for the specified query.
// If queryName is empty, it attempts to return the first query found.
func (s *Session) GetSQL(queryName string) (string, error) {
	if s.err != nil {
		return "", s.err
	}

	parsed, err := translator.Parser.ParseString("", s.content)
	if err != nil {
		return "", err
	}

	tr := translator.NewTranslator()
	sources, queries, err := tr.Translate(parsed)
	if err != nil {
		return "", err
	}

	if len(queries) == 0 {
		return "", fmt.Errorf("no queries found in source")
	}

	var targetQuery *ir.Query
	for _, q := range queries {
		if q.Name == queryName || (queryName == "" && q.Name == "") {
			targetQuery = &q
			break
		}
	}

	// If queryName is not provided and not matched above, default to first query
	if targetQuery == nil && queryName == "" {
		targetQuery = &queries[0]
	}

	if targetQuery == nil {
		return "", fmt.Errorf("query not found: %s", queryName)
	}

	var targetSource *ir.SourceDef
	for _, src := range sources {
		if src.Name == targetQuery.SourceName {
			targetSource = &src
			break
		}
	}

	if targetSource == nil {
		return "", fmt.Errorf("source not found for query: %s", targetQuery.SourceName)
	}

	comp := compiler.NewCompiler(s.engine.dialect)
	return comp.Compile(*targetSource, *targetQuery)
}

// Run executes the specified query against the configured database.
func (s *Session) Run(ctx context.Context, queryName string) (*sql.Rows, error) {
	sqlStr, err := s.GetSQL(queryName)
	if err != nil {
		return nil, err
	}

	if s.engine.db == nil {
		return nil, fmt.Errorf("database connection not configured")
	}

	return s.engine.db.QueryContext(ctx, sqlStr)
}
