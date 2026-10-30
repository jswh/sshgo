package credential

// LookupPassword returns the first password among keys for which get succeeds.
func LookupPassword(keys []string, get func(string) (string, error)) (string, bool) {
	for _, key := range keys {
		if pwd, err := get(key); err == nil {
			return pwd, true
		}
	}
	return "", false
}

// LookupSudoPassword returns the first saved sudo password among keys.
func LookupSudoPassword(store *Store, keys []string) (string, bool) {
	return LookupPassword(keys, store.GetSudoPassword)
}

// LookupSavedPassword returns the first saved SSH password among keys.
func LookupSavedPassword(store *Store, keys []string) (string, bool) {
	return LookupPassword(keys, store.Get)
}
