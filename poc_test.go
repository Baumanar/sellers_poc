package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var fake = map[string]string{
	"smaato.com": `{"sellers":[
		{"seller_id":"1","seller_type":"PUBLISHER","domain":"pub1.com"},
		{"seller_id":2,"seller_type":"publisher"},
		{"seller_id":"1","seller_type":"PUBLISHER","domain":"pub1.com"},
		{"seller_id":"i","seller_type":"INTERMEDIARY","domain":"https://Inter1.com/"},
		{"seller_id":"b","seller_type":"BOTH","domain":"both1.com"},
		{"seller_id":"g","seller_type":"INTERMEDIARY","domain":"google.com"},
		{"seller_id":"d","seller_type":"INTERMEDIARY","domain":"dead.com"}]}`,
	"inmobi.com": `{"sellers":[{"seller_id":"9","seller_type":"PUBLISHER"},
		{"seller_id":"i","seller_type":"INTERMEDIARY","domain":"inter1.com"}]}`,
	"inter1.com": `{"sellers":[{"seller_id":"a","seller_type":"PUBLISHER"},
		{"seller_id":"x","seller_type":"INTERMEDIARY","domain":"deep.com"}]}`,
	"both1.com": `{"sellers":[{"seller_id":"z","seller_type":"PUBLISHER"}]}`,
	"deep.com":  `{"sellers":[{"seller_id":"q","seller_type":"PUBLISHER"}]}`,
	"bad.com":   `{"nope":1}`,
}

func testFetcher(t *testing.T) (*Fetcher, *[]string) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/sellers.json")
		calls = append(calls, host)
		body, ok := fake[host]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	f := NewFetcher(5 * time.Second)
	f.URL = func(host string) string { return srv.URL + "/" + host + "/sellers.json" }
	return f, &calls
}

func TestCollectAndLookup(t *testing.T) {
	f, calls := testFetcher(t)
	sup := []Supplier{{"smaato", "smaato.com"}, {"inmobi", "inmobi.com"}, {"adprime", "adprime.com"}}
	data, files := Collect(context.Background(), f, sup, skipHosts, 1)

	for _, c := range *calls {
		if c == "deep.com" || c == "google.com" {
			t.Fatalf("should not fetch %s", c)
		}
	}
	if len(files) != 6 { // 3 suppliers + inter1, both1, dead
		t.Fatalf("files = %d, want 6", len(files))
	}
	if got := strings.Join(data["smaato.com"], ","); got != "1,2" {
		t.Fatalf("smaato publishers = %q", got)
	}
	if _, ok := data["dead.com"]; ok {
		t.Fatal("unreachable file must not be in data")
	}

	idx := NewPublisherIndex(data)
	if idx.ASIs() != 4 || idx.IDs() != 5 {
		t.Fatalf("asi=%d ids=%d, want 4 and 5", idx.ASIs(), idx.IDs())
	}
	cases := []struct {
		asi, sid string
		want     Status
	}{
		{"smaato.com", "1", Publisher},
		{"SMAATO.com", "2", Publisher},
		{"smaato.com", "i", NotPublisher}, // intermediary, not a publisher
		{"inter1.com", "a", Publisher},
		{"deep.com", "q", UnknownASI}, // level 2: not fetched
		{"dead.com", "1", UnknownASI},
	}
	for _, c := range cases {
		if got := idx.Lookup(c.asi, c.sid); got != c.want {
			t.Errorf("Lookup(%s,%s) = %v, want %v", c.asi, c.sid, got, c.want)
		}
	}
}

func TestFetchErrors(t *testing.T) {
	f, _ := testFetcher(t)
	if _, err := f.Fetch(context.Background(), "bad.com"); err == nil {
		t.Error("missing sellers array should fail")
	}
	if _, err := f.Fetch(context.Background(), "nothere.com"); err == nil {
		t.Error("404 should fail")
	}
}

func BenchmarkLookup(b *testing.B) {
	data := map[string][]string{}
	for a := 0; a < 300; a++ {
		ids := make([]string, 3000)
		for i := range ids {
			ids[i] = fmt.Sprintf("%d", 100000+i)
		}
		data[fmt.Sprintf("ssp%d.com", a)] = ids
	}
	idx := NewPublisherIndex(data)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Lookup("ssp42.com", "101234")
	}
}
