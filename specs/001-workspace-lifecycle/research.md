# Research decisions

Use the architecture ADRs as authority. Runtime dependencies are not installed by
this bootstrap. T001 selects stable compatible versions, creates locks and updates
the supported environment matrix. Dev Container CLI behavior must be tested against
its official specification. Bundledock attachment details are deferred to feature
002; no unsupported upstream API is assumed. Exact memory comparisons between
Phoenix and Go require equivalent measured workloads and are not architecture facts.
