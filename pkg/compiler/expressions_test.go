package compiler

import (
	"testing"

	"github.com/galihanggara68/mallow/pkg/ir"
)

func TestCompileExpressions(t *testing.T) {
	dialect := &DuckDBDialect{}
	comp := &sqlCompiler{dialect: dialect}

	tests := []struct {
		name     string
		expr     ir.Expr
		expected string
	}{
		{
			name:     "literal string",
			expr:     ir.Literal{Value: "hello", Type: ir.TypeString},
			expected: "'hello'",
		},
		{
			name:     "literal number",
			expr:     ir.Literal{Value: 42, Type: ir.TypeNumber},
			expected: "42",
		},
		{
			name:     "literal boolean true",
			expr:     ir.Literal{Value: true, Type: ir.TypeBoolean},
			expected: "TRUE",
		},
		{
			name:     "literal boolean false",
			expr:     ir.Literal{Value: false, Type: ir.TypeBoolean},
			expected: "FALSE",
		},
		{
			name:     "field reference",
			expr:     ir.FieldReference{Path: []string{"brand"}},
			expected: "\"brand\"",
		},
		{
			name:     "joined field reference",
			expr:     ir.FieldReference{Path: []string{"orders", "brand"}},
			expected: "\"orders\".\"brand\"",
		},
		{
			name: "binary add",
			expr: ir.BinaryExpr{
				Op:    ir.OpAdd,
				Left:  ir.FieldReference{Path: []string{"price"}},
				Right: ir.Literal{Value: 10, Type: ir.TypeNumber},
			},
			expected: "(\"price\" + 10)",
		},
		{
			name: "unary not",
			expr: ir.UnaryExpr{
				Op:    ir.OpNot,
				Child: ir.Literal{Value: true, Type: ir.TypeBoolean},
			},
			expected: "NOT (TRUE)",
		},
		{
			name:     "function call count",
			expr:     ir.CallExpr{Name: "count", Args: []ir.Expr{}},
			expected: "COUNT(*)",
		},
		{
			name: "function call sum",
			expr: ir.CallExpr{
				Name: "sum",
				Args: []ir.Expr{ir.FieldReference{Path: []string{"sales"}}},
			},
			expected: "SUM(\"sales\")",
		},
		{
			name: "function call day (dialect specific)",
			expr: ir.CallExpr{
				Name: "day",
				Args: []ir.Expr{ir.FieldReference{Path: []string{"order_date"}}},
			},
			expected: "EXTRACT(DAY FROM \"order_date\")",
		},
		{
			name: "nested complex formula",
			expr: ir.BinaryExpr{
				Op: ir.OpMul,
				Left: ir.BinaryExpr{
					Op:    ir.OpAdd,
					Left:  ir.FieldReference{Path: []string{"a"}},
					Right: ir.FieldReference{Path: []string{"b"}},
				},
				Right: ir.FieldReference{Path: []string{"c"}},
			},
			expected: "((\"a\" + \"b\") * \"c\")",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := comp.compileExpr(tt.expr, "", false)
			if err != nil {
				t.Fatalf("compileExpr failed: %v", err)
			}
			if got != tt.expected {
				t.Errorf("compileExpr() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCompileDimensionWithExpr(t *testing.T) {
	dialect := &DuckDBDialect{}
	comp := NewCompiler(dialect)

	source := ir.SourceDef{
		Name: "orders",
		Fields: map[string]ir.FieldDef{
			"revenue": {
				Kind: ir.KindDimension,
				Name: "revenue",
				Expr: ir.BinaryExpr{
					Op:    ir.OpMul,
					Left:  ir.FieldReference{Path: []string{"price"}},
					Right: ir.FieldReference{Path: []string{"quantity"}},
				},
			},
		},
		PrimarySource: ir.PrimarySource{
			TablePath: "orders",
		},
	}

	query := ir.Query{
		Stages: []ir.Stage{
			{
				Dimensions: []string{"revenue"},
			},
		},
	}

	expected := "SELECT (\"price\" * \"quantity\") AS \"revenue\" FROM \"orders\" AS t0 GROUP BY 1"
	got, err := comp.Compile(source, query)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if got != expected {
		t.Errorf("Compile() = %v, want %v", got, expected)
	}
}

func TestCompilePostgresDialect(t *testing.T) {
	dialect := &PostgresDialect{}
	comp := &sqlCompiler{dialect: dialect}

	expr := ir.CallExpr{
		Name: "day",
		Args: []ir.Expr{ir.FieldReference{Path: []string{"order_date"}}},
	}

	expected := "DATE_PART('day', \"order_date\")"
	got, err := comp.compileExpr(expr, "", false)
	if err != nil {
		t.Fatalf("compileExpr failed: %v", err)
	}

	if got != expected {
		t.Errorf("compileExpr() = %v, want %v", got, expected)
	}
}
