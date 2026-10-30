package main

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
