# marchikeeper

Educational ZooKeeper-inspired coordination service. Hierarchical znodes,
HTTP JSON, one learning slice at a time.

**Not ZooKeeper.** No Jute wire protocol, ACLs, or full Zab recovery.

## Run

```bash
go run ./cmd/marchikeeper -listen=:7181
```

Docker:

```bash
docker build -t marchikeeper .
docker run --rm -p 7181:7181 marchikeeper
```

## Slice 0 — persistent znodes

```bash
curl -s localhost:7181/healthz
curl -s -X PUT localhost:7181/znodes/app -d '{"k":1}'
curl -s localhost:7181/znodes/app
curl -s -X POST localhost:7181/znodes/app -d '{"k":2}'
curl -s -X PUT localhost:7181/znodes/app/w1 -d '{}'
curl -s localhost:7181/znodes/app/children
curl -s -X DELETE localhost:7181/znodes/app/w1
```

Set/delete accept `?version=` (CAS); mismatch is HTTP 409. Responses include
`stat` (`czxid`, `mzxid`, `version`, `cversion`, …).

`PUT /znodes/{path}?sequential=1` appends `-%010d` using the parent's counter.

See [PLAN.md](PLAN.md) for later slices (sessions, watches, leader latch,
broadcast).
