package violator

// RuntimeMutate calls KeyringSet from a runtime path (not init/Setup).
// The lint must flag this.
func RuntimeMutate() {
	KeyringSet("attacker-key", []byte("badkey"))
}

// KeyringSet stub. The real impl lives in the keyring package; tests
// use a same-named stub so the AST walker can locate the call.
func KeyringSet(id string, key []byte) {}
