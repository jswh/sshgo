package main

import "slices"

// credentialTarget describes where to dial and which password-store keys to try.
//
// The password store is keyed by the name the user addressed (e.g. "acchost",
// written by `config set-sudo-password acchost`), not by the resolved hostname.
// Keys is therefore ordered: addressed key first, then the canonical Host name
// (they differ when an Alias is used), then the resolved hostname as a fallback
// for entries saved before this ordering was fixed. Keys is never empty: the
// first element is the key a newly entered password is saved under.
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

// lookupPassword returns the first password among keys for which get succeeds.
func lookupPassword(keys []string, get func(string) (string, error)) (string, bool) {
	for _, key := range keys {
		if pwd, err := get(key); err == nil {
			return pwd, true
		}
	}
	return "", false
}

// lookupSudoPassword returns the first saved sudo password among keys.
func lookupSudoPassword(store *PasswordStore, keys []string) (string, bool) {
	return lookupPassword(keys, store.GetSudoPassword)
}

// lookupSavedPassword returns the first saved SSH password among keys.
func lookupSavedPassword(store *PasswordStore, keys []string) (string, bool) {
	return lookupPassword(keys, store.Get)
}
