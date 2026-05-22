# examples/minimal

Smallest valid `regatta.yaml`. Required fields only; everything else
takes a schema default. Use as a starting point.

Validate:

```bash
regatta validate-config --config examples/minimal/regatta.yaml
```

Schema source of truth: `contracts/schemas/regatta.v1.cue`.
