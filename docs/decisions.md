# Decisions log (rulings made during build)

## Phase 1
- Ruling R1: T1 symlink experiment uses throwaway CLAUDE_CONFIG_DIR, no login; if a settings save can't be triggered without login, record "inconclusive — verify in T12 live E2E" — why: avoids touching real accounts — cost if wrong: detection still exists in doctor, so low.
- Ruling R2: tests for --purge use fake exec only; never real `security delete-generic-password` — why: destructive on user keychain — cost: none.
- Ruling R3: every test uses tmp HOME/CCM_HOME and fake Exec/fetch; no test reads real ~/.claude, ~/.claude.json, Keychain, network, or ~/.t3 — why: anti-goals — cost if wrong: n/a.
- Ruling R4: typecheck via devDeps `typescript` + `@types/bun` (bun add -d) — why: plan says bunx tsc — cost: small.
- Ruling R5: user chose Go (over TS; Rust offered, Claude picked Go — stdlib covers http/json/sha/exec, faster iteration, good base for future CLI). TS scaffold removed (6d020c0). Plan rewritten for Go. Rulings R1-R3 carry over; R4 obsolete. Only dep allowed: golang.org/x/text (NFC).
- Ruling R6: commit trailers name the model that actually authored (subagent harness reminder) — accurate attribution beats plan text — cost: none.
- Ruling R7: user asked "work in multiple agents" → parallel waves, each implementer in its own git worktree, merged into feat/ccm by controller. Waves: A={T3,T4,T8,T10} B={T5,T7} C={T6} D={T9} E={T11} F={T12}. T7 decoupled from T6: Fetcher gets field `Refresh func(ctx context.Context, a registry.Account) error` (T9/T11 wire auth.Refresh). — why: user request; tasks touch disjoint files — cost if wrong: merge conflicts in go.mod only, resolved by controller.
- Task 10: done 35f0ede. Ruling R8: accept reformat of T3 settings (2-space) since backup kept and content/key order preserved — cost if wrong: cosmetic diff in user file. Ruling R9: id collision (claude-<name> taken with other homePath) → error, no overwrite — safe/careful — cost: user renames account.
- Ruling R10: merged T5 pre-fix (f0bda4f) so T6/T9/T12 can run in parallel; T5 fix round merges after its re-review; Sync/Merge signatures unchanged by fix — cost if wrong: small rebase.
- Ruling R12: user "add more workers, need fast output" → T11 split: T11a internal/shellenv (use + shell-init), T11b internal/doctor (checks w/ injected deps), T11c internal/install, T11d wiring (cli dispatch, run/list/sync/remove/t3-sync/status/best wrappers, cmd/ccm) after T6+T9. Plus T7m cleanup worker (usage minors). — why: user speed request; disjoint packages — cost if wrong: interface mismatch fixed in T11d.
- T11c: complete (b3eb5b2..6ecc4a2) — merged. Ruling R13: NextSteps trailing "\n" kept (caller prints with Fprint) — reviewer ❌ was exact-text nit — cost: none. minors deferred: link-conflict check after shim write; relative symlink treated foreign; untested wrong-mode/new localBin
- Ruling R14: merged T6, T9 (pre-fix), T11a, T11b (pre-review) into feat/ccm to unblock T11d wiring — fix commits on same branches merge later.
- Ruling R15 (user concern: default browser auto-open may log in wrong account): claude opens login URL via `$BROWSER <url>` else `open <url>` (verified in binary fn d(): St(o||"open",[e])). New task T6b: during login set BROWSER=<ccm exe> + CCM_LOGIN_ACCOUNT=<name>; ccm invoked as `ccm <https-url>` with that env → open-url handler: default mode copy (pbcopy + print guidance, exit 0, never opens browser); per-account remembered `--open` mode: copy | incognito (Chrome --incognito) | chrome:<Profile dir> (open -na "Google Chrome" --args --profile-directory=<dir> <url>). Stored in registry Account.Browser. Wire into cmd/ccm after T11d.
- T11d: review (opus) → 2 Important (purge guard lexical/no symlink/ClaudeDir/Home; SIGINT swallowed at prompts). Ruling R16: fix round 1 routed to T11e agent (already editing main.go/app.go) instead of original implementer — avoids merge conflicts — cost: none. Minors folded in (128+sig exit, clamp reset, run -h usage, unknown cmd w/ corrupt registry, purge leftover reporting). Deferred minor: SIGTERM not forwarded to child.
- Ruling R17: Important#1 (T3 main homePath "") dismissed — T3 ClaudeHome.ts makeClaudeEnvironment returns base env unchanged when homePath empty (verified in T3 source earlier) — cost if wrong: claude-main thread uses wrong dir; checked in live run.
- Final fix wave: re-review clean (a90d3da..0a9663a). Ruling R19: keep SDD workspace until T12 live E2E done (it's the only open plan step) — cost: none. Deferred: install coverage 68.2% <80% rule.

## Phase 2
- Ruling P-R1: P1/P2/P3 run in parallel worktrees; P3 adds (*App) LaunchAccount wrapper in its own file calling existing runAccount; P1 changes runAccount internals to merge args, so dash inherits defaults. Controller resolves trivial dispatch.go merge conflicts — cost: small.
- Ruling P-R2: statusline install may write ~/.claude/settings.json (user-requested exception) with diff + confirm + backup.
- P1: done cf26eaf..a534d75 → review. Ruling P-R3:  reserved name accepted (18).
- Ruling/decision P-D1 (user): shared history (projects, file-history, todos, plans). P5 redirected: merge migration + status combined tokens + simplified continue.
- P2: fix round 1 done (96fb290..535380c) → re-review. Ruling P-R4: lock kept after failed refresh (throttle to 1/60s) accepted.
- P5: fix round 1/5 (1 Crit + 3 Imp + minors addressed). complete — merged 82a30da. Ruling P-R5: merge committed with internal/dash not compiling (Row.Today removed); P6 dispatched immediately to fix — cost: feat/ccm red for ~15min. minors deferred: reused-pid stale session → perpetual skip w/o reason; hardlinked child aborts merge; doctor '(nothing to merge)' wording.
- Ruling P-R6 (user: faster, multiple agents): P6 split → P6a (running agent: items 1,2,3,6), P6b (idle/low/duplicates in usage/status/statusline/doctor/login), P6c (docs). Dash will consume status.StateLabel/Window helpers after P6b merges (small follow-up if needed).
- P6a: complete (review Approved; Imp: LaunchCmd swallows config error → P6d). P6b merged be47f02 → review. Ruling P-R7: login refusal only on add (not login) so existing admin dup still works — accepted.
- P6d: merged 28597ce. Ruling P-R8 (user wants speed): P6d task review folded into the phase-2 final whole-branch review (opus) — cost: one review seat fewer on a small UI diff.

## Deferred minors (phase 2 final re-review)
- dashboard resumes recorded session id without checking transcript exists; /clear changes session id
- --resume launch records no new id (falls back to latest idle)
- continue --to duplicate check skipped when both --from/--to explicit (README overstates)
- memoEmail caches empty email for the whole --watch
- default ccm continue may call claude auth status per account (latency)
- --refresh-cache doesn't collapse duplicates lacking email in statusline cache
- reused pid makes a dead session look live (conservative)
