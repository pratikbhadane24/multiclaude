import { join } from "node:path";

export interface Paths {
  readonly home: string;
  readonly claudeDir: string;
  readonly claudeJson: string;
  readonly accountsRoot: string;
  readonly shimPath: string;
  readonly realClaude: string;
  readonly t3Settings: string;
}

export type Env = Record<string, string | undefined>;

export function resolvePaths(env: Env, home: string): Paths {
  const accountsRoot = env.CCM_HOME || join(home, ".claude-accounts");
  return {
    home,
    claudeDir: join(home, ".claude"),
    claudeJson: join(home, ".claude.json"),
    accountsRoot,
    shimPath: join(accountsRoot, "bin", "claude"),
    realClaude: env.CCM_CLAUDE_BIN || join(home, ".local", "bin", "claude"),
    t3Settings: join(home, ".t3", "userdata", "settings.json"),
  };
}

export interface ExecOptions {
  env?: Record<string, string>;
  inherit?: boolean;
}

export interface ExecResult {
  code: number;
  stdout: string;
  stderr: string;
}

export interface Exec {
  (cmd: string, args: string[], opts?: ExecOptions): Promise<ExecResult>;
}

export const realExec: Exec = async (cmd, args, opts = {}) => {
  const proc = Bun.spawn([cmd, ...args], {
    env: opts.env ? { ...process.env, ...opts.env } : process.env,
    stdin: opts.inherit ? "inherit" : "ignore",
    stdout: opts.inherit ? "inherit" : "pipe",
    stderr: opts.inherit ? "inherit" : "pipe",
  });
  const [stdout, stderr, code] = await Promise.all([
    opts.inherit ? "" : new Response(proc.stdout as ReadableStream).text(),
    opts.inherit ? "" : new Response(proc.stderr as ReadableStream).text(),
    proc.exited,
  ]);
  return { code, stdout, stderr };
};

export type Fetch = typeof fetch;

export interface Runtime {
  paths: Paths;
  exec: Exec;
  fetch: Fetch;
  now: () => number;
  log: (m: string) => void;
  warn: (m: string) => void;
}
