# Handoff fixture corpus (MVP-2)

These fixtures are the contract for `handoff.schema.json` and the
`internal/missions` parse/validate path. They are exercised by
`TestHandoffCorpus` in `corpus_test.go`.

Layout mirrors the L0 corpus shape (`gates/l0/testdata/`):

```
testdata/handoffs/
├── README.md                              (this file)
├── pass/                                  schema-valid handoffs
│   ├── 00_minimal.json                    smallest legal payload
│   ├── 01_full.json                       every optional field populated
│   └── 02_signed_roundtrip.json           HMAC verifies under TEST_KEY
└── fail/                                  schema-invalid handoffs
    ├── 00_bad_mission_id.json             mission_id pattern violation
    ├── 01_falsification_no_citation.json  citation regex violation
    └── 02_trap_pattern_no_falsification.json  pattern claimed, no backing falsification

```

The `02_signed_roundtrip.json` fixture uses a documented test key
(`internal/missions/corpus_test.go:testHMACKey`). The mac field is
recomputed by the test if you change the JSON body; do not hand-edit
the mac.

Adding a fixture:

1. Place the JSON file in `pass/` or `fail/`.
2. The test auto-discovers via filepath.Walk; no registration needed.
3. For `pass/`, the test asserts ParseAndValidate succeeds.
4. For `fail/`, the test asserts ParseAndValidate returns an error
   matching ErrSchemaInvalid or ErrFalsificationMissingForPattern.
