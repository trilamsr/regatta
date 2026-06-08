// schemas/regatta/secrets.cue
//
// Hoisted from regatta.v1.cue per #970 slice 1. Same package
// (regattav1) → within-package symbol resolution stays free; #Config
// still references #Secrets without an import. CUE auto-merges every
// *.cue under the embed glob at compile time.

package regattav1

// #Secret routes one logical secret to one operator-chosen source per
// #911. Field-shape only; cross-field invariants (name xor path) live
// in Go because CUE struct-shaping in unified contexts is brittle.
#Secret: {
	source:  "env" | "keychain" | "pass" | "file"
	name?:   string
	path?:   string
	key_id?: string
}

// #Secrets is the operator-facing block consolidating secret sources
// for canonical regatta keys. Absent block ⇒ Default chain (back-compat).
#Secrets: {
	anthropic_api_key?: #Secret
	gh_token?:          #Secret
	brief_hmac?:        #Secret
	audit_hmac?:        #Secret
	approval_token?:    #Secret
}
