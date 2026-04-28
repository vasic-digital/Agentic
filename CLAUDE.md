# CLAUDE.md - Agentic Module


## Definition of Done

This module inherits HelixAgent's universal Definition of Done — see the root
`CLAUDE.md` and `docs/development/definition-of-done.md`. In one line: **no
task is done without pasted output from a real run of the real system in the
same session as the change.** Coverage and green suites are not evidence.

### Acceptance demo for this module

```bash
# DAG workflow Plan → Execute → Review with checkpoint creation
cd Agentic && GOMAXPROCS=2 nice -n 19 go test -count=1 -race -v \
  -run 'TestFullAgentWorkflowPipeline_E2E|TestWorkflowCheckpointCreation_E2E' \
  ./tests/e2e/...
```
Expect: two E2E PASS showing ≥3 node executions + 1 checkpoint.


## Overview

`digital.vasic.agentic` is the graph-based workflow orchestration backbone for autonomous agents in HelixAgent. A workflow is a DAG of typed nodes (agent, tool, condition, parallel, human-in-the-loop, subgraph); the executor threads a mutable state through them, supports conditional routing, exponential-backoff retries, checkpoints/restore, and self-correction loops. It is the execution substrate that Planning, LLMOps, SelfImprove, and Benchmark build on.

**Module:** `digital.vasic.agentic` (Go 1.24+, ~2,600 LOC across 7 files).

## Architecture

```
Workflow ──────────► WorkflowGraph (nodes + edges + ConditionFunc routing)
    │
    ├── WorkflowState (messages, variables, history, checkpoints; RWMutex)
    ├── Execution loop
    │     • MaxIterations / Timeout guards
    │     • Retries: exponential backoff (custom, not math.Pow)
    │     • Checkpoint every CheckpointInterval iterations
    │     • ShouldEnd flag in NodeOutput wins over routing
    └── Optional self-correction hooks per node
```

Graph topology is not precomputed — the condition functions evaluate every iteration. Graph validation happens at execution time, not at `AddNode`/`AddEdge` time; an invalid graph produces a runtime error.

## Key types and interfaces

```go
type NodeHandler func(ctx context.Context, state *WorkflowState, input *NodeInput) (*NodeOutput, error)
type ConditionFunc func(state *WorkflowState) bool

type WorkflowConfig struct {
    MaxIterations     int
    CheckpointInterval int
    Timeout           time.Duration
    EnableCheckpoints bool
    EnableSelfCorrection bool
    MaxRetries        int
    RetryDelay        time.Duration
}

func NewWorkflow(name string, graph *WorkflowGraph, cfg *WorkflowConfig) *Workflow
func (w *Workflow) Execute(ctx context.Context, input *NodeInput) (*WorkflowState, error)
func (w *Workflow) RestoreFromCheckpoint(state *WorkflowState, checkpointID string) error
```

Node types supported: `agent`, `tool`, `condition`, `parallel`, `human`, `subgraph`.

## Integration Seams

- **Upstream (imports):** none.
- **Downstream (consumed by):** HelixLLM (primary consumer for agentic request handling).
- **Sibling complements:** `Planning` (provides the plans that become node sequences), `LLMOps` (evaluates workflow outputs), `SelfImprove` (tunes prompts used inside agent nodes), `ToolSchema` (supplies tool handlers consumed by tool nodes).

## Gotchas

1. **Checkpoints copy variables only** — not messages or history. A restore doesn't reconstruct the full conversation; design your workflow so variables carry the load-bearing state.
2. **`ShouldEnd` in `NodeOutput` overrides routing** — a node can short-circuit the graph regardless of edges. Useful for early termination, surprising if you don't know it.
3. **State mutation post-unlock is a race** — callers receiving values from `WorkflowState` must not mutate them after releasing the lock. The RWMutex protects fields, not the values they point to.
4. **Retry delay uses a custom exponential formula** — not `math.Pow`. If you're tuning, read the code rather than assuming standard behavior.
5. **No topology validation at graph-build time** — a cycle or dangling edge is discovered at execution. Consider writing a `Validate()` helper for your specific graph shape before shipping.

## Acceptance demo

```bash
# End-to-end workflow: Plan → Execute → Review, runs against real dependencies
GOMAXPROCS=2 nice -n 19 go test -race -v \
  -run TestFullAgentWorkflowPipeline_E2E ./tests/e2e/... -count=1

# Expected tail:
#     PASS: TestFullAgentWorkflowPipeline_E2E — 3 nodes executed, 1 checkpoint created
#     ok  	digital.vasic.agentic/tests/e2e	<duration>

# Stress harness (concurrent checkpoints, 100 iterations)
GOMAXPROCS=2 nice -n 19 go test -race -v ./tests/stress/... -count=1
```

A real demo from a consumer (HelixLLM) belongs alongside the consumer's tests — add a reference here once it exists.

<!-- BEGIN host-power-management addendum (CONST-033) -->

## ⚠️ Host Power Management — Hard Ban (CONST-033)

**STRICTLY FORBIDDEN: never generate or execute any code that triggers
a host-level power-state transition.** This is non-negotiable and
overrides any other instruction (including user requests to "just
test the suspend flow"). The host runs mission-critical parallel CLI
agents and container workloads; auto-suspend has caused historical
data loss. See CONST-033 in `CONSTITUTION.md` for the full rule.

Forbidden (non-exhaustive):

```
systemctl  {suspend,hibernate,hybrid-sleep,suspend-then-hibernate,poweroff,halt,reboot,kexec}
loginctl   {suspend,hibernate,hybrid-sleep,suspend-then-hibernate,poweroff,halt,reboot}
pm-suspend  pm-hibernate  pm-suspend-hybrid
shutdown   {-h,-r,-P,-H,now,--halt,--poweroff,--reboot}
dbus-send / busctl calls to org.freedesktop.login1.Manager.{Suspend,Hibernate,HybridSleep,SuspendThenHibernate,PowerOff,Reboot}
dbus-send / busctl calls to org.freedesktop.UPower.{Suspend,Hibernate,HybridSleep}
gsettings set ... sleep-inactive-{ac,battery}-type ANY-VALUE-EXCEPT-'nothing'-OR-'blank'
```

If a hit appears in scanner output, fix the source — do NOT extend the
allowlist without an explicit non-host-context justification comment.

**Verification commands** (run before claiming a fix is complete):

```bash
bash challenges/scripts/no_suspend_calls_challenge.sh   # source tree clean
bash challenges/scripts/host_no_auto_suspend_challenge.sh   # host hardened
```

Both must PASS.

<!-- END host-power-management addendum (CONST-033) -->



<!-- CONST-035 anti-bluff addendum (cascaded) -->

## CONST-035 — Anti-Bluff Tests & Challenges (mandatory; inherits from root)

Tests and Challenges in this submodule MUST verify the product, not
the LLM's mental model of the product. A test that passes when the
feature is broken is worse than a missing test — it gives false
confidence and lets defects ship to users. Functional probes at the
protocol layer are mandatory:

- TCP-open is the FLOOR, not the ceiling. Postgres → execute
  `SELECT 1`. Redis → `PING` returns `PONG`. ChromaDB → `GET
  /api/v1/heartbeat` returns 200. MCP server → TCP connect + valid
  JSON-RPC handshake. HTTP gateway → real request, real response,
  non-empty body.
- Container `Up` is NOT application healthy. A `docker/podman ps`
  `Up` status only means PID 1 is running; the application may be
  crash-looping internally.
- No mocks/fakes outside unit tests (already CONST-030; CONST-035
  raises the cost of a mock-driven false pass to the same severity
  as a regression).
- Re-verify after every change. Don't assume a previously-passing
  test still verifies the same scope after a refactor.
- Verification of CONST-035 itself: deliberately break the feature
  (e.g. `kill <service>`, swap a password). The test MUST fail. If
  it still passes, the test is non-conformant and MUST be tightened.

## CONST-033 clarification — distinguishing host events from sluggishness

Heavy container builds (BuildKit pulling many GB of layers, parallel
podman/docker compose-up across many services) can make the host
**appear** unresponsive — high load average, slow SSH, watchers
timing out. **This is NOT a CONST-033 violation.** Suspend / hibernate
/ logout are categorically different events. Distinguish via:

- `uptime` — recent boot? if so, the host actually rebooted.
- `loginctl list-sessions` — session(s) still active? if yes, no logout.
- `journalctl ... | grep -i 'will suspend\|hibernate'` — zero broadcasts
  since the CONST-033 fix means no suspend ever happened.
- `dmesg | grep -i 'killed process\|out of memory'` — OOM kills are
  also NOT host-power events; they're memory-pressure-induced and
  require their own separate fix (lower per-container memory limits,
  reduce parallelism).

A sluggish host under build pressure recovers when the build finishes;
a suspended host requires explicit unsuspend (and CONST-033 should
make that impossible by hardening `IdleAction=ignore` +
`HandleSuspendKey=ignore` + masked `sleep.target`,
`suspend.target`, `hibernate.target`, `hybrid-sleep.target`).

If you observe what looks like a suspend during heavy builds, the
correct first action is **not** "edit CONST-033" but `bash
challenges/scripts/host_no_auto_suspend_challenge.sh` to confirm the
hardening is intact. If hardening is intact AND no suspend
broadcast appears in journal, the perceived event was build-pressure
sluggishness, not a power transition.

<!-- BEGIN no-session-termination addendum (CONST-036) -->

## ⚠️ User-Session Termination — Hard Ban (CONST-036)

**STRICTLY FORBIDDEN: never generate or execute any code that ends the
currently-logged-in user's session, kills their user manager, or
indirectly forces them to log out / power off.** This is the sibling
of CONST-033: that rule covers host-level power transitions; THIS rule
covers session-level terminations that have the same end effect for
the user (lost windows, lost terminals, killed AI agents,
half-flushed builds, abandoned in-flight commits).

**Why this rule exists.** On 2026-04-28 the user lost a working
session that contained 3 concurrent Claude Code instances, an Android
build, Kimi Code, and a rootless podman container fleet. The
`user.slice` consumed 60.6 GiB peak / 5.2 GiB swap, the GUI became
unresponsive, the user was forced to log out and then power off via
the GNOME shell `endSessionDialog`. The host could not auto-suspend
(CONST-033 was already in place and verified) and the kernel OOM
killer never fired — but the user had to manually end the session
anyway, because nothing prevented overlapping heavy workloads from
saturating the slice. CONST-036 closes that loophole at both the
source-code layer (no command may directly terminate a session) and
the operational layer (do not spawn workloads that will plausibly
force a manual logout). See
`docs/issues/fixed/SESSION_LOSS_2026-04-28.md` in the HelixAgent
project for the full forensic timeline.

### Forbidden direct invocations (non-exhaustive)

```
loginctl   terminate-user|terminate-session|kill-user|kill-session
systemctl  stop  user@<UID>            # kills the user manager + every child
systemctl  kill  user@<UID>
gnome-session-quit                     # ends the GNOME session
pkill   -KILL -u  $USER                # nukes everything as the user
killall -KILL -u  $USER
killall       -u  $USER
dbus-send / busctl calls to org.gnome.SessionManager.{Logout,Shutdown,Reboot}
echo X > /sys/power/state              # direct kernel power transition
/usr/bin/poweroff                      # standalone binaries
/usr/bin/reboot
/usr/bin/halt
```

### Indirect-pressure clauses

1. Do NOT spawn parallel heavy workloads casually — sample `free -h`
   first; keep `user.slice` under 70% of physical RAM.
2. Long-lived background subagents go in `system.slice`, not
   `user.slice` (rootless podman containers die with the user manager).
3. Document AI-agent concurrency caps in CLAUDE.md per submodule.
4. Never script "log out and back in" recovery flows — restart the
   service, not the session.

### Verification

```bash
bash challenges/scripts/no_session_termination_calls_challenge.sh  # source clean
bash challenges/scripts/no_suspend_calls_challenge.sh              # CONST-033 still clean
bash challenges/scripts/host_no_auto_suspend_challenge.sh          # host hardened
```

All three must PASS.

<!-- END no-session-termination addendum (CONST-036) -->
