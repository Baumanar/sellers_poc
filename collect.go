package main

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// Supplier is an SSP and the host serving its sellers.json.
type Supplier struct {
	Name string
	Host string
}

// FileResult describes one sellers.json requested.
type FileResult struct {
	Host       string
	Supplier   string // first supplier that led to this file
	Level      int    // 0 = supplier, 1 = intermediary / both
	Err        error
	Sellers    int
	Types      map[string]int
	Publishers []string // distinct seller_ids we keep for indexing
	follow     []string // INTERMEDIARY / BOTH domains (level 0 only)

	// Level 0 only: what happened to the supplier's INTERMEDIARY / BOTH sellers.
	FollowEntries int // INTERMEDIARY / BOTH sellers listed
	NoDomain      int // ... with no domain: nothing to fetch
	SelfRef       int // ... pointing to the supplier's own host
	FollowDomains int // distinct domains among the rest
	Skipped       int // ... skipped on purpose (Google)
	FirstSeen     int // ... not already requested via an earlier supplier
	Reached       int // ... whose sellers.json was reachable
	Unreached     int // ... whose sellers.json was not reachable
}

// followCols heads the columns of followStats, in the same order.
var followCols = [...]string{"inter/both", "no domain", "own host", "domains", "skipped", "new", "reachable", "not reach."}

func (r *FileResult) followStats() [len(followCols)]int {
	return [...]int{r.FollowEntries, r.NoDomain, r.SelfRef, r.FollowDomains, r.Skipped,
		r.FirstSeen, r.Reached, r.Unreached}
}

var followTypes = map[string]bool{"INTERMEDIARY": true, "BOTH": true}

// Collect fetches each supplier's sellers.json, then the sellers.json of every INTERMEDIARY or
// BOTH seller it lists (one level, no recursion). It returns asi -> seller_ids for
// every reachable file, plus a report of every file requested.
func Collect(ctx context.Context, f *Fetcher, suppliers []Supplier, skip map[string]bool,
	workers int) (map[string][]string, []FileResult) {

	results := map[string]*FileResult{}

	level0 := make([]FileResult, len(suppliers))
	for i, s := range suppliers {
		level0[i] = FileResult{Host: normalizeDomain(s.Host), Supplier: s.Name, Level: 0}
	}
	fetchAll(ctx, f, level0, workers, true)

	var level1 []FileResult
	queued := map[string]bool{}
	for i := range level0 {
		r := &level0[i]
		results[r.Host] = r
		queued[r.Host] = true
	}
	for i := range level0 {
		r := &level0[i]
		r.FollowDomains = len(r.follow)
		for _, d := range r.follow {
			switch {
			case skip[d]:
				r.Skipped++
			case !queued[d]:
				queued[d] = true
				r.FirstSeen++
				level1 = append(level1, FileResult{Host: d, Supplier: r.Supplier, Level: 1})
			}
		}
	}
	fetchAll(ctx, f, level1, workers, false)

	for i := range level1 {
		results[level1[i].Host] = &level1[i]
	}
	for i := range level0 {
		r := &level0[i]
		for _, d := range r.follow {
			if skip[d] {
				continue
			}
			if results[d].Err == nil {
				r.Reached++
			} else {
				r.Unreached++
			}
		}
		r.follow = nil
	}

	all := append(level0, level1...)
	data := make(map[string][]string, len(all))
	for _, r := range all {
		if r.Err == nil {
			data[r.Host] = r.Publishers
		}
	}
	return data, all
}

// fetchAll fetches the files concurrently and keeps only what we need from each one,
// so the full seller lists are garbage as soon as a file has been processed.
func fetchAll(ctx context.Context, f *Fetcher, files []FileResult, workers int, follow bool) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				r := &files[i]
				sellers, err := f.Fetch(ctx, r.Host)
				if err != nil {
					r.Err = err
					continue
				}
				extract(r, sellers, follow)
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

func extract(r *FileResult, sellers []Seller, follow bool) {
	r.Sellers = len(sellers)
	r.Types = map[string]int{}
	pubs := map[string]struct{}{}
	targets := map[string]struct{}{}
	for _, s := range sellers {
		t := sellerType(s)
		r.Types[t]++
		if id := strings.TrimSpace(string(s.SellerID)); id != "" {
			pubs[id] = struct{}{}
		}
		switch {
		case follow && followTypes[t]:
			r.FollowEntries++
			switch d := normalizeDomain(s.Domain); {
			case d == "":
				r.NoDomain++
			case d == r.Host:
				r.SelfRef++
			default:
				targets[d] = struct{}{}
			}
		}
	}
	r.Publishers = sortedKeys(pubs)
	r.follow = sortedKeys(targets)
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, strings.Clone(k)) // detach from the decoded JSON
	}
	sort.Strings(out)
	return out
}
