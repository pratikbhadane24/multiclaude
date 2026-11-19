export const ACCOUNT_NAME_RE = /^[a-z0-9-]{1,32}$/;

export const RESERVED_NAMES: ReadonlySet<string> = new Set([
  "main",
  "add",
  "login",
  "list",
  "run",
  "best",
  "use",
  "status",
  "sync",
  "t3-sync",
  "doctor",
  "remove",
  "shell-init",
  "help",
]);

export const SHARED_ITEMS: readonly string[] = [
  "CLAUDE.md",
  "settings.json",
  "rules",
  "skills",
  "plugins",
  "scripts",
  "agents",
  "commands",
  "hooks",
  "output-styles",
  "keybindings.json",
];

export const USAGE_URL = "https://api.anthropic.com/api/oauth/usage";
export const USAGE_BETA = "oauth-2025-04-20";
export const KEYCHAIN_BASE = "Claude Code-credentials";
