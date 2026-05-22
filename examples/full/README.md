# examples/full

Every `regatta.yaml` option exercised. Use as a tuning reference; not
a copy-paste starting point (most fields are tuned for a particular
deployment shape).

Validate:

```bash
regatta validate-config --config examples/full/regatta.yaml
```

Compare against `examples/minimal/regatta.yaml` to see which fields
have defaults vs which must be set explicitly.

Schema source of truth: `contracts/schemas/regatta.v1.cue`.
