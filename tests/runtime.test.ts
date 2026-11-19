import { describe, expect, test } from "bun:test";
import { realExec, resolvePaths } from "../src/runtime.ts";
import { ACCOUNT_NAME_RE, RESERVED_NAMES } from "../src/constants.ts";

describe("resolvePaths", () => {
  test("derives defaults from home", () => {
    const p = resolvePaths({}, "/h");
    expect(p.home).toBe("/h");
    expect(p.claudeDir).toBe("/h/.claude");
    expect(p.claudeJson).toBe("/h/.claude.json");
    expect(p.accountsRoot).toBe("/h/.claude-accounts");
    expect(p.shimPath).toBe("/h/.claude-accounts/bin/claude");
    expect(p.realClaude).toBe("/h/.local/bin/claude");
    expect(p.t3Settings).toBe("/h/.t3/userdata/settings.json");
  });

  test("CCM_HOME overrides accountsRoot and shim location", () => {
    const p = resolvePaths({ CCM_HOME: "/x" }, "/h");
    expect(p.accountsRoot).toBe("/x");
    expect(p.shimPath).toBe("/x/bin/claude");
  });

  test("CCM_CLAUDE_BIN overrides realClaude", () => {
    expect(resolvePaths({ CCM_CLAUDE_BIN: "/b" }, "/h").realClaude).toBe("/b");
  });

  test("empty overrides are ignored", () => {
    const p = resolvePaths({ CCM_HOME: "", CCM_CLAUDE_BIN: "" }, "/h");
    expect(p.accountsRoot).toBe("/h/.claude-accounts");
    expect(p.realClaude).toBe("/h/.local/bin/claude");
  });
});

describe("constants", () => {
  test("account name regex", () => {
    expect(ACCOUNT_NAME_RE.test("work-2")).toBe(true);
    expect(ACCOUNT_NAME_RE.test("Work")).toBe(false);
    expect(ACCOUNT_NAME_RE.test("a".repeat(33))).toBe(false);
    expect(ACCOUNT_NAME_RE.test("")).toBe(false);
  });
  test("reserved names", () => {
    expect(RESERVED_NAMES.has("main")).toBe(true);
    expect(RESERVED_NAMES.size).toBe(14);
  });
});

describe("realExec", () => {
  test("captures stdout and exit code", async () => {
    const r = await realExec("/bin/echo", ["hi"]);
    expect(r).toEqual({ code: 0, stdout: "hi\n", stderr: "" });
  });
  test("passes env and reports non-zero exit", async () => {
    const r = await realExec("/bin/sh", ["-c", "echo $CCM_T; exit 3"], { env: { CCM_T: "v" } });
    expect(r.code).toBe(3);
    expect(r.stdout).toBe("v\n");
  });
});
