package main

import "strings"

// PublisherIndex answers "is seller_id a PUBLISHER in asi's sellers.json?".
// asi is the advertising system domain, i.e. the host serving the sellers.json.
type PublisherIndex struct {
	m       map[string]map[string]struct{}
	ids     int
	aliases int
}

// asiAliases maps an asi seen in schain nodes to the host serving its sellers.json, when the two
// differ. The alias shares the host's seller_id set, so lookups stay a single map access.
var asiAliases = map[string]string{
	"google.com":  "realtimebidding.google.com", // Google AdX: schain asi google.com
	"sovrn.com":   "lijit.com",                  // Sovrn publishes its sellers.json on lijit.com
	"magnite.com": "telaria.com",                // Magnite publishes its sellers.json on telaria.com
	"lkqd.net":    "smmato.com",                 // LKQD publishes its sellers.json on lkqd.com

}

type Status uint8

const (
	UnknownASI   Status = iota // we have no sellers.json for this asi
	Publisher                  // sid is a PUBLISHER in asi's sellers.json
	NotPublisher               // asi known, sid not listed as PUBLISHER
)

func (s Status) String() string {
	return [...]string{"unknown asi", "publisher", "not a publisher"}[s]
}

// NewPublisherIndex builds the index. Strings are cloned so the index owns its memory and
// doesn't keep the input alive.
func NewPublisherIndex(data map[string][]string) *PublisherIndex {
	idx := &PublisherIndex{m: make(map[string]map[string]struct{}, len(data))}
	for asi, ids := range data {
		set := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			set[strings.Clone(id)] = struct{}{}
		}
		idx.m[strings.Clone(normalizeDomain(asi))] = set
		idx.ids += len(set)
	}
	for alias, host := range asiAliases {
		if _, ok := idx.m[alias]; ok {
			continue // the alias has its own sellers.json: keep it
		}
		if set, ok := idx.m[host]; ok {
			idx.m[alias] = set
			idx.aliases++
		}
	}
	return idx
}

func (i *PublisherIndex) Lookup(asi, sid string) Status {
	set, ok := i.m[strings.ToLower(asi)] // no allocation when asi is already lowercase
	if !ok {
		return UnknownASI
	}
	if _, ok := set[sid]; ok {
		return Publisher
	}
	return NotPublisher
}

func (i *PublisherIndex) ASIs() int    { return len(i.m) - i.aliases }
func (i *PublisherIndex) Aliases() int { return i.aliases }
func (i *PublisherIndex) IDs() int     { return i.ids }
