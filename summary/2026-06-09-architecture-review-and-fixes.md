# Session summary — architecture review & remediation

Date: 2026-06-09
Branch: `main` (commits `183e682..ef8f5ef`, all local/unpushed)
Base: `394ef3d` (pre-session main tip)

## What happened

1. **Architecture review** of nxterm/nxtermd (code-only, no docs) via four
   parallel subsystem reviewers — server, TUI client, `pkg/te` emulator,
   protocol/transport. Produced `doc/project/fable-review-items.md`: 69 itemized
   findings with 1–10 severity ratings, per-subsystem grades, and strengths.
   Overall grade B−/C+: strong actor/backpressure/PTY engineering, weak spots in
   security, choreography-not-structure invariants, emulator chunk-boundary/
   resource handling, and silent input loss.

2. **Crash fixes** (the panic/fatal-grade items), then **rebased onto `main`**
   (the fixes were authored on `nx3`; verified zero file overlap with the nx3
   feature commits, cherry-picked onto `main`, reordered doc → fixes → closure,
   and rewrote the closure doc's commit-hash references to the new hashes).

3. **Corruption fixes** (`pkg/te` data integrity).

4. **Reliability/lifecycle batch** — iterated through the section test-first
   (failing test → fix → doc update → commit), pausing only for genuine design
   decisions.

Every fix shipped with a test verified to fail before and pass after; where the
fix's own correctness was subtle, the failing state was reproduced via a
temporary revert. Cumulative regression after each phase: `-race` unit suites
(`pkg/te`, `pkg/layer`, `internal/tui`, `internal/server`) + `make` build +
non-GUI e2e, all green. (One intermittent e2e failure was confirmed to be
parallel-load flake, not a regression.)

## Commits (oldest → newest)

```
183e682 docs(review): add Fable architecture review findings        ← review doc (original)
e371d5a fix(crash): repair panic/crash paths from architecture review ← #1, #13, #26, #58, scrollback nil-deref
d07f140 fix(crash): make the live-upgrade pause a true freeze (#2)
62f7c30 fix(tui): propagate quit out of reconnect/connect drain loops ← pre-existing WIP, finalized
0f871c2 docs(review): mark crash findings fixed in the architecture review
51ea1a0 fix(te): don't pollute scrollback from alt-screen/scroll-region (#5)
0118934 fix(te): carry split UTF-8 runes across Feed calls (#3)
ebed911 fix(te): bound and de-quadratic OSC/DCS string accumulation (#4)
684e581 docs(review): mark corruption findings (#3, #4, #5) fixed
e7c5f10 test(tui): guard single-dial on startup connect overlay (#11)
292835c fix(te): ESC/CAN/SUB abort in-progress control sequences (#14)
ab51706 fix(te): handle CSI colon sub-parameters (#17)
ce96b30 fix(te): maintain wide-char cell-pair invariants on overwrite (#18)
547c2d6 fix(tui): normalize kitty/modifyOtherKeys input to legacy bytes (#12)
78bb9b1 fix(te): stop Screen.Resize from wiping unrelated state (#30)
70e8d55 fix(te): resolve pending HPA before ESC-abort; #32 won't-fix
16bc493 fix(te): wrap a wide char that doesn't fit the last column (#31)
ecd6d7d fix(server): bound the mode-2026 synchronized-output batch (#25)
a8eca75 fix(server): correct overlay bookkeeping on re-register/disconnect (#28)
87130a2 fix(layer): deliver task-done notifications reliably (#35)
3e163d2 fix(server): negotiate compression off the accept loop (#27)
ef8f5ef fix(tui): propagate window resize to inactive tabs (#22)
```

## Findings addressed

### Crash / panic grade — all fixed
- **#1** `CSI 8;t` resize-shrink panic (every attached client) — `Screen.Resize`
  now truncates the buffer + clamps the cursor.
- **#2** live-upgrade pause consumed an arbitrary client request (concurrent-map
  fatal + wedged client) — dedicated resume channel; the freeze is now real.
- **#13** event-loop panic left the terminal in alt-screen/mouse-on — `recover`
  net restores terminal state then re-panics. *(SIGTSTP aside still open.)*
- **#26** upgrade-recv FD/spec-mismatch panic + `MSG_CTRUNC` + rollback FD leak.
  *(253-FD single-`sendmsg` `SCM_RIGHTS` cap still open.)*
- **#58** `Server.Close` send-on-closed-channel race — close only `done`.
- scrollback nil-deref (`handleSyncChunk`) — nil-guard.

### Data corruption — all fixed
- **#5** alt-screen/scroll-region scrolling polluted primary scrollback and the
  `TotalAdded` sync counter — only full-screen primary scrolling accrues.
- **#3** split-UTF-8 runes mangled across `Feed` calls — `Stream`/`ByteStream`
  carry the partial trailing bytes. (Latent lib bug; server PTY path was already
  guarded by `sequenceSafe`.)
- **#4** quadratic/unbounded OSC/DCS accumulation (CPU/memory DoS) — O(n)
  `strings.Builder`, 512 KB cap.

### Reliability / lifecycle (#11–#35)
- **Fixed:** #12 (kitty/modifyOtherKeys → legacy input), #14 (ESC/CAN/SUB abort
  sequences), #17 (CSI colon sub-parameters / colon truecolor), #18 (wide-char
  overwrite), #27 (compression off the accept loop), #28 (overlay
  re-register/disconnect bookkeeping), #30 (resize stops wiping palette/cursor-
  style/etc.), #31 (wide char wraps instead of clipping).
- **Partial (core fixed, remainder noted):** #22 (resize → inactive tabs; multi-
  *session* case open), #25 (mode-2026 batch bounded; idle flush-timeout open),
  #35 (taskDoneMsg delivered reliably; Cancel/CheckFilters notes open).
- **Resolved without a code fix:** #11 (startup double-dial — *not reproduced* on
  current `main`, regression-guarded), #32 (`'` HPA/DECIC ambiguity — *won't
  fix*: conformance requires HPA dispatch at chunk-end; investigation surfaced &
  fixed a real #14 regression where a pending HPA was dropped by ESC).

## Still open

- **Reliability, tractable but heavy to test:** #20 dropped-broadcast repair
  (needs an actor timer), #21 bracketed-paste-unaware input, #23 task→render data
  races (needs in-process `-race` harness — the e2e detector can't see into the
  spawned `nxterm`), #24 request timeout, #29 server-side input-drop surfacing.
- **Deferred — need a decision:** #15 outbound-drop policy, #16 event-loop↔actor
  structural deadlock (risky), #19 tree-node geometry on resize (trivial fix,
  needs an observability hook — `getRegionInfos` reads region atomics, which are
  already correct, so only the *node* is stale), #33 ssh PTY cooked-mode, #34 no
  protocol version negotiation.
- **Untouched sections:** Security cluster (#6–#10: no auth on tcp/ws, MITM-able
  dssh host key, unhardened unix socket, arbitrary-binary `upgrade_to`),
  emulator fidelity gaps (#36 BCE, #37 ACS-in-UTF-8, #38–#43, …), and the
  severity-1–2 nits (#51–#69).

The security cluster (#6–#10) is the highest-value untouched work: those are the
default-reachable control surface and the main reason the protocol/transport
grade is held at C+.

## Notes for next time

- The `pkg/te` conformance suite follows **pyte** semantics in a couple of edge
  cases that diverge from xterm (the `'` HPA/DECIC ambiguity in #32; wide-char
  line-end cursor in #31). New emulator fixes should run the full `pkg/te` suite
  and reconcile against those tests, not just xterm behavior.
- e2e tests spawn the real `nxtermd`/`nxterm` binaries, so `go test -race ./e2e`
  does **not** instrument them — in-process tests are required to catch races
  inside those processes (relevant to #23).
- `git push` from this sandbox needs `GIT_SSH_COMMAND="ssh -F /dev/null"`.
- Status of every item is tracked inline in `doc/project/fable-review-items.md`
  (Status column + a "Remediation status" table near the top).
