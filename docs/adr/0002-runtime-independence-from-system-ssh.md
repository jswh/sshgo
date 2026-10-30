# Runtime independence from system SSH

sshgo never reads `~/.ssh/config` while running. The file is read exactly once, by `config import`, and from then on sshgo's behavior is defined solely by its own metadata in `~/.sshgo_config`.

## Context

sshgo presents itself as an independent client that imports `~/.ssh/config` one time, yet its runtime behavior contradicted that. When a name was neither in local metadata nor an explicit address, sshgo silently dialed `root@<name>:22`. That is what system `ssh` does with an unconfigured name, so users reasonably concluded sshgo was reading their system config. It was not: the name was simply used as a hostname with hardcoded defaults. A typo became a 30-second connect timeout instead of "this host does not exist", and the failure surfaced as a network problem rather than a configuration problem.

Three entry points (legacy mode, `exec`, `info`) each re-implemented the fallback, and they had already drifted: legacy wrote errors to stdout, the other two to stderr, and `info` added an extra alias re-lookup.

## Decision

- **The product boundary is explicit**: sshgo is independent of system ssh at runtime. It does not parse, complete, or guess anything from `~/.ssh/config` while running. That file is a seed, consumed only by `config import`.
- **Direct Addresses are product syntax**, not a parsing leftover: input containing `@` or `:` is dialed as given and is never reported as a missing Host.
- **A bare name resolves only against local metadata.** A miss is a hard error (stderr, exit code `1`) naming the three ways out: `config import`, `config set <name> hostname <addr>`, or a direct address.
- **One resolution entry point** serves all three CLI paths and produces three typed failures — host not found, no connection details, IPv6 unsupported — so callers branch on type rather than message text, and the paths cannot drift again.
- Defaults stay `user=root`, `port=22`, matching ordinary ssh habits.

## Rejected alternatives

- **Keep reading `~/.ssh/config` at runtime.** Convenient, and closest to what users expect from `ssh`. Rejected because it makes sshgo's behavior depend on a file sshgo does not own: editing the system config would silently change sshgo, and a name absent from the system config would still have to produce a sensible error, which is the same error this decision asks for anyway. The convenience buys unpredictability.
- **Read the system config but do not guess the user.** This fixes the most visible lie (inventing `root`) while keeping runtime coupling. Rejected because it preserves the deeper problem: sshgo's host list would still be a live view of someone else's file, so `config set` metadata and `~/.sshgo_config` would not be the source of truth, and the "import once" promise would remain untrue.
- **Fall back to a direct connection for a bare name, but print a warning.** Rejected: the error is the feature. A warning followed by a 30-second timeout is still a timeout, and scripts cannot branch on a warning.

## Consequences

- `config import` is a snapshot/migration, not a sync: it copies effective values once, keeps user-owned metadata on re-import, and never deletes entries that disappeared from `~/.ssh/config`. `config remove <host>` exists so deletion has a discoverable path.
- `Source` is persisted provenance rather than a derived field, so an entry edited with `config set` no longer gets relabeled `imported`.
- `ProxyJump` is stored but not honoured; imports warn per host, and docs mark it stored-only.
- IPv6 is explicitly rejected with a pointer to a Host entry, rather than being split on the last colon into a bogus host and port.
