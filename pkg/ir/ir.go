package ir

// DataType represents the basic types in the Mallow type system.
type DataType string

const (
	TypeString    DataType = "string"
	TypeNumber    DataType = "number"
	TypeBoolean   DataType = "boolean"
	TypeDate      DataType = "date"
	TypeTimestamp DataType = "timestamp"
	TypeUnknown   DataType = "unknown"
)

// FieldKind represents the kind of field (dimension, measure, join, nest).
type FieldKind string

const (
	KindDimension FieldKind = "dimension"
	KindMeasure   FieldKind = "measure"
	KindJoin      FieldKind = "join"
	KindJoinOne   FieldKind = "join_one"
	KindJoinMany  FieldKind = "join_many"
	KindNest      FieldKind = "nest"
)

// FieldDef represents a field in a SourceDef.
type FieldDef struct {
	Kind       FieldKind  `json:"kind"`
	Name       string     `json:"name"`
	As         string     `json:"as,omitempty"`
	Type       DataType   `json:"type,omitempty"`
	JoinSource *SourceDef `json:"joinSource,omitempty"`
	JoinOn     Expr       `json:"joinOn,omitempty"`
	Expression string     `json:"expression,omitempty"`
	Expr       Expr       `json:"expr,omitempty"`
	NestedDef  *Query     `json:"nestedDef,omitempty"`
}

// ActiveName returns the active name of the field (alias if present, otherwise name).
func ActiveName(f FieldDef) string {
	if f.As != "" {
		return f.As
	}
	return f.Name
}

// SourceDef is the root container for a table or query result.
type SourceDef struct {
	Name          string              `json:"name"`
	Fields        map[string]FieldDef `json:"fields"`
	PrimarySource PrimarySource       `json:"primarySource"`
}

// PrimarySource contains information about the underlying table or SQL.
type PrimarySource struct {
	TablePath string `json:"tablePath,omitempty"`
	SQL       string `json:"sql,omitempty"`
}

// Query represents a sequence of query stages (pipeline).
type Query struct {
	Name       string  `json:"name,omitempty"`
	SourceName string  `json:"sourceName,omitempty"`
	Stages     []Stage `json:"stages"`
}

// Stage represents a single reduce or project operation in a query.
type Stage struct {
	Dimensions   []string            `json:"dimensions"`
	Measures     []string            `json:"measures"`
	Nests        []string            `json:"nests,omitempty"`
	Filters      []Expr              `json:"filters,omitempty"`
	InlineFields map[string]FieldDef `json:"inlineFields,omitempty"`
	OrderBy      []string            `json:"orderBy,omitempty"`
	Limit        *int                `json:"limit,omitempty"`
}
