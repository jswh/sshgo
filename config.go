package main

import (
	"fmt"
	"os"
	"strings"

	"sshgo/internal/host"
	"sshgo/internal/sshconfig"
	"sshgo/internal/ui"
)

// handleConfig subcommand dispatcher.
func handleConfig(args []string) {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Println("Usage: sshgo config <command> [args]")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  list                          List all hosts with metadata")
		fmt.Println("  get <host> [key]              Get metadata")
		fmt.Println("  set <host> <key> <value>      Set metadata")
		fmt.Println("  unset <host> [key]            Remove metadata or entire host entry")
		fmt.Println("  remove <host>                 Remove an entire host entry (same as unset <host>)")
		fmt.Println("  import                        Import hosts from ~/.ssh/config")
		fmt.Println("  set-sudo-password <host>      Set sudo password for a host")
		fmt.Println("  find --tag <tag>              Find hosts by tag")
		fmt.Println()
		fmt.Println("Metadata keys: alias, notes, tags, connection-priority, hostname, port, user, identity-file, proxy-jump")
		return
	}

	cfg, err := host.LoadLocalConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	cmd := args[0]

	switch cmd {
	case "list":
		hosts := cfg.List()
		if len(hosts) == 0 {
			fmt.Println("No hosts in local config")
			return
		}
		for _, name := range hosts {
			meta := cfg.Hosts[name]
			fmt.Printf("%s:\n", name)
			fmt.Println("  " + strings.ReplaceAll(cfg.FormatMeta(meta), "\n", "\n  "))
		}

	case "get":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: sshgo config get <host> [key]")
			os.Exit(1)
		}
		host := args[1]
		key := ""
		if len(args) > 2 {
			key = args[2]
		}
		val, err := cfg.Get(host, key)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(val)

	case "set":
		if len(args) < 4 {
			fmt.Fprintln(os.Stderr, "Usage: sshgo config set <host> <key> <value>")
			os.Exit(1)
		}
		host, key, value := args[1], args[2], args[3]
		if err := cfg.Set(host, key, value); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Set %s[%s] = %s\n", host, key, value)

	case "unset":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: sshgo config unset <host> [key]")
			os.Exit(1)
		}
		host := args[1]
		key := ""
		if len(args) > 2 {
			key = args[2]
		}
		if err := cfg.Unset(host, key); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Unset %s[%s]\n", host, key)

	case "remove":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: sshgo config remove <host>")
			os.Exit(1)
		}
		host := args[1]
		if err := cfg.Remove(host); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Removed %s\n", host)

	case "find":
		if len(args) < 3 || args[1] != "--tag" {
			fmt.Fprintln(os.Stderr, "Usage: sshgo config find --tag <tag>")
			os.Exit(1)
		}
		tag := args[2]
		hosts := cfg.FindByTag(tag)
		if len(hosts) == 0 {
			fmt.Printf("No hosts with tag %q\n", tag)
			return
		}
		for _, name := range hosts {
			fmt.Println(name)
		}

	case "import":
		handleConfigImport()

	case "set-sudo-password":
		handleSetSudoPassword(args[1:])

	default:
		fmt.Fprintf(os.Stderr, "Unknown config command: %q\n", cmd)
		os.Exit(1)
	}
}

// handleSetSudoPassword prompts for a sudo password and saves it to the encrypted store.
func handleSetSudoPassword(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: sshgo config set-sudo-password <host>")
		os.Exit(1)
	}
	host := args[0]

	pwd, err := ui.Password(fmt.Sprintf("Enter sudo password for %s", host))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read password: %v\n", err)
		os.Exit(1)
	}

	confirm, err := ui.Password("Confirm sudo password")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read password: %v\n", err)
		os.Exit(1)
	}

	if pwd != confirm {
		fmt.Fprintln(os.Stderr, "Passwords do not match")
		os.Exit(1)
	}

	store, err := initPasswordStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to init password store: %v\n", err)
		os.Exit(1)
	}
	if err := store.SetSudoPassword(host, pwd); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save sudo password: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("sudo password for %s saved\n", host)
}

// handleConfigImport reads ~/.ssh/config and stores the hosts it describes.
// The work itself lives in sshconfig.BuildSnapshot; this is only the I/O shell.
func handleConfigImport() {
	sshCfg, err := sshconfig.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read ~/.ssh/config: %v\n", err)
		os.Exit(1)
	}

	lc, err := host.LoadLocalConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	result := sshconfig.BuildSnapshot(sshconfig.NewSource(sshCfg), lc.Hosts)

	for _, warning := range result.Warnings {
		fmt.Fprintln(os.Stderr, "Warning:", warning)
	}

	lc.Hosts = result.Hosts
	if err := lc.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Imported %d hosts from ~/.ssh/config\n", result.Imported)
	if len(result.Skipped) > 0 {
		fmt.Printf("Skipped %d pattern(s): %s\n", len(result.Skipped), sshconfig.FormatSkipped(result.Skipped))
	}
	fmt.Println("Run `sshgo config list` to see imported hosts.")
	fmt.Println("Re-running `sshgo config import` refreshes connection details from ~/.ssh/config;")
	fmt.Println("hosts deleted there are not removed here.")
}
