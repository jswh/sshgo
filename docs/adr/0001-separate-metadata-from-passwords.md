# Separate local host metadata from encrypted password store

Local host metadata (alias, notes, tags, connection priority) and encrypted credentials serve different security and access patterns. Metadata is non-sensitive and read frequently; passwords are sensitive and read only during authentication. Storing them separately means metadata reads don't require the master password, and a metadata file compromise doesn't expose credentials. The metadata file uses plain JSON at `~/.sshgo_config`; the password file stays as encrypted JSON at `~/.sshgo_passwords`.
