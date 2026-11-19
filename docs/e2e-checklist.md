# Live E2E checklist

Run with the user. Fill in Result/Notes during the run.

- [ ] Install: `make build && ./bin/ccm install`, add shell-init line, new shell
  - Result:
- [ ] `ccm doctor` passes (or only expected warnings)
  - Result:
- [ ] `ccm add <second>`; user logs in; email matches
  - Result:
- [ ] Parallel: `ccm main` and `ccm <second>` in two terminals at once; `/status` shows different emails
  - Result:
- [ ] `ccm status` shows both accounts plus the `ALL` row
  - Result:
- [ ] `ccm t3-sync`, restart T3, one thread per instance works
  - Result:
- [ ] `t3 up`; open from the phone and drive a thread
  - Result:
- [ ] MCP add: `claude mcp add -s user` a dummy server in main; a new `<second>` session lists it under `/mcp`
  - Result:
- [ ] MCP remove: remove it from main; gone from `<second>` at the next launch
  - Result:
- [ ] Interactive `/config` change in an account: is `settings.json` still a symlink? (`test -L <dir>/settings.json`)
  - Result:
