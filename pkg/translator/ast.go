package translator

import (
	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

var (
	mallowLexer = lexer.MustSimple([]lexer.SimpleRule{
		{Name: "Keyword", Pattern: `\b(source|query|run|is|table|dimension|measure|join_one|join_many|on|project|aggregate|group_by|nest|not|where|order_by|limit|cast|as|number|string|boolean|timestampz|timestamp|date)\b`},
		{Name: "Ident", Pattern: "[a-zA-Z_][a-zA-Z0-9_]*|`[^`]*`"},
		{Name: "String", Pattern: `'[^']*'|"[^"]*"`},
		{Name: "Number", Pattern: `\d+(\.\d+)?`},
		{Name: "Arrow", Pattern: `->`},
		{Name: "Punct", Pattern: `[-+*/%,;().\[\]{}]|::|:|[=!<>]=?`},
		{Name: "Whitespace", Pattern: `\s+`},
	})

	Parser = participle.MustBuild[Mallow](
		participle.Lexer(mallowLexer),
		participle.Elide("Whitespace"),
		participle.Unquote("String"),
	)
)

type Mallow struct {
	Statements []*Statement `parser:"@@*"`
}

type Statement struct {
	Source *SourceDeclaration `parser:"( 'source' ':' @@ )"`
	Query  *QueryDeclaration  `parser:"| ( 'query' ':' @@ )"`
	Run    *RunDeclaration    `parser:"| ( 'run' ':' @@ )"`
}

type RunDeclaration struct {
	Def *QueryDef `parser:"@@"`
}

type SourceDeclaration struct {
	Name string      `parser:"@Ident 'is'"`
	Def  *SourceDef  `parser:"@@"`
	Body *SourceBody `parser:"( '{' @@ '}' )?"`
}

type SourceDef struct {
	Table *string `parser:"'table' '(' @String ')'"`
	Ref   *string `parser:"| @Ident"`
}

type SourceBody struct {
	Items []*SourceItem `parser:"@@*"`
}

type SourceItem struct {
	Dimensions []*Dimension `parser:"'dimension' ':' @@ ( ','? @@ )*"`
	Measures   []*Measure   `parser:"| 'measure' ':' @@ ( ','? @@ )*"`
	JoinOnes   []*Join      `parser:"| 'join_one' ':' @@ ( ','? @@ )*"`
	JoinManys  []*Join      `parser:"| 'join_many' ':' @@ ( ','? @@ )*"`
}

type Join struct {
	Name  string `parser:"@Ident"`
	HasIs bool   `parser:"( 'is' "`
	Ref   string `parser:"  @Ident "`
	HasOn bool   `parser:"  'on' "`
	Expr  *Expr  `parser:"  @@ )?"`
}

type Dimension struct {
	Name  string `parser:"@Ident"`
	HasIs bool   `parser:"( 'is' "`
	Expr  *Expr  `parser:"  @@ )?"`
}

type Measure struct {
	Name  string `parser:"@Ident"`
	HasIs bool   `parser:"( 'is' "`
	Expr  *Expr  `parser:"  @@ )?"`
}

type Expr struct {
	Comparison *Comparison `parser:"@@"`
}

type Comparison struct {
	Left  *Addition       `parser:"@@"`
	Right []*OpComparison `parser:"@@*"`
}

type OpComparison struct {
	Op    string    `parser:"@('<' | '>' | '=' | '!' '=' | '<' '=' | '>' '=')"`
	Right *Addition `parser:"@@"`
}

type Addition struct {
	Left  *Multiplication `parser:"@@"`
	Right []*OpAddition   `parser:"@@*"`
}

type OpAddition struct {
	Op    string          `parser:"@('+' | '-')"`
	Right *Multiplication `parser:"@@"`
}

type Multiplication struct {
	Left  *Unary              `parser:"@@"`
	Right []*OpMultiplication `parser:"@@*"`
}

type OpMultiplication struct {
	Op    string `parser:"@('*' | '/')"`
	Right *Unary `parser:"@@"`
}

type Unary struct {
	Op   string `parser:"[ @('-' | 'not') ]"`
	Cast *Cast  `parser:"@@"`
}

type Cast struct {
	Primary *Primary `parser:"@@"`
	Casts   []string `parser:"( '::' @('number' | 'string' | 'boolean' | 'timestamp' | 'timestampz' | 'date') )*"`
}

type Primary struct {
	Number   *float64  `parser:"  @Number"`
	String   *string   `parser:"| @String"`
	CastFunc *CastFunc `parser:"| @@"`
	SubExpr  *Expr     `parser:"| '(' @@ ')'"`
	Call     *Call     `parser:"| @@"`
	Field    *Path     `parser:"| @@"`
}

type CastFunc struct {
	Expr *Expr  `parser:"'cast' '(' @@"`
	Type string `parser:"'as' @('number' | 'string' | 'boolean' | 'timestamp' | 'timestampz' | 'date') ')'"`
}

type Path struct {
	Parts []string `parser:"@Ident ( '.' @Ident )*"`
}

type Call struct {
	Name string  `parser:"@Ident"`
	Args []*Expr `parser:"'(' ( @@ ( ',' @@ )* )? ')'"`
}

type QueryDeclaration struct {
	Name string    `parser:"@Ident 'is'"`
	Def  *QueryDef `parser:"@@"`
}

type QueryDef struct {
	Source string       `parser:"@Ident"`
	Stages []*QueryBody `parser:"( '->' '{' @@ '}' )+"`
}

type QueryBody struct {
	Items []*QueryItem `parser:"@@*"`
}

type QueryItem struct {
	Project   []*Dimension   `parser:"'project' ':' @@ ( ','? @@ )*"`
	Aggregate []*Measure     `parser:"| 'aggregate' ':' @@ ( ','? @@ )*"`
	GroupBy   []*Dimension   `parser:"| 'group_by' ':' @@ ( ','? @@ )*"`
	Nest      []*Nest        `parser:"| 'nest' ':' @@ ( ','? @@ )*"`
	Where     []*Expr        `parser:"| 'where' ':' @@ ( ','? @@ )*"`
	OrderBy   []*OrderByItem `parser:"| 'order_by' ':' @@ ( ','? @@ )*"`
	Limit     *int           `parser:"| 'limit' ':' @Number"`
}

type OrderByItem struct {
	Name      string `parser:"@Ident"`
	Direction string `parser:"[ @('asc' | 'desc' | 'ASC' | 'DESC') ]"`
}

type Nest struct {
	Name string     `parser:"@Ident 'is'"`
	Ref  *string    `parser:"( @Ident '->' )?"`
	Body *QueryBody `parser:"'{' @@ '}'"`
}
