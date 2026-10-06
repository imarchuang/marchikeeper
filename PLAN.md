# marchikeeper — ZooKeeper-inspired coordination MVP

Educational coordination service in Go. Same spirit as other `marchi*`
repos: **one learning goal per slice**, inspectable tree + txn log, HTTP
first, Docker runnable.

**Not ZooKeeper / not Curator.** We borrow the **znode data model,
sessions, ephemeral + sequential nodes, one-shot watches**, and a
simplified **Zab-style atomic broadcast** — not Jute/ZK wire protocol,
ACLs, chroot, or the full Zab recovery state machine.

Contrast with [marchiraft](../marchiraft): Raft teaches *how a log is
replicated*. ZooKeeper teaches *what you put on that log* so distributed
apps can elect leaders, lock, and discover services.

---

## Learning goal

1. ZK is a **hierarchical filesystem of small znodes**, not a general KV.
2. **zxid** totally orders updates; clients reason with `stat` (czxid,
   mzxid, version, ctime, …).
3. **Session + heartbeat:** if the session expires, **ephemeral** znodes
   disappear — that *is* the failure detector for locks/elections.
4. **Sequential** children give a total order (`/lock/guid-0000000003`)
   used by lock and leader recipes.
5. **Watches** are one-shot notifications (“tell me if this changes”),
   not a push database.
6. Writes go to the **leader** and are **broadcast**; followers may serve
   stale reads unless the client `sync`s.

**Pass bar:** implement lock / leader-election with ephemeral sequential
nodes + watches (no extra consensus API); kill the holder’s session and
show the next waiter is notified.

---

## Concepts we keep (and drop)

| ZooKeeper | marchikeeper v0 | Deferred |
|---|---|---|
| Persistent znode | `POST /znodes` with body | embedded / container types |
| Ephemeral | dies with session | ephemeral sequential (yes, v0) |
| Sequential | suffix `-%010d` | container sequential only |
| Watch | one-shot on get/children | persistent recursive watches (3.6) |
| Session | `sessionId` + timeout + ping | SASL, multi-server session move |
| Stat | czxid, mzxid, version, cversion, dataLength, numChildren | pzxid, aversion, ctime/mtime |
| multi / txn | later slice | check versions in one txn |
| Zab | leader broadcast + majority ack + commit | full recovery epochs / snapshots |
| ACL | none | world:anyone |
| Wire protocol | HTTP JSON | java client / zkCli |

**Non-goals:** Observer nodes, dynamic reconfig, quota, 4LW `ruok`,
embedded ZK in Kafka (KRaft contrast belongs in marchiq notes).

---

## Why this is not marchiraft

| | marchiraft | marchikeeper |
|---|---|---|
| State machine | flat `map[string]string` | znode tree + sessions |
| Client API | GET/SET | create / set / delete / getChildren / exists + watch |
| Failure API | none | session timeout deletes ephemeral |
| Notification | none | watches fire once |
| Consensus vocab | term, commitIndex | zxid, Zab broadcast |
| Recipes | — | lock, leader latch, membership |

v0 may **reuse Raft-shaped RPCs internally** (vote + append) if that is
faster to ship, but the *public* story and tests must be ZK: zxid, znodes,
sessions. Do not expose Raft terms on `/znodes`.

---

## Data model

```text
/                          # persistent, always exists
  app/
    workers/
      w-0000000001         # ephemeral sequential, data = {addr}
      w-0000000002
    leader                 # ephemeral, data = holder session
```

Each znode:

```text
{ path, data, ephemeral, sequential,
  stat: { czxid, mzxid, version, cversion, dataLength, numChildren, ephemeralOwner } }
```

**zxid:** 64-bit, monotonic on the leader; every mutating request gets one.
`czxid` = create, `mzxid` = last mutate.

---

## Core loop (write)

```text
Client                         Leader                         Followers
  |  create /app/leader eph      |                                |
  +----------------------------->|  session must be live          |
  |                              |  zxid++; append txn            |
  |                              |  broadcast ------------------->|
  |                              |<-- majority ack                |
  |                              |  commit: tree + fire watches   |
  |  {path, stat} <--------------+                                |
```

**Session expire:** leader (or local) notices missed pings past timeout →
txn `expireSession` → delete all ephemeralOwner=sid → watches on parents
fire (`NodeChildrenChanged` / `NodeDeleted`).

---

## On-disk layout (per node)

```text
{dataDir}/
  meta.json              # myid, votedFor / zab epoch if used
  log/
    000001.jsonl         # {zxid, type, path, data, sid, ...}
  sessions.json          # optional snapshot of live sids (v0: replay log)
```

Tree is rebuilt by replaying committed txns (v0: no fuzzy snapshot).

---

## API (HTTP)

Sessions:

| Method | Path | Purpose |
|---|---|---|
| POST | `/sessions` | `{timeoutMs}` → `{sessionId}` |
| POST | `/sessions/{id}/ping` | heartbeat |
| DELETE | `/sessions/{id}` | explicit close (delete eph) |

Znodes (`X-Session-Id` required for ephemeral ops):

| Method | Path | Purpose |
|---|---|---|
| PUT | `/znodes/{path}` | create; query `ephemeral=1&sequential=1` |
| GET | `/znodes/{path}` | get data + stat; `watch=1` |
| POST | `/znodes/{path}` | set data; `version=` for CAS |
| DELETE | `/znodes/{path}` | delete; `version=` |
| GET | `/znodes/{path}/children` | names + optional watch |
| GET | `/znodes/{path}/exists` | 200/404 + optional watch |
| GET | `/events?session=` | long-poll / SSE one-shot watch deliveries |
| POST | `/sync` | optional: wait until this replica saw latest zxid |

Internal (cluster): broadcast / vote — not for app clients.

Flags: `-id`, `-listen=:2181` analogue e.g. `:7181`, `-peers`, `-dataDir`.

---

## Recipes the demo must show

1. **Membership:** workers create ephemeral sequential under `/workers`.
2. **Leader latch:** lowest sequential child is leader; others watch the
   child immediately before them (Herd avoidance, Curator-style).
3. **Lock:** same as latch; release = delete own node or session death.

These are *client recipes*, not extra server RPCs — that is the ZK thesis.

---

## MVP slices

### Slice 0 — skeleton
One process, persistent znodes only: create/get/set/delete/children.
No cluster.

### Slice 1 — stat + version CAS
`version` on set/delete; mismatch → 409. zxid increments.

### Slice 2 — sequential
Create with `sequential=1` → path suffix. Test: 3 creates, ordered names.

### Slice 3 — sessions + ephemeral
Ping; expire deletes ephemeral; children list updates.
Test: create eph, stop pinging, node gone.

### Slice 4 — one-shot watches
`GET ?watch=1` then mutate → one event on `/events`; second mutate does
not re-fire until re-armed.
Test: watch children, sequential create, waiter wakes.

### Slice 5 — leader latch demo
`cmd/demo` elects a leader; kill session; standby becomes leader.

### Slice 6 — 3-node broadcast (optional same milestone train)
Writes on leader only; followers apply by zxid; kill leader, new leader
has committed tree. If this duplicates marchiraft too hard, **call Raft
internally** and keep ZK API — still required: failover does not drop
committed znodes.

---

## Demo (graduation)

```bash
# terminal 1 — server
go run ./cmd/marchikeeper -dataDir=./data

# terminal 2 — session A becomes leader candidate
SID=$(curl -s -X POST localhost:7181/sessions -d '{"timeoutMs":5000}' | jq -r .sessionId)
curl -X PUT "localhost:7181/znodes/election/n?ephemeral=1&sequential=1" \
  -H "X-Session-Id: $SID" -d '{"who":"A"}'

# terminal 3 — B watches /election children; close A session
curl -X DELETE localhost:7181/sessions/$SID
# B is notified and takes leadership
```

---

## Relation to siblings

| Project | Contrast |
|---|---|
| **marchiraft** | consensus engine; no tree/session/watch |
| **marchiq** | Kafka used ZK historically for membership; KRaft replaces it |
| **marchiepoch** | workflow leases vs ZK ephemeral locks |
| **marchidynamo** | no coordination primitive; LWW is not locking |

Start at **slice 0** on `feat/skeleton`.
