package main

// SupplyChain is the OpenRTB 2.6 source.schain object (fields we use).
type SupplyChain struct {
	Complete int               `json:"complete"`
	Nodes    []SupplyChainNode `json:"nodes"`
	Ver      string            `json:"ver"`
}

type SupplyChainNode struct {
	ASI    string `json:"asi"`
	SID    string `json:"sid"`
	HP     *int   `json:"hp,omitempty"`
	RID    string `json:"rid,omitempty"`
	Name   string `json:"name,omitempty"`
	Domain string `json:"domain,omitempty"`
}

// CheckSChain returns the publisher check for each node of the chain. In a complete chain the
// first node is the one that should be the publisher; later nodes are intermediaries.
func (i *PublisherIndex) CheckSChain(sc SupplyChain) []Status {
	out := make([]Status, len(sc.Nodes))
	for n, node := range sc.Nodes {
		out[n] = i.Lookup(node.ASI, node.SID)
	}
	return out
}

// ValidSChain reports whether sc has at least one node and every node is a publisher.
func (i *PublisherIndex) ValidSChain(sc *SupplyChain) bool {
	if sc == nil || len(sc.Nodes) == 0 {
		return false
	}
	for _, st := range i.CheckSChain(*sc) {
		if st != Publisher {
			return false
		}
	}
	return true
}
