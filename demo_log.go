package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type bidRequest struct {
	Source struct {
		Schain *SupplyChain `json:"schain"` // OpenRTB 2.6 location
		Ext    struct {
			Schain *SupplyChain `json:"schain"` // OpenRTB 2.5 extension
		} `json:"ext"`
	} `json:"source"`
}

func (r *bidRequest) schain() *SupplyChain {
	if r.Source.Schain != nil {
		return r.Source.Schain
	}
	return r.Source.Ext.Schain
}

// logStats aggregates the schain checks of the bid requests of one supplier.
type logStats struct {
	Files, Requests, DecodeErrors int
	NoSchain, EmptySchain         int
	Valid, Invalid                int // Invalid includes requests without schain and decode errors
	Complete                      int // schain.complete == 1
	Nodes                         int
	NodeStatus                    [3]int         // indexed by Status
	UnknownASI                    map[string]int // asi -> nodes whose asi is not in the index
	NotListed                     map[string]int // asi -> nodes whose sid is not in asi's sellers.json
}

func newLogStats() *logStats {
	return &logStats{UnknownASI: map[string]int{}, NotListed: map[string]int{}}
}

func (s *logStats) add(o *logStats) {
	s.Files += o.Files
	s.Requests += o.Requests
	s.DecodeErrors += o.DecodeErrors
	s.NoSchain += o.NoSchain
	s.EmptySchain += o.EmptySchain
	s.Valid += o.Valid
	s.Invalid += o.Invalid
	s.Complete += o.Complete
	s.Nodes += o.Nodes
	for i := range s.NodeStatus {
		s.NodeStatus[i] += o.NodeStatus[i]
	}
	for k, v := range o.UnknownASI {
		s.UnknownASI[k] += v
	}
	for k, v := range o.NotListed {
		s.NotListed[k] += v
	}
}

// supplierFromPath returns the supplier prefix of a log file name: "smaato-ctv__0108b20b-0400.log" -> "smaato-ctv".
func supplierFromPath(p string) string {
	name := filepath.Base(p)
	if i := strings.Index(name, "__"); i > 0 {
		return name[:i]
	}
	return strings.TrimSuffix(name, filepath.Ext(name))
}

func demoFromLog(idx *PublisherIndex, path string) {
	paths, err := collectLogPaths(path)
	if err != nil {
		log.Fatal(err)
	}

	perSupplier := map[string]*logStats{}
	for _, p := range paths {
		sup := supplierFromPath(p)
		st := perSupplier[sup]
		if st == nil {
			st = newLogStats()
			perSupplier[sup] = st
		}
		if err := scanLogFile(idx, p, st); err != nil {
			log.Fatal(err)
		}
	}

	suppliers := make([]string, 0, len(perSupplier))
	for s := range perSupplier {
		suppliers = append(suppliers, s)
	}
	sort.Strings(suppliers)
	total := newLogStats()
	for _, s := range suppliers {
		total.add(perSupplier[s])
	}

	fmt.Printf("\n=== schain check per supplier (%d files from %s) ===\n", total.Files, path)
	fmt.Println("\nRequests")
	fmt.Printf("%-18s %6s %9s %9s %9s %9s %9s %8s\n",
		"supplier", "files", "requests", "no schain", "complete", "valid", "invalid", "valid %")
	row := func(name string, s *logStats) {
		fmt.Printf("%-18s %6d %9d %9d %9d %9d %9d %7.1f%%\n", name, s.Files, s.Requests, s.NoSchain+s.EmptySchain,
			s.Complete, s.Valid, s.Invalid, pct(s.Valid, s.Requests))
	}
	for _, s := range suppliers {
		row(s, perSupplier[s])
	}
	row("TOTAL", total)

	fmt.Println("\nschain nodes")
	fmt.Printf("%-18s %9s %10s %9s %12s %9s %12s %9s\n",
		"supplier", "nodes", "nodes/req", "listed", "", "not listed", "", "unknown asi")
	nrow := func(name string, s *logStats) {
		withChain := s.Requests - s.NoSchain - s.EmptySchain - s.DecodeErrors
		fmt.Printf("%-18s %9d %10.2f %9d %11.1f%% %9d %11.1f%% %9d (%.1f%%)\n", name, s.Nodes,
			ratio(s.Nodes, withChain),
			s.NodeStatus[Publisher], pct(s.NodeStatus[Publisher], s.Nodes),
			s.NodeStatus[NotPublisher], pct(s.NodeStatus[NotPublisher], s.Nodes),
			s.NodeStatus[UnknownASI], pct(s.NodeStatus[UnknownASI], s.Nodes))
	}
	for _, s := range suppliers {
		nrow(s, perSupplier[s])
	}
	nrow("TOTAL", total)

	fmt.Println("\nTop asi causing failures, per supplier (nodes)")
	for _, s := range suppliers {
		st := perSupplier[s]
		fmt.Printf("  %s\n", s)
		fmt.Printf("    unknown asi (no sellers.json in the index): %s\n", top(st.UnknownASI, 5))
		fmt.Printf("    sid not in asi's sellers.json:              %s\n", top(st.NotListed, 5))
	}
	if total.DecodeErrors > 0 {
		fmt.Printf("\n%d lines could not be decoded (counted as invalid)\n", total.DecodeErrors)
	}
	fmt.Println(`
  valid       every node's sid is in its asi's sellers.json (ValidSChain)
  invalid     any other request, including those without a schain
  listed      node's sid found in its asi's sellers.json
  not listed  asi is in the index, sid is not
  unknown asi asi has no sellers.json in the index`)
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return 100 * float64(n) / float64(d)
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func top(m map[string]int, n int) string {
	if len(m) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > n {
		keys = keys[:n]
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		label := k
		if label == "" {
			label = `""`
		}
		parts[i] = fmt.Sprintf("%s=%d", label, m[k])
	}
	return strings.Join(parts, ", ")
}

func collectLogPaths(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}

	var paths []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".log") {
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	return paths, err // WalkDir visits entries in lexical order
}

// validateLogFile returns the number of valid and invalid schains in one log file.
func validateLogFile(idx *PublisherIndex, path string) (validated, invalid int, err error) {
	st := newLogStats()
	err = scanLogFile(idx, path, st)
	return st.Valid, st.Invalid, err
}

// scanLogFile checks the schain of every bid request in the file and adds the results to st.
func scanLogFile(idx *PublisherIndex, path string, st *logStats) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st.Files++

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16<<20) // bid requests can exceed the default 64KB line limit
	lineNo := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		lineNo++
		st.Requests++

		var req bidRequest
		if err := json.Unmarshal(line, &req); err != nil {
			st.DecodeErrors++
			st.Invalid++
			fmt.Printf("invalid schain file=%s line=%d reason=decode error: %v\n", path, lineNo, err)
			continue
		}
		chain := req.schain()
		switch {
		case chain == nil:
			st.NoSchain++
		case len(chain.Nodes) == 0:
			st.EmptySchain++
		default:
			if chain.Complete == 1 {
				st.Complete++
			}
			st.Nodes += len(chain.Nodes)
			for n, status := range idx.CheckSChain(*chain) {
				st.NodeStatus[status]++
				asi := strings.ToLower(chain.Nodes[n].ASI)
				switch status {
				case UnknownASI:
					st.UnknownASI[asi]++
				case NotPublisher:
					st.NotListed[asi]++
				}
			}
		}
		if idx.ValidSChain(chain) {
			st.Valid++
		} else {
			st.Invalid++
		}
	}
	return sc.Err()
}
