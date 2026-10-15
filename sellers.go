package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Seller is the subset of a sellers.json entry we need.
type Seller struct {
	SellerID   flexString `json:"seller_id"`
	SellerType string     `json:"seller_type"`
	Domain     string     `json:"domain"`
}

type sellersFile struct {
	Sellers *[]Seller `json:"sellers"`
}

// flexString accepts a JSON string or number (some sellers.json files use numeric seller_ids).
type flexString string

func (s *flexString) UnmarshalJSON(b []byte) error {
	switch {
	case len(b) > 0 && b[0] == '"':
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*s = flexString(str)
	case string(b) == "null":
		*s = ""
	default:
		*s = flexString(b)
	}
	return nil
}

const maxSellersJSONBytes = 512 << 20

// Fetcher downloads <host>/sellers.json.
type Fetcher struct {
	Client *http.Client
	URL    func(host string) string
}

func NewFetcher(timeout time.Duration) *Fetcher {
	return &Fetcher{
		Client: &http.Client{
			Timeout: timeout,
			// each host is fetched once: no idle connections left around to skew the heap measurement
			Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DisableKeepAlives: true},
		},
		URL: func(host string) string { return "https://" + host + "/sellers.json" },
	}
}

func (f *Fetcher) Fetch(ctx context.Context, host string) ([]Seller, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL(host), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sellers-json-poc/1.0")
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var file sellersFile
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxSellersJSONBytes)).Decode(&file); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if file.Sellers == nil {
		return nil, errors.New("no 'sellers' array")
	}
	return *file.Sellers, nil
}

func normalizeDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	if i := strings.IndexByte(d, '/'); i >= 0 {
		d = d[:i]
	}
	return strings.Trim(d, ".")
}

func sellerType(s Seller) string {
	t := strings.ToUpper(strings.TrimSpace(s.SellerType))
	if t == "" {
		return "UNKNOWN"
	}
	return t
}
