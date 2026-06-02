package authz

import "github.com/open-policy-agent/opa/v1/rego"

// opaStore holds compiled-once queries keyed by "tenant/action".
//
// Mutation discipline: a store is BUILT then PUBLISHED to opaAuthorizer
// via atomic.Pointer.Store; readers Load the *opaStore and walk
// queries without locking. Reload allocates a fresh *opaStore (shallow
// copy of the map + tenant slot replacement), so in-flight readers
// retain a consistent view (spec §3.3.3, R8 — RW-lock rejected).
type opaStore struct {
	queries   map[string]*rego.PreparedEvalQuery
	revisions map[string]string // tenant -> 8-char SHA prefix
}

// query returns the prepared eval query for the (tenant, action) slot,
// plus the tenant's 8-char revision. Returns (nil, "", false) when the
// tenant is unknown — caller maps to ErrTenantUnknown.
func (s *opaStore) query(tenant string, a Action) (*rego.PreparedEvalQuery, string, bool) {
	q, ok := s.queries[tenant+"/"+string(a)]
	if !ok {
		return nil, "", false
	}
	rev := s.revisions[tenant]
	return q, rev, true
}

// cloneWithTenant returns a shallow copy of s with the tenant's 4
// action slots replaced by q4. Used by Reload to publish atomically.
func (s *opaStore) cloneWithTenant(tenant string, rev string, q4 map[Action]*rego.PreparedEvalQuery) *opaStore {
	out := &opaStore{
		queries:   make(map[string]*rego.PreparedEvalQuery, len(s.queries)+4),
		revisions: make(map[string]string, len(s.revisions)+1),
	}
	for k, v := range s.queries {
		out.queries[k] = v
	}
	for k, v := range s.revisions {
		out.revisions[k] = v
	}
	for a, q := range q4 {
		out.queries[tenant+"/"+string(a)] = q
	}
	out.revisions[tenant] = rev
	return out
}
