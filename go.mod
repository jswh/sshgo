module sshgo

go 1.25.0

require (
	github.com/kevinburke/ssh_config v1.6.0
	golang.org/x/crypto v0.53.0
	golang.org/x/term v0.44.0
)

require (
	golang.org/x/sys v0.46.0 // indirect
)

replace golang.org/x/term v0.44.0 => ./.deps/golang.org/x/term

