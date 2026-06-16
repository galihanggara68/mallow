package ir

// Expr is the interface for all expression types.
type Expr interface {
	isExpr()
}

// BinaryOp represents binary operators.
type BinaryOp string

const (
	OpAdd   BinaryOp = "+"
	OpSub   BinaryOp = "-"
	OpMul   BinaryOp = "*"
	OpDiv   BinaryOp = "/"
	OpEq    BinaryOp = "="
	OpNotEq BinaryOp = "!="
	OpGt    BinaryOp = ">"
	OpGtEq  BinaryOp = ">="
	OpLt    BinaryOp = "<"
	OpLtEq  BinaryOp = "<="
	OpAnd   BinaryOp = "AND"
	OpOr    BinaryOp = "OR"
)

// BinaryExpr represents an expression with two operands and an operator.
type BinaryExpr struct {
	Op    BinaryOp `json:"op"`
	Left  Expr     `json:"left"`
	Right Expr     `json:"right"`
}

func (e BinaryExpr) isExpr() {}

// UnaryOp represents unary operators.
type UnaryOp string

const (
	OpNot UnaryOp = "NOT"
	OpNeg UnaryOp = "-"
)

// UnaryExpr represents an expression with one operand and an operator.
type UnaryExpr struct {
	Op    UnaryOp `json:"op"`
	Child Expr    `json:"child"`
}

func (e UnaryExpr) isExpr() {}

// Literal represents a constant value.
type Literal struct {
	Value interface{} `json:"value"`
	Type  DataType    `json:"type"`
}

func (e Literal) isExpr() {}

// FieldReference represents a reference to a field.
type FieldReference struct {
	Path []string `json:"path"` // e.g., ["table", "field"] or ["field"]
}

func (e FieldReference) isExpr() {}

// CallExpr represents a function call.
type CallExpr struct {
	Name string `json:"name"`
	Args []Expr `json:"args"`
}

func (e CallExpr) isExpr() {}
