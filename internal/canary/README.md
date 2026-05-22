# internal/canary/

Canary injection mechanism (~5% of agent spawns; archetype patches
from `testdata/gates/canary/`).

Activation trigger: MVP-3 canary injector implementation lands.
Corpus + archetype catalog already in `testdata/gates/canary/`;
this directory holds the orchestrator-side injector that selects +
applies them.
