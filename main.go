// seller_poc builds an in-memory index asi -> PUBLISHER seller_ids from the sellers.json of
// the bidder's CTV suppliers (plus one level of INTERMEDIARY / BOTH sellers), and measures
// how much heap the index takes.
//
//	go run .                          fetch everything, build the index, print its heap size
//	go run . -save publishers.json    same, and save the collected data
//	go run . -load publishers.json    skip fetching (also reads check_sellers.py's publishers_by_asi.json)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Mirrors the suppliers with enabledForCTV: true in bidder/domain/suppliers.go (minus Google AdX).
// Hosts marked "verify" are best guesses.
var ctvSuppliers = []Supplier{
	{"adprime", "x.adprime.com"},                 // verify
	{"adxopenrtb", "realtimebidding.google.com"}, // skipped: too many entries
	{"indexexchange", "indexexchange.com"},
	{"inmobi", "inmobi.com"},
	{"magnite", "magnite.com"},
	{"smaato", "smaato.com"},
	{"sovrn", "lijit.com"}, // verify
}

var skipHosts = map[string]bool{
	"google.com":                 true,
	"realtimebidding.google.com": true,
}

func main() {
	workers := flag.Int("workers", 16, "concurrent sellers.json requests")
	timeout := flag.Duration("timeout", 30*time.Second, "timeout per sellers.json request")
	load := flag.String("load", "", "read asi -> publisher seller_ids from this JSON file instead of fetching")
	save := flag.String("save", "", "write the collected asi -> publisher seller_ids to this JSON file")
	demoLog := flag.String("demo-log", "", "read bid requests from this log file or directory and validate schain")
	flag.Parse()

	var data map[string][]string
	if *load != "" {
		b, err := os.ReadFile(*load)
		if err != nil {
			log.Fatal(err)
		}
		if err := json.Unmarshal(b, &data); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("loaded %d asi from %s\n", len(data), *load)
	} else {
		var files []FileResult
		data, files = Collect(context.Background(), NewFetcher(*timeout), ctvSuppliers, skipHosts, *workers)
		report(files)
	}
	if *save != "" {
		b, _ := json.Marshal(data)
		if err := os.WriteFile(*save, b, 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("saved to %s\n", *save)
	}

	// Heap measurement: `data` stays alive on both sides, so the delta is the index alone.
	before := heapAfterGC()
	idx := NewPublisherIndex(data)
	after := heapAfterGC()
	size := int64(after) - int64(before)

	rawBytes := 0
	for _, ids := range data {
		for _, id := range ids {
			rawBytes += len(id)
		}
	}
	fmt.Println("\n=== PublisherIndex (map[asi]map[seller_id]struct{}) ===")
	fmt.Printf("  asi:                   %d (+%d aliases)\n", idx.ASIs(), idx.Aliases())
	fmt.Printf("  publisher seller_ids:  %d\n", idx.IDs())
	fmt.Printf("  raw seller_id bytes:   %s\n", human(int64(rawBytes)))
	fmt.Printf("  heap used by index:    %s", human(size))
	if idx.IDs() > 0 {
		fmt.Printf("  (%.1f bytes per seller_id)", float64(size)/float64(idx.IDs()))
	}
	fmt.Printf("\n  Go %s\n", runtime.Version())

	if *demoLog != "" {
		demoFromLog(idx, *demoLog)
	} else {
		demo(idx, data)
	}
	runtime.KeepAlive(data)
	runtime.KeepAlive(idx)
}

func heapAfterGC() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func report(files []FileResult) {
	fmt.Printf("%-15s %-22s %9s %10s  %s\n", "supplier", "sellers.json host", "sellers", "publishers", "status")
	reachable, level1 := 0, 0
	for _, r := range files {
		status := "ok"
		if r.Err != nil {
			status = r.Err.Error()
		} else {
			reachable++
		}
		if r.Level == 1 {
			level1++
			continue
		}
		fmt.Printf("%-15s %-22s %9d %10d  %s\n", r.Supplier, r.Host, r.Sellers, len(r.Publishers), status)
	}

	fmt.Println("\n=== INTERMEDIARY / BOTH sellers per supplier ===")
	fmt.Printf("%-15s", "supplier")
	for _, c := range followCols {
		fmt.Printf(" %11s", c)
	}
	fmt.Println()
	var tot [len(followCols)]int
	for _, r := range files {
		if r.Level != 0 || r.Err != nil {
			continue
		}
		fmt.Printf("%-15s", r.Supplier)
		for i, n := range r.followStats() {
			fmt.Printf(" %11d", n)
			tot[i] += n
		}
		fmt.Println()
	}
	fmt.Printf("%-15s", "sum")
	for _, n := range tot {
		fmt.Printf(" %11d", n)
	}
	fmt.Println()
	fmt.Println(`
  inter/both  INTERMEDIARY or BOTH sellers listed by the supplier
  no domain   ... without a domain (nothing to fetch)
  own host    ... pointing to the supplier itself
  domains     distinct domains among the rest
  skipped     ... skipped on purpose (Google)
  new         ... not already requested via a supplier above (sum of "new" = level-1 files)
  reachable / not reach.  status of those domains' sellers.json (a shared domain counts for each supplier)`)

	fmt.Printf("\n%d sellers.json requested (%d suppliers + %d intermediary/both domains): %d reachable, %d not\n",
		len(files), len(files)-level1, level1, reachable, len(files)-reachable)
}

// demo runs a few lookups so the index is exercised.
func demo(idx *PublisherIndex, data map[string][]string) {
	asis := make([]string, 0, len(data))
	for a, ids := range data {
		if len(ids) > 0 {
			asis = append(asis, a)
		}
	}
	if len(asis) == 0 {
		return
	}
	sort.Strings(asis)
	a := asis[0]
	sc := SupplyChain{Complete: 1, Ver: "1.0", Nodes: []SupplyChainNode{
		{ASI: a, SID: data[a][0]},
		{ASI: strings.ToUpper(a), SID: "does-not-exist"},
		{ASI: "unknown-exchange.example", SID: "1"},
		{ASI: "adprime.com", SID: "AJxF6R54a9M6CaTvK"},
	}}
	printSChain(idx, "demo schain", sc)

	hp1 := 1
	sc2 := SupplyChain{Complete: 1, Ver: "1.0", Nodes: []SupplyChainNode{
		{ASI: "ocmediasolutions.com", SID: "", HP: &hp1, RID: "bid_request_unique_id", Name: "OC Media Solutions", Domain: "ocmediasolutions.com"},
		{ASI: "smaato.com", SID: "1000105", HP: &hp1, RID: "bid_request_unique_id", Name: "Smaato", Domain: "smaato.com"},
		{ASI: "x.adprime.com", SID: "AJxF6R95a9M6CaTvK", HP: &hp1, RID: "ZSIVPBSRTF7R"},
	}}
	printSChain(idx, "demo schain 2", sc2)
}

func printSChain(idx *PublisherIndex, title string, sc SupplyChain) {
	fmt.Printf("\n=== %s ===\n", title)
	for n, st := range idx.CheckSChain(sc) {
		fmt.Printf("  node %d  asi=%-30s sid=%-20s -> %s\n", n, sc.Nodes[n].ASI, sc.Nodes[n].SID, st)
	}
}

func human(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(b)/(1<<10))
	}
	return fmt.Sprintf("%d B", b)
}
