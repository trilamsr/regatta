package boot

// Setup is the legitimate boot-time path; KeyringSet here is allowed.
func Setup() {
	KeyringSet("k1", []byte("0123456789abcdef0123456789abcdef"))
}

func init() {
	KeyringSet("k0", []byte("aaaaaaaabbbbbbbbccccccccdddddddd"))
}

// KeyringSet stub. See sibling fixture.
func KeyringSet(id string, key []byte) {}
