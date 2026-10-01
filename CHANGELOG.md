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

### Bug fixes

- **policy:** Ask before push and pin the forbidden floor (#4) ([34ac485](https://github.com/wstein/workharbor/commit/34ac48538c65516438af6b89c1b42ee9cf056506))

