# Changelog

All notable changes to workharbor are documented here. The file is generated from
Conventional Commits by `make changelog`; do not edit it by hand.

## Unreleased

### Features

- **domain:** Add core objects and task state transitions ([d484154](https://github.com/wstein/workharbor/commit/d4841547e2ecb8dc5a154a6bab8a836b19088445))
- **policy:** Add default-deny autonomy table ([921bb7a](https://github.com/wstein/workharbor/commit/921bb7a7a2fd5123bb56e70b8a128ace3fc9677b))
- **runtime:** Define runtime adapter contract and capabilities ([76d523e](https://github.com/wstein/workharbor/commit/76d523eb370d3ba2e652c29d3d89d33117dcdc49))
- **agent:** Define agent adapter contract and capabilities ([926b284](https://github.com/wstein/workharbor/commit/926b284bc7bbb177e361d8e9370b51ea7f4186d1))
- **forge:** Define forge and CI adapter interfaces ([e6b226c](https://github.com/wstein/workharbor/commit/e6b226c0712312fa3e1965772f4257b0feb47209))
- **cli:** Add whr entrypoint with version and exit codes ([e636da2](https://github.com/wstein/workharbor/commit/e636da2ffaec17110d917969319de24f83c1b947))
- **assets:** Use the logo as favicon ([a480a23](https://github.com/wstein/workharbor/commit/a480a23413e6ba0b94486c2bce395796b67b7e20))
- **domain:** Define the task state machine (#8) ([c8ed615](https://github.com/wstein/workharbor/commit/c8ed61547f4117da7da169078491475fff04544c))
- **domain:** Add run and environment transition tables (#15) ([8f850d0](https://github.com/wstein/workharbor/commit/8f850d0ce1eda987b706191cdf737fa5efd0665f))
- **runtime:** Add CheckMount to reject forbidden bind mounts (#18) ([239867d](https://github.com/wstein/workharbor/commit/239867d4bc982a258e935f603ab1ebb6ab02e701))
- **domain:** Add decision status, capped input and Raise (#17) ([6f19efa](https://github.com/wstein/workharbor/commit/6f19efa03b6c17b71a3265ffeb079d5d3a659def))
- **domain:** Resolve decisions fail closed (#17) ([98c00a2](https://github.com/wstein/workharbor/commit/98c00a211f850cbe150db9531fdb527c2ab76870))
- **exitcode:** Map errors to exit codes through a Coder interface (#16) ([7e94f0d](https://github.com/wstein/workharbor/commit/7e94f0d18077963856d1ec82fae4b95d3d1235a3))
- **domain:** Report illegal transitions as conflicts (#16) ([388d766](https://github.com/wstein/workharbor/commit/388d766a15882093100e9eb43c00cbfc9ca74c78))
- **domain:** Guard run and environment changes on a task aggregate (#16) ([c0d900e](https://github.com/wstein/workharbor/commit/c0d900e34df6525c8cabd24e02eeca7762959d79))
- **domain:** Guard ready_for_review on a stopped run, SHA and CI (#16) ([c3a66c1](https://github.com/wstein/workharbor/commit/c3a66c1c5a689a3274f90a5f3a3fa4a72311f19b))
- **hostgit:** Run git hardened and read-only in agent trees (#19) ([a938e9f](https://github.com/wstein/workharbor/commit/a938e9f8c136104416f7220d695c63b252fbcdf3))
- **hostgit:** Fetch an agent branch into a supervisor-owned repo (#19) ([0c361c6](https://github.com/wstein/workharbor/commit/0c361c6bcb97128aa212bbc85b334d057532f594))
- **runtime:** Add the hardened Spec with volume and bind mounts (#20) ([9a1ded2](https://github.com/wstein/workharbor/commit/9a1ded21177c7c4ebbef5cfbce9486044b6ae1f1))
- **runtime:** Add List by owner, streaming Exec and typed Inspect (#20) ([18171e7](https://github.com/wstein/workharbor/commit/18171e7fd7eb7a447783478914d9104886825329))
- **runtimetest:** Add an in-memory runtime that can restart (#20) ([923b413](https://github.com/wstein/workharbor/commit/923b41310a4a368ed1b98dca0f8ff44902f4e5be))
- **runtimetest:** Add the runtime conformance suite (#20) ([a1c4483](https://github.com/wstein/workharbor/commit/a1c448339f1c626328cc6311aa30cd353df1e249))
- **agent:** Version the contract and type events, delivery and auth (#20) ([8f4199a](https://github.com/wstein/workharbor/commit/8f4199a5d39f5f3d25875999af8b5cff5e783c1b))
- **agenttest:** Add a scripted fake agent (#20) ([271296c](https://github.com/wstein/workharbor/commit/271296c0670426ee4c8005045525d81408bb72b5))
- **agenttest:** Add the agent conformance suite (#20) ([5d63f8f](https://github.com/wstein/workharbor/commit/5d63f8fb117d2a208888607b2507b927a5f7b1ba))
- **domain:** Record the events of a change and carry versions (#21) ([5b4f22e](https://github.com/wstein/workharbor/commit/5b4f22ee1a64ff0d2f0289c2cd9aba86a3676a68))
- **domain:** Export NewConflict and ErrNotFound for the store (#21) ([4c1a4bd](https://github.com/wstein/workharbor/commit/4c1a4bd7123b8b14fcea5d40b9942dbb246da141))
- **store:** Open SQLite in WAL mode with embedded migrations (#21) ([caba17e](https://github.com/wstein/workharbor/commit/caba17e11a20cfa5bee95e5859b7b9181103c6de))
- **store:** Append-only events with tiers and a self-recording purge (#21) ([310d54f](https://github.com/wstein/workharbor/commit/310d54f5e50d3178ea5f2dfa5be15a77967e5444))
- **store:** Replay a command by its idempotency key (#21) ([0f787c2](https://github.com/wstein/workharbor/commit/0f787c2db3aecda0e20fed837bef2aabd80cda40))
- **domain:** Read pending events without taking them (#21) ([788c2ac](https://github.com/wstein/workharbor/commit/788c2ac984816331a13ff1cd8b2c9d74e08649f4))
- **store:** Save aggregates and decisions with compare-and-swap (#21) ([0bef014](https://github.com/wstein/workharbor/commit/0bef014b46478fc5b43f5adb8ed85e0f5b6e8b86))

### Bug fixes

- **policy:** Ask before push and pin the forbidden floor (#4) ([34ac485](https://github.com/wstein/workharbor/commit/34ac48538c65516438af6b89c1b42ee9cf056506))
- **domain:** Reject negative timeouts and a zero time when raising (#49) ([79932dd](https://github.com/wstein/workharbor/commit/79932dd6fb049987f6e6ef544fb51613c3b8797a))
- **domain:** Refuse an answer that has no time (#49) ([5e992ab](https://github.com/wstein/workharbor/commit/5e992ab1936d7af33d7dc1723aed77a9ce59ba5b))
- **domain:** Keep truncation and the stored timeout when re-raising (#49) ([12b1057](https://github.com/wstein/workharbor/commit/12b10576eb71e178d6ba7e22ce6c21c477ad1517))
- **domain:** Report decision state errors as conflicts (#49) ([24991ae](https://github.com/wstein/workharbor/commit/24991aeb94b5bac89268e5a86ef4f3b1df470929))
- **domain:** StartRun accepts only a new run with a unique ID (#52) ([fae7252](https://github.com/wstein/workharbor/commit/fae72529964988018e5abd0e55b6300f1d58d68c))
- **domain:** Refuse to start a run on a finished task (#52) ([9c205d9](https://github.com/wstein/workharbor/commit/9c205d9b3fd8028702db02847892c209ccc6834c))
- **domain:** Tie a review candidate to the run that produced it (#52) ([7b91839](https://github.com/wstein/workharbor/commit/7b918397b11ef8ffc6f2d099567f8bf2c17ae966))
- **policy:** Cap agent push at ask and forbid unknown modes (#51) ([d49aa7a](https://github.com/wstein/workharbor/commit/d49aa7a74c1c5318840f4893549585215a8176c7))
- **runtime:** Resolve symlinks in the secrets paths too (#50) ([ec61259](https://github.com/wstein/workharbor/commit/ec612591db233724782e4110c70f98104dd3c8dc))
- **runtime:** Reject more secrets and runtime socket locations (#50) ([f9c6378](https://github.com/wstein/workharbor/commit/f9c6378ecfbcbe390bd278f33844f71ef4a41846))
- **runtime:** Compare mount paths by file identity (#50) ([4687e6e](https://github.com/wstein/workharbor/commit/4687e6e2bd44224e7396ef2981e5880baf0807a4))

