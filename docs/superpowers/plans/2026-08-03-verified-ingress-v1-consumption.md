# Verified Ingress V1 Consumption Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Consume the frozen Connect `geyserliteabi.VerifiedIngressV1` handoff in embedded and subprocess modes without allowing GeyserLite to manufacture verified identity.

**Architecture:** `Server` owns a bounded, generation-aware ingress broker that
publishes native connection-open events, records one short-lived assignment per
handle/correlation, and publishes only opaque frames delivered by a frozen
callback or authenticated IPC session. Embedded mode binds the two frozen
native symbols and uses process-lifetime purego trampolines routed through a
per-isolate generation. Subprocess mode owns one authenticated `SOCK_SEQPACKET`
session per launch with ordered HMAC-protected packets and launch-local cleanup.

**Tech Stack:** Go 1.26, `go.minekube.com/connect/geyserliteabi` pinned to commit
`8001cda93b1d035b064aecfe0dfdeb739d527af0`, purego v0.10.2, Unix
`SOCK_SEQPACKET`, HMAC-SHA256, Task.

## Global Constraints

- Callback ABI version and subprocess frame version are exactly 1.
- Correlations are exactly 16 bytes, opaque frames are 1–4096 bytes, and expiry is positive and no more than five seconds ahead.
- GeyserLite never parses a frame into, constructs, or exposes a caller-settable verified claim; only callback/IPC delivery can publish `VerifiedFrame`.
- Unknown, duplicate, expired, mismatched, stale-generation, malformed, unauthenticated, sequence-invalid, and overflow input fails closed and clears affected pending state.
- No OAuth, credentials, production endpoints, releases, deployments, or Moxy changes.

---

### Task 1: Closed ingress broker and public API

**Files:**

- Create: `go/ingress.go`
- Create: `go/ingress_test.go`
- Modify: `go/server.go`
- Modify: `go/geyserlite.go`
- Modify: `go/go.mod`
- Modify: `go/go.sum`

**Interfaces:**

- Produces: `ConnectionOpen`, `ConnectionAssignment`, `VerifiedFrame`, `Server.ConnectionOpened`, `Server.VerifiedIngress`, and `Server.Assign`.
- Consumes: frozen bounds and return codes from `go.minekube.com/connect/geyserliteabi`.

- [ ] **Step 1: Write failing broker tests**

Cover exact correlation and five-second bounds, one assignment per
`(generation, handle, correlation)`, callback-only frame publication,
synchronous frame copying, take-once cleanup, expiry, mismatch, generation
shutdown, and bounded queue overflow.

- [ ] **Step 2: Run the focused tests and verify RED**

Run: `cd go && go test ./... -run 'Ingress|Assignment|Correlation' -count=1`

Expected: compile failure because the public types and broker do not exist.

- [ ] **Step 3: Implement the minimal broker and API**

Use private pending values and unexported delivery entrypoints. `Assign`
validates, records pending state, invokes only the active runner transport, and
deletes the pending value on every nonzero/error response.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run: `cd go && go test -race ./... -run 'Ingress|Assignment|Correlation' -count=1`

Expected: PASS with no races.

### Task 2: Embedded frozen callback ABI

**Files:**

- Create: `go/embedded_ingress.go`
- Create: `go/embedded_ingress_test.go`
- Modify: `go/embedded.go`

**Interfaces:**

- Consumes: `geyserlite_set_ingress_callbacks_v1(thread, open, verified)` and `geyserlite_assign_verified_ingress_v1(thread, handle, correlation, expires)`.
- Produces: stable callback pointers, one active `callbackGeneration`, and attached-thread assignment calls.

- [ ] **Step 1: Write failing lifecycle and bounds tests**

Invoke Go callback closures directly with valid memory and invalid
pointer/length combinations. Prove copy-before-return, nonblocking queue
overflow fatal wake, registration failure, assignment return-code rejection,
unregister-before-teardown, in-flight callback drain, and stale-generation
rejection.

- [ ] **Step 2: Run the focused tests and verify RED**

Run: `cd go && go test -race . -run 'EmbeddedIngress|CallbackGeneration' -count=1`

Expected: compile failure because callback roots and generation logic do not exist.

- [ ] **Step 3: Implement stable trampolines and generation drain**

Create purego callbacks once, validate pointers and lengths before
`unsafe.Slice`, copy synchronously, wake the supervisor on any fatal callback
condition, bind both frozen symbols, register after isolate initialization, call
assignment on a newly attached thread, unregister with null/null as a barrier,
wait for Go callbacks, then shut down and tear down.

- [ ] **Step 4: Run focused race tests and verify GREEN**

Run: `cd go && go test -race . -run 'EmbeddedIngress|CallbackGeneration' -count=10`

Expected: PASS with deterministic cleanup counts and no races.

### Task 3: Authenticated subprocess framing and launch ownership

**Files:**

- Create: `go/subprocess_ingress.go`
- Create: `go/subprocess_ingress_test.go`
- Modify: `go/subprocess.go`
- Modify: `go/subprocess_unix.go`
- Modify: `go/subprocess_other.go`

**Interfaces:**

- Produces: bootstrap `version || generation || key`; HMAC-protected assignment, ACK, and verified packets; exact sequence checking; one launch-owned reader/writer/session.
- Consumes: frozen subprocess constants from `geyserliteabi`.

- [ ] **Step 1: Write failing codec and session tests**

Cover exact bootstrap and packet bytes, MAC tampering, sequence gap/reuse,
generation mismatch, ACK handle/correlation mismatch, duplicate verified
result, length/lifetime bounds, old-generation packets, key absence from
args/environment/logs, and cleanup on every launch setup edge.

- [ ] **Step 2: Run focused tests and verify RED**

Run: `cd go && go test -race . -run 'SubprocessIngress|AuthenticatedPacket|LaunchGeneration' -count=1`

Expected: compile failure because the codec/session does not exist.

- [ ] **Step 3: Implement codec, Unix socketpair, and launch cleanup**

Use unsigned big-endian integers, HMAC-SHA256 over every packet prefix, sequence
starting at one, `SOCK_SEQPACKET`, one child `ExtraFiles` entry, immediate
parent-side child-FD close after start, and generation-local
cancellation/cleanup before restart.

- [ ] **Step 4: Run focused race tests and verify GREEN**

Run: `cd go && go test -race . -run 'SubprocessIngress|AuthenticatedPacket|LaunchGeneration' -count=10`

Expected: PASS with no descriptor or goroutine leakage.

### Task 4: Contract and repository verification

**Files:**

- Modify: `go/README.md` only if the public API needs contributor documentation.
- Modify: `AGENTS.md` only if implementation reveals durable cross-session knowledge not already captured.

- [ ] **Step 1: Verify the frozen dependency and forbidden construction boundary**

Run: `cd go && go list -m -json go.minekube.com/connect && go test ./... -run 'NeverConstructsVerified|ABIMismatch|FailureRejects' -count=1`

Expected: the module resolves to the commit-derived pseudo-version and all negative contract tests pass.

- [ ] **Step 2: Run repository checks**

Run: `task overlay:apply && task test && task lint`

Expected: all commands exit zero.

- [ ] **Step 3: Review scope and commit**

Confirm `git diff --check`, no release/deploy/credential/Moxy changes, and no parser or constructor for `VerifiedIngressV1`. Commit all task files on `fm/bedrock-option-a-geyserlite-ingress`.

## Plan Self-Review

- Coverage: embedded callbacks, assignment, correlation, lifetime, cleanup, subprocess authentication/generation/sequence, fail-closed mismatch behavior, and non-construction are each assigned a focused RED/GREEN cycle.
- Placeholder scan: no deferred implementation or unspecified error path remains.
- Type consistency: both transports publish the same `ConnectionOpen` and `VerifiedFrame` values through the single broker; only assignment transport differs.
- Scope: source-only GeyserLite consumption; Gate consumption and Java verifier production remain outside this repository change.
