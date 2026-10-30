package cli

import (
	"fmt"
	"strings"

	"sshgo/internal/host"
	"sshgo/internal/sshconfig"
)

// runConfig dispatches the config subcommand.
func (c *CLI) runConfig(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		c.printConfigHelp()
		return 0
	}

	cfg, err := host.LoadLocalConfig()
	if err != nil {
		fmt.Fprintf(c.Err, "Error: %v\n", err)
		return 1
	}

	switch cmd := args[0]; cmd {
	case "list":
		return c.runConfigList(cfg)
	case "get":
		return c.runConfigGet(cfg, args[1:])
	case "set":
		return c.runConfigSet(cfg, args[1:])
	case "unset":
		return c.runConfigUnset(cfg, args[1:])
	case "remove":
		return c.runConfigRemove(cfg, args[1:])
	case "find":
		return c.runConfigFind(cfg, args[1:])
	case "import":
		return c.runConfigImport()
	case "set-password":
		return c.runSetPassword(args[1:])
	case "set-sudo-password":
		return c.runSetSudoPassword(args[1:])
	default:
		fmt.Fprintf(c.Err, "Unknown config command: %q\n", cmd)
		return 1
	}
}

func (c *CLI) runConfigList(cfg *host.LocalConfig) int {
	names := cfg.List()
	if len(names) == 0 {
		fmt.Fprintln(c.Out, "No hosts in local config")
		return 0
	}
	for _, name := range names {
		fmt.Fprintf(c.Out, "%s:\n", name)
		fmt.Fprintln(c.Out, "  "+strings.ReplaceAll(cfg.FormatMeta(cfg.Hosts[name]), "\n", "\n  "))
	}
	return 0
}

func (c *CLI) runConfigGet(cfg *host.LocalConfig, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(c.Err, "Usage: sshgo config get <host> [key]")
		return 1
	}
	key := ""
	if len(args) > 1 {
		key = args[1]
	}
	val, err := cfg.Get(args[0], key)
	if err != nil {
		fmt.Fprintf(c.Err, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintln(c.Out, val)
	return 0
}

func (c *CLI) runConfigSet(cfg *host.LocalConfig, args []string) int {
	if len(args) < 3 {
		fmt.Fprintln(c.Err, "Usage: sshgo config set <host> <key> <value>")
		return 1
	}
	hostName, key, value := args[0], args[1], args[2]
	if err := cfg.Set(hostName, key, value); err != nil {
		fmt.Fprintf(c.Err, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintf(c.Out, "Set %s[%s] = %s\n", hostName, key, value)
	return 0
}

func (c *CLI) runConfigUnset(cfg *host.LocalConfig, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(c.Err, "Usage: sshgo config unset <host> [key]")
		return 1
	}
	key := ""
	if len(args) > 1 {
		key = args[1]
	}
	if err := cfg.Unset(args[0], key); err != nil {
		fmt.Fprintf(c.Err, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintf(c.Out, "Unset %s[%s]\n", args[0], key)
	return 0
}

func (c *CLI) runConfigRemove(cfg *host.LocalConfig, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(c.Err, "Usage: sshgo config remove <host>")
		return 1
	}
	if err := cfg.Remove(args[0]); err != nil {
		fmt.Fprintf(c.Err, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintf(c.Out, "Removed %s\n", args[0])
	return 0
}

func (c *CLI) runConfigFind(cfg *host.LocalConfig, args []string) int {
	if len(args) < 2 || args[0] != "--tag" {
		fmt.Fprintln(c.Err, "Usage: sshgo config find --tag <tag>")
		return 1
	}
	tag := args[1]
	names := cfg.FindByTag(tag)
	if len(names) == 0 {
		fmt.Fprintf(c.Out, "No hosts with tag %q\n", tag)
		return 0
	}
	for _, name := range names {
		fmt.Fprintln(c.Out, name)
	}
	return 0
}

// runSetPassword prompts for a Host's SSH password and saves it to the encrypted
// store. It exists because the only other way to file an SSH password is the
// "Save password?" prompt of an interactive login — and a jump host is exactly
// the Host that never gets one.
func (c *CLI) runSetPassword(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(c.Err, "Usage: sshgo config set-password <host>")
		return 1
	}
	return c.savePassword(args[0], passwordSpec{
		label:       "SSH password",
		usage:       "Usage: sshgo config set-password <host>",
		savedSuffix: "saved",
		set:         func(store passwordStore, host, pwd string) error { return store.Set(host, pwd) },
	})
}

// runSetSudoPassword prompts for a sudo password and saves it to the encrypted store.
func (c *CLI) runSetSudoPassword(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(c.Err, "Usage: sshgo config set-sudo-password <host>")
		return 1
	}
	return c.savePassword(args[0], passwordSpec{
		label:       "sudo password",
		usage:       "Usage: sshgo config set-sudo-password <host>",
		savedSuffix: "saved",
		set:         func(store passwordStore, host, pwd string) error { return store.SetSudoPassword(host, pwd) },
	})
}

// passwordSpec is what the two password commands disagree on. The interaction,
// the failure wording, and the storage path are shared so they cannot drift.
type passwordSpec struct {
	label       string
	usage       string
	savedSuffix string
	set         func(store passwordStore, host, pwd string) error
}

// savePassword runs the shared two-entry + confirm flow and stores the result
// under the addressed Host name, which is the first Credential Key.
func (c *CLI) savePassword(host string, spec passwordSpec) int {
	pwd, err := c.readPassword(fmt.Sprintf("Enter %s for %s", spec.label, host))
	if err != nil {
		fmt.Fprintf(c.Err, "Failed to read password: %v\n", err)
		return 1
	}

	confirm, err := c.readPassword(fmt.Sprintf("Confirm %s", spec.label))
	if err != nil {
		fmt.Fprintf(c.Err, "Failed to read password: %v\n", err)
		return 1
	}

	if pwd != confirm {
		fmt.Fprintln(c.Err, "Passwords do not match")
		return 1
	}

	store, err := c.saveStore()
	if err != nil {
		fmt.Fprintf(c.Err, "Failed to init password store: %v\n", err)
		return 1
	}
	if err := spec.set(store, host, pwd); err != nil {
		fmt.Fprintf(c.Err, "Failed to save %s: %v\n", spec.label, err)
		return 1
	}
	fmt.Fprintf(c.Out, "%s for %s %s\n", spec.label, host, spec.savedSuffix)
	return 0
}

// runConfigImport reads ~/.ssh/config and stores the hosts it describes.
// The work itself lives in sshconfig.BuildSnapshot; this is only the I/O shell.
func (c *CLI) runConfigImport() int {
	sshCfg, err := sshconfig.LoadConfig()
	if err != nil {
		fmt.Fprintf(c.Err, "Failed to read ~/.ssh/config: %v\n", err)
		return 1
	}

	lc, err := host.LoadLocalConfig()
	if err != nil {
		fmt.Fprintf(c.Err, "Error: %v\n", err)
		return 1
	}

	result := sshconfig.BuildSnapshot(sshconfig.NewSource(sshCfg), lc.Hosts)

	for _, warning := range result.Warnings {
		fmt.Fprintln(c.Err, "Warning:", warning)
	}

	lc.Hosts = result.Hosts
	if err := lc.Save(); err != nil {
		fmt.Fprintf(c.Err, "Error saving config: %v\n", err)
		return 1
	}

	fmt.Fprintf(c.Out, "Imported %d hosts from ~/.ssh/config\n", result.Imported)
	if len(result.Skipped) > 0 {
		fmt.Fprintf(c.Out, "Skipped %d pattern(s): %s\n", len(result.Skipped), sshconfig.FormatSkipped(result.Skipped))
	}
	fmt.Fprintln(c.Out, "Run `sshgo config list` to see imported hosts.")
	fmt.Fprintln(c.Out, "Re-running `sshgo config import` refreshes connection details from ~/.ssh/config;")
	fmt.Fprintln(c.Out, "hosts deleted there are not removed here.")
	return 0
}

// printConfigHelp prints the `config` usage block.
func (c *CLI) printConfigHelp() {
	fmt.Fprintln(c.Out, "Usage: sshgo config <command> [args]")
	fmt.Fprintln(c.Out)
	fmt.Fprintln(c.Out, "Commands:")
	fmt.Fprintln(c.Out, "  list                          List all hosts with metadata")
	fmt.Fprintln(c.Out, "  get <host> [key]              Get metadata")
	fmt.Fprintln(c.Out, "  set <host> <key> <value>      Set metadata")
	fmt.Fprintln(c.Out, "  unset <host> [key]            Remove metadata or entire host entry")
	fmt.Fprintln(c.Out, "  remove <host>                 Remove an entire host entry (same as unset <host>)")
	fmt.Fprintln(c.Out, "  import                        Import hosts from ~/.ssh/config")
	fmt.Fprintln(c.Out, "  set-password <host>           Set SSH password for a host")
	fmt.Fprintln(c.Out, "  set-sudo-password <host>      Set sudo password for a host")
	fmt.Fprintln(c.Out, "  find --tag <tag>              Find hosts by tag")
	fmt.Fprintln(c.Out)
	fmt.Fprintln(c.Out, "Metadata keys: alias, notes, tags, connection-priority, hostname, port, user, identity-file, proxy-jump")
}
