# seller_poc

Proof of concept for validating OpenRTB `schain` against sellers.json.

It fetches the sellers.json of the bidder's CTV suppliers (plus one level of the
INTERMEDIARY / BOTH sellers they list), builds an in-memory index
`asi -> PUBLISHER seller_ids`, measures how much heap that index takes, and checks
supply chains against it.

## Requirements

- Go 1.27+ (see `go.mod`)
- Network access, unless you use `-load`

## Running

```sh
# Fetch every sellers.json, build the index, print its heap size and a demo schain check
go run .

# Same, and save the collected data for later runs
go run . -save publishers.json

# Skip fetching and use previously saved data (a snapshot is committed: publishers.json)
go run . -load publishers.json

# Validate the schain of bid requests from a log file, or every *.log file in a directory
go run . -load publishers.json -demo-log path/to/requests.log
go run . -load publishers.json -demo-log path/to/logs/
```

### Flags

| Flag        | Default | Description                                                              |
|-------------|---------|--------------------------------------------------------------------------|
| `-workers`  | `16`    | Concurrent sellers.json requests                                         |
| `-timeout`  | `30s`   | Timeout per sellers.json request                                         |
| `-load`     |         | Read `asi -> publisher seller_ids` from this JSON file instead of fetching |
| `-save`     |         | Write the collected `asi -> publisher seller_ids` to this JSON file      |
| `-demo-log` |         | Read bid requests from this log file or directory and validate schain    |

`-load` expects a JSON object mapping each asi (domain) to its PUBLISHER seller_ids:

```json
{"152media.info": ["141", "1475077", "..."]}
```

### Log format for `-demo-log`

One OpenRTB bid request per line (JSON). The schain is read from
`source.ext.schain`. A chain is valid when it has at least one node and every node's
`(asi, sid)` is a PUBLISHER in that asi's sellers.json. Lines that fail to decode are
counted as invalid and printed. The run ends with the validated / invalid totals.

## Output

When fetching, the tool first prints:

- one row per supplier: sellers.json host, number of sellers, number of publishers, status
- a breakdown of each supplier's INTERMEDIARY / BOTH sellers (with or without a domain,
  pointing to the supplier itself, skipped, new, reachable or not)
- the total number of sellers.json requested and how many were reachable

Then, in every mode:

- index stats: number of asi, publisher seller_ids, raw bytes, heap used by the index
- either the built-in demo schains with each node's status (`publisher`, `not a publisher`,
  `unknown asi`), or the `-demo-log` results

## Configuration

The supplier list is hard-coded in `ctvSuppliers` in `main.go`. It mirrors the suppliers
with `enabledForCTV: true` in `bidder/domain/suppliers.go`. Hosts marked `verify` are best
guesses. Google hosts are skipped (`skipHosts`) because they have too many entries.

## Tests

```sh
go test ./...
```

The tests use a local `httptest` server and need no network access.
