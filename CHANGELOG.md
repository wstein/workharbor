# Changelog

All notable changes to workharbor are documented here. The file is generated from
Conventional Commits by `make changelog`; do not edit it by hand.

## Unreleased

### Build and CI

- Enforce formatting and linting via golangci-lint and editorconfig-checker ([89c49d3](https://github.com/wstein/workharbor/commit/89c49d3bd43fcb6d661bf91b55da5f48a75b2e3c))
- Add pre-commit and conventional commit-msg hooks ([5d1edec](https://github.com/wstein/workharbor/commit/5d1edecccb7469f9ce7a9fe30f52ec103ed4fa1b))
- Run make check on push and pull requests ([2773781](https://github.com/wstein/workharbor/commit/2773781aa525c45b8070e42dd258f5fe33ee9a25))
- Deploy documentation to GitHub Pages ([ef0ee34](https://github.com/wstein/workharbor/commit/ef0ee34adcac2820350e3e51fbacfdaaa8432df2))
- **commitlint:** Add commit message linter ([2107f4b](https://github.com/wstein/workharbor/commit/2107f4be22268a081b8f53ade7b638bdaf3cfa2a))
- **commitlint:** Add command for files and revision ranges ([b50bb79](https://github.com/wstein/workharbor/commit/b50bb794d4df6da912a5217b4483ac8f8962823b))
- Run commitlint from the commit-msg hook ([0bb10ce](https://github.com/wstein/workharbor/commit/0bb10ce756770dc84d9075abe1800d0b4f6b748a))
- Add commit template and commitlint target ([d6e63d6](https://github.com/wstein/workharbor/commit/d6e63d60d4fdb630d98050a08f896b07ee2c6a99))
- Lint commit messages on pull requests ([3ed741a](https://github.com/wstein/workharbor/commit/3ed741a0d6000615ed1a1c65fac208c0635007ce))
- Configure git-cliff changelog generation ([8e329dd](https://github.com/wstein/workharbor/commit/8e329ddd7cfde9312c6fceb2d208d117cf0079f4))

### Documentation

- **design:** Add goals and requirements ([6d575e1](https://github.com/wstein/workharbor/commit/6d575e14e156e65c8a8f5efab7c2366b5993c02a))
- **design:** Record key architecture decisions ([316bb13](https://github.com/wstein/workharbor/commit/316bb133d28cbaf3b975f527eb99e7c4272499d4))
- **design:** Define domain model, state machines and persistence ([5496dbf](https://github.com/wstein/workharbor/commit/5496dbf45215580a72f39c40b8ee667274680522))
- **design:** Describe components, adapter contracts and reconciler ([9464bd2](https://github.com/wstein/workharbor/commit/9464bd262fb8be4adf98781745ed6955a446ae27))
- **design:** Add autonomy policy table ([cf7ad52](https://github.com/wstein/workharbor/commit/cf7ad52ceadb843ffebb6b0a8ef12a0bcc30b729))
- **design:** Add threat-driven security requirements ([ecaa6d1](https://github.com/wstein/workharbor/commit/ecaa6d16186e4fca77341b149f53eade4fee0b4e))
- **design:** Revise resource budget for 16 GB host ([6e9229a](https://github.com/wstein/workharbor/commit/6e9229a9b3a8e2dfe5251c903698fddf98ce4baa))
- **design:** Specify whr CLI grammar and scripting contract ([bd929d6](https://github.com/wstein/workharbor/commit/bd929d62ec05e073cabdcbb815657dda0aeec5b5))
- **design:** Scope forge, CI and identity integrations ([cf7a47c](https://github.com/wstein/workharbor/commit/cf7a47c77d4fc4000f6075ab1036a0931f517552))
- **design:** Compare existing platforms and re-rate strategies ([5d1a6e7](https://github.com/wstein/workharbor/commit/5d1a6e765315ea721197567b87508dc6e4068c15))
- **design:** Reorder open decisions and spike checklist ([215014e](https://github.com/wstein/workharbor/commit/215014e107279e371a866bf2480aaa821990f64b))
- **design:** Plan release 1 vertical slice and later phases ([91e6096](https://github.com/wstein/workharbor/commit/91e6096f3bc8a2ff0c86d6c6426a50eb66e9e5ba))
- **design:** Add review log and references ([cb00bc4](https://github.com/wstein/workharbor/commit/cb00bc4c1e1e67f1161b6cfdcfd1d870140cd46c))
- Add README ([3aed7c3](https://github.com/wstein/workharbor/commit/3aed7c34297b5d7134d9899aa5baca16a43fd793))
- Add README badges ([3c1cb77](https://github.com/wstein/workharbor/commit/3c1cb7740e6200409ac50afeaef3546ded7ff4f3))
- Add AGENTS.md for coding agents ([594a997](https://github.com/wstein/workharbor/commit/594a9976dcd4c1639405bfebd502d2d40e0e3496))
- Document enforced format and lint workflow in AGENTS.md ([f04e0bd](https://github.com/wstein/workharbor/commit/f04e0bdaaec6f54bc0527d9be640dbf734cda2bd))
- **assets:** Add anchor logo ([1df1cd6](https://github.com/wstein/workharbor/commit/1df1cd6d69289e1aac449f3055278e42e417fb20))
- **assets:** Add favicon and apple touch icon ([30d4ced](https://github.com/wstein/workharbor/commit/30d4cedd7bae4247cf6f23094062b3a796862393))
- Show logo in README ([eb1d4b6](https://github.com/wstein/workharbor/commit/eb1d4b61106245e8cf51824aab502b8092d65040))
- **assets:** Add social preview image ([c0a5d56](https://github.com/wstein/workharbor/commit/c0a5d5684c7804a2f70feb6862d8ffdcceb0effe))
- **site:** Configure Jekyll with just-the-docs theme and brand assets ([4f1d80c](https://github.com/wstein/workharbor/commit/4f1d80c811460e904cd6a1be4594f62d0d988641))
- **site:** Add home page and navigation front matter ([058067d](https://github.com/wstein/workharbor/commit/058067d46b432dbe8713a2982a3c1ac346190f4a))
- Link README to the documentation site ([b3f8ce0](https://github.com/wstein/workharbor/commit/b3f8ce0798809c5ff55afebaeb67b54e7a779e90))
- **assets:** Add README banner ([b2fb88d](https://github.com/wstein/workharbor/commit/b2fb88d2cc8cc5ecef939201bbefed3d011ac785))
- Use banner as README header ([36e2ad5](https://github.com/wstein/workharbor/commit/36e2ad50ead7c69faff875e067ff3444bcde6868))
- **assets:** Make README banner slimmer ([e44c014](https://github.com/wstein/workharbor/commit/e44c01487bb7a82a8cbc83673b7218cc8c822cf6))
- Change commit guideline terminology from small to focused ([784b36d](https://github.com/wstein/workharbor/commit/784b36d575c2f0618ff93808384a3510d4140cb2))
- Document commit trailers for agents ([bb1d9c4](https://github.com/wstein/workharbor/commit/bb1d9c4ca036b278d443e5e96d008bc693fa8865))
- Generate initial changelog ([e29ed79](https://github.com/wstein/workharbor/commit/e29ed79a0496f1816137005ba3ed90919a2752ce))

### Features

- **domain:** Add core objects and task state transitions ([38b5fc4](https://github.com/wstein/workharbor/commit/38b5fc4ec84460d6f125919608f521efa91881a6))
- **policy:** Add default-deny autonomy table ([92c475d](https://github.com/wstein/workharbor/commit/92c475d89cad48060e580354d5e069c26a53a26d))
- **runtime:** Define runtime adapter contract and capabilities ([9d9b129](https://github.com/wstein/workharbor/commit/9d9b129ec4b6e1a8083d166b6b5eb71ff9d480ed))
- **agent:** Define agent adapter contract and capabilities ([14982b4](https://github.com/wstein/workharbor/commit/14982b4945681a25fe12871fa4a8ec9ea5ce3aa4))
- **forge:** Define forge and CI adapter interfaces ([7eeda84](https://github.com/wstein/workharbor/commit/7eeda84d11d56a6ad27e06b21ac8998074129d46))
- **cli:** Add whr entrypoint with version and exit codes ([52ab01f](https://github.com/wstein/workharbor/commit/52ab01f5b4a3f6e03ba9f7018c81012d68c12d68))
- **assets:** Use the logo as favicon ([339b64f](https://github.com/wstein/workharbor/commit/339b64f82fef5ddfc536ab3c16af9a9634d4711d))

### Maintenance

- Initial empty commit ([ff9bfdb](https://github.com/wstein/workharbor/commit/ff9bfdb2fa26719694d12cf8a1e666270cbc4b3b))
- Add EUPL-1.2 license ([4a5f3c9](https://github.com/wstein/workharbor/commit/4a5f3c9e519665a191c8d66b8372b7802d54639a))
- Initialize Go module ([2bb0319](https://github.com/wstein/workharbor/commit/2bb03196ab5e004e04d937a977b70dd45dcd2ee8))
- Add gitignore and Makefile ([cd70820](https://github.com/wstein/workharbor/commit/cd7082049c0ed8a6965711d8bb5cafc232f58ed2))
- Add editorconfig ([4d9cbbb](https://github.com/wstein/workharbor/commit/4d9cbbb3da9a751e4a86e0c2004020788f344486))

### Style

- Satisfy editorconfig in usage text and design checklist ([3ca3e88](https://github.com/wstein/workharbor/commit/3ca3e88a2ee3a8f3f028fc67b623d5fc1d7d952f))

