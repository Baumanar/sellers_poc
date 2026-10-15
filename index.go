package main

import "strings"

// PublisherIndex answers "is seller_id a PUBLISHER in asi's sellers.json?".
// asi is the advertising system domain, i.e. the host serving the sellers.json.
type PublisherIndex struct {
	m   map[string]map[string]struct{}
	ids int
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

func (i *PublisherIndex) ASIs() int { return len(i.m) }
func (i *PublisherIndex) IDs() int  { return i.ids }
