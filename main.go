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
	{"adprime", "adprime.com"},                   // verify
	{"adxopenrtb", "realtimebidding.google.com"}, // skipped: too many entries
	{"indexexchange", "indexexchange.com"},
	{"inmobi", "inmobi.com"},
	{"magnite", "magnite.com"}, // verify: CTV may sit under a legacy SpotX domain
	{"smaato", "smaato.com"},
	{"sovrn", "		lijit.com"}, // verify
}

var skipHosts = map[string]bool{}

func main() {
	workers := flag.Int("workers", 16, "concurrent sellers.json requests")
	timeout := flag.Duration("timeout", 30*time.Second, "timeout per sellers.json request")
	load := flag.String("load", "", "read asi -> publisher seller_ids from this JSON file instead of fetching")
	save := flag.String("save", "", "write the collected asi -> publisher seller_ids to this JSON file")
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
	fmt.Printf("  asi:                   %d\n", idx.ASIs())
	fmt.Printf("  publisher seller_ids:  %d\n", idx.IDs())
	fmt.Printf("  raw seller_id bytes:   %s\n", human(int64(rawBytes)))
	fmt.Printf("  heap used by index:    %s", human(size))
	if idx.IDs() > 0 {
		fmt.Printf("  (%.1f bytes per seller_id)", float64(size)/float64(idx.IDs()))
	}
	fmt.Printf("\n  Go %s\n", runtime.Version())

	demo(idx, data)
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
	fmt.Printf("%-15s %-35s %-5s %9s %10s  %s\n", "supplier", "sellers.json host", "level", "sellers", "publishers", "status")
	reachable := 0
	for _, r := range files {
		status := "ok"
		if r.Err != nil {
			status = r.Err.Error()
		} else {
			reachable++
		}
		if r.Level == 0 || r.Err != nil {
			fmt.Printf("%-15s %-35s %-5d %9d %10d  %s\n", r.Supplier, r.Host, r.Level, r.Sellers, len(r.Publishers), status)
		}
	}
	fmt.Printf("\n%d sellers.json requested, %d reachable, %d not (reachable level-1 files not listed above)\n",
		len(files), reachable, len(files)-reachable)
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
	}}
	fmt.Println("\n=== demo schain ===")
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
