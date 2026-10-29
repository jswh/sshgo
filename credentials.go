package main

import "slices"

// credentialTarget describes where to dial and which password-store keys to try.
//
// The password store is keyed by the name the user addressed (e.g. "acchost",
// written by `config set-sudo-password acchost`), not by the resolved hostname.
// Keys is therefore ordered: addressed key first, then the canonical Host name
// (they differ when an Alias is used), then the resolved hostname as a fallback
// for entries saved before this ordering was fixed.
type credentialTarget struct {
	Keys []string
	Host string
	User string
}

// credentialKeys builds the ordered, de-duplicated lookup key list.
func credentialKeys(addressed, canonical, hostname string) []string {
	keys := make([]string, 0, 3)
	for _, k := range []string{addressed, canonical, hostname} {
		if k == "" || slices.Contains(keys, k) {
			continue
		}
		keys = append(keys, k)
	}
	return keys
}

// primaryKey is the key used when saving a newly entered password. It falls
// back to the resolved hostname so callers never index an empty list.
func (c credentialTarget) primaryKey() string {
	if len(c.Keys) > 0 {
		return c.Keys[0]
	}
	return c.Host
}

// lookupSudoPassword returns the first saved sudo password among keys.
func lookupSudoPassword(store *PasswordStore, keys []string) (string, bool) {
	for _, key := range keys {
		if !store.HasSudoPassword(key) {
			continue
		}
		if pwd, err := store.GetSudoPassword(key); err == nil {
			return pwd, true
		}
	}
	return "", false
}

// lookupSavedPassword returns the first saved SSH password among keys.
func lookupSavedPassword(store *PasswordStore, keys []string) (string, bool) {
	for _, key := range keys {
		if !store.Has(key) {
			continue
		}
		if pwd, err := store.Get(key); err == nil {
			return pwd, true
		}
	}
	return "", false
}
