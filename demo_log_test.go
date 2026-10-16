package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateLogFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.log")
	if err := os.WriteFile(path, []byte(
		`{"id":"1","source":{"ext":{"schain":{"ver":"1.0","complete":0,"nodes":[{"asi":"x.adprime.com","sid":"AJxF6R54a9M6CaTvK","hp":1}]}}}}`+"\n"+
			`{"id":"2","source":{"ext":{"schain":{"ver":"1.0","complete":0,"nodes":[{"asi":"unknown.example","sid":"1","hp":1}]}}}}`+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := NewPublisherIndex(map[string][]string{
		"x.adprime.com": []string{"AJxF6R54a9M6CaTvK"},
	})
	validated, invalid, err := validateLogFile(idx, path)
	if err != nil {
		t.Fatal(err)
	}
	if validated != 1 || invalid != 1 {
		t.Fatalf("validated=%d invalid=%d", validated, invalid)
	}
}

func TestScanLogFileStats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acme-ctv__0001-0000.log")
	lines := `{"source":{"ext":{"schain":{"complete":1,"nodes":[{"asi":"a.com","sid":"1"}]}}}}
{"source":{"schain":{"complete":1,"nodes":[{"asi":"A.com","sid":"1"},{"asi":"b.com","sid":"9"}]}}}
{"source":{"ext":{"schain":{"complete":0,"nodes":[{"asi":"a.com","sid":"2"},{"asi":"zz.com","sid":"1"}]}}}}
{"source":{"ext":{}}}
{"source":{"ext":{"schain":{"nodes":[]}}}}
not json
`
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := NewPublisherIndex(map[string][]string{"a.com": {"1"}, "b.com": {"9"}})
	st := newLogStats()
	if err := scanLogFile(idx, path, st); err != nil {
		t.Fatal(err)
	}
	if supplierFromPath(path) != "acme-ctv" {
		t.Fatalf("supplier = %q", supplierFromPath(path))
	}
	if st.Requests != 6 || st.Valid != 2 || st.Invalid != 4 || st.NoSchain != 1 || st.EmptySchain != 1 ||
		st.DecodeErrors != 1 || st.Complete != 2 || st.Nodes != 5 {
		t.Fatalf("stats = %+v", st)
	}
	if st.NodeStatus != [3]int{1, 3, 1} { // unknown, listed, not listed
		t.Fatalf("node status = %v", st.NodeStatus)
	}
	if st.UnknownASI["zz.com"] != 1 || st.NotListed["a.com"] != 1 {
		t.Fatalf("asi maps: %v %v", st.UnknownASI, st.NotListed)
	}
}
