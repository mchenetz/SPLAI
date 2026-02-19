package state

import (
	"reflect"
	"testing"
	"testing/fstest"
)

func TestListMigrationFiles(t *testing.T) {
	f := fstest.MapFS{
		"zzz.txt":              {Data: []byte("ignore")},
		"0002_indexes.sql":     {Data: []byte("--")},
		"0001_init.sql":        {Data: []byte("--")},
		"subdir/0003_more.sql": {Data: []byte("--")},
	}

	got, err := listMigrationFiles(f)
	if err != nil {
		t.Fatalf("listMigrationFiles returned error: %v", err)
	}
	want := []string{"0001_init.sql", "0002_indexes.sql"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected migration list: got=%v want=%v", got, want)
	}
}
