package ir

import (
	"encoding/json"
	"testing"
)

func TestActiveName(t *testing.T) {
	tests := []struct {
		name     string
		field    FieldDef
		expected string
	}{
		{
			name: "name only",
			field: FieldDef{
				Name: "carrier",
			},
			expected: "carrier",
		},
		{
			name: "name and alias",
			field: FieldDef{
				Name: "carrier",
				As:   "c",
			},
			expected: "c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ActiveName(tt.field); got != tt.expected {
				t.Errorf("ActiveName() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestJSONSerialization(t *testing.T) {
	source := SourceDef{
		Name: "flights",
		Fields: map[string]FieldDef{
			"carrier": {
				Kind: KindDimension,
				Name: "carrier",
				Type: TypeString,
			},
			"flight_count": {
				Kind:       KindMeasure,
				Name:       "flight_count",
				Type:       TypeNumber,
				Expression: "count()",
			},
		},
		PrimarySource: PrimarySource{
			TablePath: "mallow-data.flights",
		},
	}

	// Marshal
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Unmarshal
	var unmarshaled SourceDef
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// Validate
	if unmarshaled.Name != source.Name {
		t.Errorf("unmarshaled.Name = %v, want %v", unmarshaled.Name, source.Name)
	}

	if len(unmarshaled.Fields) != len(source.Fields) {
		t.Errorf("len(unmarshaled.Fields) = %v, want %v", len(unmarshaled.Fields), len(source.Fields))
	}

	if f, ok := unmarshaled.Fields["carrier"]; !ok || f.Name != "carrier" {
		t.Errorf("carrier field not found or incorrect")
	}

	if f, ok := unmarshaled.Fields["flight_count"]; !ok || f.Expression != "count()" {
		t.Errorf("flight_count field not found or incorrect")
	}
}

func TestStructuralValidation(t *testing.T) {
	// Create a nested structure: flights -> carriers -> regions
	regions := &SourceDef{
		Name: "regions",
		Fields: map[string]FieldDef{
			"id":   {Kind: KindDimension, Name: "id", Type: TypeNumber},
			"name": {Kind: KindDimension, Name: "name", Type: TypeString},
		},
		PrimarySource: PrimarySource{TablePath: "data.regions"},
	}

	carriers := &SourceDef{
		Name: "carriers",
		Fields: map[string]FieldDef{
			"code": {Kind: KindDimension, Name: "code", Type: TypeString},
			"region": {
				Kind:       KindJoin,
				Name:       "region",
				JoinSource: regions,
			},
		},
		PrimarySource: PrimarySource{TablePath: "data.carriers"},
	}

	flights := SourceDef{
		Name: "flights",
		Fields: map[string]FieldDef{
			"carrier_code": {Kind: KindDimension, Name: "carrier_code", Type: TypeString},
			"carrier": {
				Kind:       KindJoin,
				Name:       "carrier",
				JoinSource: carriers,
			},
		},
		PrimarySource: PrimarySource{TablePath: "data.flights"},
	}

	// Traverse the tree
	if flights.Name != "flights" {
		t.Errorf("expected flights, got %s", flights.Name)
	}

	carrierJoin, ok := flights.Fields["carrier"]
	if !ok || carrierJoin.Kind != KindJoin {
		t.Fatal("carrier join not found")
	}

	regionJoin, ok := carrierJoin.JoinSource.Fields["region"]
	if !ok || regionJoin.Kind != KindJoin {
		t.Fatal("region join not found")
	}

	if regionJoin.JoinSource.Name != "regions" {
		t.Errorf("expected regions, got %s", regionJoin.JoinSource.Name)
	}

	regionName, ok := regionJoin.JoinSource.Fields["name"]
	if !ok || regionName.Type != TypeString {
		t.Errorf("region name field not found or incorrect type")
	}
}
