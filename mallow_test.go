package mallow_test

import (
	"os"
	"testing"

	mallow "github.com/galihanggara68/mallow"
)

func TestFromFile(t *testing.T) {
	// Create a temporary file
	content := `
		source: test_source is table('datamart.cc_records') {
			dimension: id
		}

		query: get_ids is test_source -> {
			group_by: id
		}
	`
	tmpfile, err := os.CreateTemp("", "mallow-test-*.mallow")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	engine := mallow.New(nil, nil)
	session := engine.FromSource(tmpfile.Name())
	sqlStr, err := session.GetSQL("get_ids")
	if err != nil {
		t.Fatalf("GetSQL failed: %v", err)
	}

	if sqlStr == "" {
		t.Fatal("Expected SQL string, got empty")
	}
}
