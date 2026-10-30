# ProxyJump as a chain of sshgo Hosts

`ProxyJump` is honoured: a Host is reached through one or more Jump Hosts, each of which is an ordinary sshgo Host with its own credentials. The chain is resolved at parse time, has no depth limit, and rejects loops before anything is dialled.

## Context

`proxy-jump` was a field that lied. `config import` stored `ProxyJump` from `~/.ssh/config`, the docs taught `sshgo config set <host> proxy-jump <value>`, and connections ignored it. Importing even printed "imported, but jump-host connections are not supported yet" per host. A user with an internal machine reachable only through a bastion got a configuration that looked correct, a command that ran, and a network error that pointed at the wrong thing.

Two constraints shaped the fix.

First, **Runtime Independence** (ADR-0002) already decided that sshgo does not read `~/.ssh/config` at runtime and that a bare name resolves only against sshgo's own metadata. A jump host therefore cannot be an arbitrary hostname parsed out of an ssh config: it has to be something sshgo already manages. That is a consequence of an existing decision, not a new principle.

Second, the connection logic had been collapsed into a single dial point (`internal/connect`) precisely so that "given a Host, produce an `ssh.Client`" exists once. A chain is a sequence of those, so it belongs in the same place; putting it anywhere else would have re-created the duplication the refactor removed.

## Decision

- **A Jump Host is an sshgo Host.** The proxy-jump value is resolved by the same addressing grammar as every other positional argument: Host name, Alias, or Direct Address. `none` and the empty value mean a direct connection (`none` is ssh's spelling and arrives through import). `ssh://` URIs and comma-separated multi-hop syntax are rejected: they are ssh's grammar, not sshgo's, and there is exactly one addressing grammar in this project.
- **Depth is unlimited, and loops are detected at parse time.** Resolution walks outward from the target and stops at the first Host with no jump host; the hops are then dialled outermost first. A cycle is reported as a path (`db1 -> bastion -> db1`) without dialling, so `info` reaches the same verdict as a connection instead of describing an unreachable Host as healthy.
- **Each hop is an ordinary Host, and credentials never cross hops.** A hop's user, port, IdentityFile, and saved password come from that hop's own metadata and its own Credential Keys. Nothing is inherited from the target, and nothing is handed back to it. The SSH agent is the only shared thing, because it belongs to the user rather than to any Host.
- **The sudo password never reaches a Jump Host.** It is resolved only after the chain is up, and it is used only as the target session's `sudo -S` stdin. It is never part of any hop's `ssh.ClientConfig.Auth`. The two exits are separate in the `Authenticator` interface for exactly this reason.
- **Parse time is the only authority on a chain.** `config set proxy-jump` accepts forward references (create the target first, the bastion later) and `config import` does not validate chains at all; import's old warning was deleted rather than reworded, because a broken chain at import time is usually just an unfinished one.
- **`exec` never prompts for SSH authentication, on any hop.** A hop with no credential fails immediately, names the hop, and points at `sshgo config set-password <hop>`. Interactive login keeps prompting, and keeps its existing "save password?" flow. This asymmetry is deliberate: a CI job cannot answer a prompt.

## Rejected alternatives

- **Accept any hostname as a jump host.** Convenient, and closest to what ssh writes. Rejected because it contradicts Runtime Independence: sshgo would be dialling a name it does not own and cannot describe, and a typo in a bastion name would become a connect timeout instead of a chain error naming the entry to create.
- **Use `ProxyCommand` instead.** It is the more general mechanism in ssh, and it would cover cases ProxyJump cannot. Rejected because it requires shelling out, which contradicts the single-binary, no-system-ssh premise.
- **Validate the chain when `config set proxy-jump` runs.** Tempting, since it would catch typos early. Rejected because building a target before its bastion is a normal order; write-time validation would reject a legal workflow to catch a problem that resolution reports better, later, and with the full path.
- **Warn at import time instead of resolving at parse time.** Rejected: two authorities produce two answers, and the import-time answer would be noise for every forward reference.
- **Share one credential across the whole chain.** It would make three-hop setups pleasant to configure. Rejected outright: it hands the target's password to every bastion in the path, which is the security boundary this design exists to hold.

## Consequences

- **Host keys are still not verified.** sshgo uses `ssh.InsecureIgnoreHostKey()` on every hop, exactly as before. A jump chain therefore **does not make sshgo safer** and must not be read as a security feature: a compromised bastion can impersonate the target, and the man-in-the-middle is inside the tunnel where it is hardest to notice. `known_hosts` verification is a separate feature; until it lands, this limitation belongs in the README rather than in a footnote.
- Chain failures are a new class of error, reported before any dial: a missing Jump Host (named together with its target), a Jump Host without connection details, a loop with its path, and an unsupported jump spelling. A Jump Host that resolves but fails to authenticate or connect is a different failure, prefixed with the hop so the two ends of a chain stay distinguishable.
- `info` can now fail on a Host whose fields are all present, because it describes whether the Host can be reached. It resolves the chain but never dials.
- `info --json` gained an optional `jump` array (`omitempty`), so a single-hop Host's output is unchanged and existing consumers are unaffected. Each element carries only method names and counts: no key paths, passwords, or passphrases, which is the same promise the top-level `auth_methods` already made.
- The password store is unlocked at most once per invocation, which is a deliberate behaviour change: the interactive path used to prompt for the master password twice when saving a password.
- A password-only bastion is usable non-interactively only because `config set-password` exists; without it the "save one" advice in the error message would have nowhere to land.
- `config import` no longer prints a ProxyJump warning. That warning described a limitation that no longer exists.
