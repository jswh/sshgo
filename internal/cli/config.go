package cli

import (
	"fmt"
	"strings"

	"sshgo/internal/host"
	"sshgo/internal/sshconfig"
	"sshgo/internal/ui"
)

// runConfig dispatches the config subcommand.
func (c *CLI) runConfig(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(c.Out, "Usage: sshgo config <command> [args]")
		fmt.Fprintln(c.Out)
		fmt.Fprintln(c.Out, "Commands:")
		fmt.Fprintln(c.Out, "  list                          List all hosts with metadata")
		fmt.Fprintln(c.Out, "  get <host> [key]              Get metadata")
		fmt.Fprintln(c.Out, "  set <host> <key> <value>      Set metadata")
		fmt.Fprintln(c.Out, "  unset <host> [key]            Remove metadata or entire host entry")
		fmt.Fprintln(c.Out, "  remove <host>                 Remove an entire host entry (same as unset <host>)")
		fmt.Fprintln(c.Out, "  import                        Import hosts from ~/.ssh/config")
		fmt.Fprintln(c.Out, "  set-sudo-password <host>      Set sudo password for a host")
		fmt.Fprintln(c.Out, "  find --tag <tag>              Find hosts by tag")
		fmt.Fprintln(c.Out)
		fmt.Fprintln(c.Out, "Metadata keys: alias, notes, tags, connection-priority, hostname, port, user, identity-file, proxy-jump")
		return 0
	}

	cfg, err := host.LoadLocalConfig()
	if err != nil {
		fmt.Fprintf(c.Err, "Error: %v\n", err)
		return 1
	}

	switch cmd := args[0]; cmd {
	case "list":
		hosts := cfg.List()
		if len(hosts) == 0 {
			fmt.Fprintln(c.Out, "No hosts in local config")
			return 0
		}
		for _, name := range hosts {
			meta := cfg.Hosts[name]
			fmt.Fprintf(c.Out, "%s:\n", name)
			fmt.Fprintln(c.Out, "  "+strings.ReplaceAll(cfg.FormatMeta(meta), "\n", "\n  "))
		}
		return 0

	case "get":
		if len(args) < 2 {
			fmt.Fprintln(c.Err, "Usage: sshgo config get <host> [key]")
			return 1
		}
		host := args[1]
		key := ""
		if len(args) > 2 {
			key = args[2]
		}
		val, err := cfg.Get(host, key)
		if err != nil {
			fmt.Fprintf(c.Err, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintln(c.Out, val)
		return 0

	case "set":
		if len(args) < 4 {
			fmt.Fprintln(c.Err, "Usage: sshgo config set <host> <key> <value>")
			return 1
		}
		host, key, value := args[1], args[2], args[3]
		if err := cfg.Set(host, key, value); err != nil {
			fmt.Fprintf(c.Err, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintf(c.Out, "Set %s[%s] = %s\n", host, key, value)
		return 0

	case "unset":
		if len(args) < 2 {
			fmt.Fprintln(c.Err, "Usage: sshgo config unset <host> [key]")
			return 1
		}
		host := args[1]
		key := ""
		if len(args) > 2 {
			key = args[2]
		}
		if err := cfg.Unset(host, key); err != nil {
			fmt.Fprintf(c.Err, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintf(c.Out, "Unset %s[%s]\n", host, key)
		return 0

	case "remove":
		if len(args) < 2 {
			fmt.Fprintln(c.Err, "Usage: sshgo config remove <host>")
			return 1
		}
		host := args[1]
		if err := cfg.Remove(host); err != nil {
			fmt.Fprintf(c.Err, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintf(c.Out, "Removed %s\n", host)
		return 0

	case "find":
		if len(args) < 3 || args[1] != "--tag" {
			fmt.Fprintln(c.Err, "Usage: sshgo config find --tag <tag>")
			return 1
		}
		tag := args[2]
		hosts := cfg.FindByTag(tag)
		if len(hosts) == 0 {
			fmt.Fprintf(c.Out, "No hosts with tag %q\n", tag)
			return 0
		}
		for _, name := range hosts {
			fmt.Fprintln(c.Out, name)
		}
		return 0

	case "import":
		return c.runConfigImport()

	case "set-sudo-password":
		return c.runSetSudoPassword(args[1:])

	default:
		fmt.Fprintf(c.Err, "Unknown config command: %q\n", cmd)
		return 1
	}
}

// runSetSudoPassword prompts for a sudo password and saves it to the encrypted store.
func (c *CLI) runSetSudoPassword(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(c.Err, "Usage: sshgo config set-sudo-password <host>")
		return 1
	}
	host := args[0]

	pwd, err := ui.Password(fmt.Sprintf("Enter sudo password for %s", host))
	if err != nil {
		fmt.Fprintf(c.Err, "Failed to read password: %v\n", err)
		return 1
	}

	confirm, err := ui.Password("Confirm sudo password")
	if err != nil {
		fmt.Fprintf(c.Err, "Failed to read password: %v\n", err)
		return 1
	}

	if pwd != confirm {
		fmt.Fprintln(c.Err, "Passwords do not match")
		return 1
	}

	store, err := c.initPasswordStore()
	if err != nil {
		fmt.Fprintf(c.Err, "Failed to init password store: %v\n", err)
		return 1
	}
	if err := store.SetSudoPassword(host, pwd); err != nil {
		fmt.Fprintf(c.Err, "Failed to save sudo password: %v\n", err)
		return 1
	}
	fmt.Fprintf(c.Out, "sudo password for %s saved\n", host)
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
