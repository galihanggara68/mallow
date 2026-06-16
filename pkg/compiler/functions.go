package compiler

import (
	"fmt"
)

// FunctionBlueprint defines how to translate a function call to SQL.
type FunctionBlueprint func(dialect Dialect, args []string) (string, error)

var functionRegistry = map[string]FunctionBlueprint{
	"count": func(dialect Dialect, args []string) (string, error) {
		if len(args) == 0 {
			return "COUNT(*)", nil
		}
		return fmt.Sprintf("COUNT(%s)", args[0]), nil
	},
	"sum": func(dialect Dialect, args []string) (string, error) {
		if len(args) != 1 {
			return "", fmt.Errorf("sum expects 1 argument, got %d", len(args))
		}
		return fmt.Sprintf("SUM(%s)", args[0]), nil
	},
	"avg": func(dialect Dialect, args []string) (string, error) {
		if len(args) != 1 {
			return "", fmt.Errorf("avg expects 1 argument, got %d", len(args))
		}
		return fmt.Sprintf("AVG(%s)", args[0]), nil
	},
	"day": func(dialect Dialect, args []string) (string, error) {
		if len(args) != 1 {
			return "", fmt.Errorf("day expects 1 argument, got %d", len(args))
		}
		return dialect.DatePart("DAY", args[0]), nil
	},
}

// RegisterFunction adds a new function blueprint to the registry.
func RegisterFunction(name string, blueprint FunctionBlueprint) {
	functionRegistry[name] = blueprint
}
