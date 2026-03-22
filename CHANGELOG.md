# Changelog

## 1.0.0 (2026-03-22)


### Features

* add make setup target for dev environment ([b05d0f6](https://github.com/SteerSpec/strspc-manager/commit/b05d0f6bae42433359f41e2768449263c0d673b0))
* bootstrap Go project with CLI skeleton and CI/CD ([c4b320e](https://github.com/SteerSpec/strspc-manager/commit/c4b320e3710e5560d906a31f1c3c77515dc80d7d))
* bootstrap Go project with CLI skeleton and CI/CD ([8b229ff](https://github.com/SteerSpec/strspc-manager/commit/8b229ffc2c252c1a6fc123b1eb80df01391d080f)), closes [#5](https://github.com/SteerSpec/strspc-manager/issues/5)
* scaffold public Go packages for CLI and Cloud integration ([3e5228b](https://github.com/SteerSpec/strspc-manager/commit/3e5228bf3fb5e6dcd4e836a9ba469c8d3d47b52c))
* scaffold public Go packages for CLI and Cloud integration ([a806e1e](https://github.com/SteerSpec/strspc-manager/commit/a806e1e843a1c532262cef7dcd61b22321965d09)), closes [#12](https://github.com/SteerSpec/strspc-manager/issues/12)


### Bug Fixes

* address PR [#13](https://github.com/SteerSpec/strspc-manager/issues/13) review comments ([71a3cfb](https://github.com/SteerSpec/strspc-manager/commit/71a3cfb0f4c4ecb475e419ac6a177da93f6202c6))
* address PR [#7](https://github.com/SteerSpec/strspc-manager/issues/7) review comments ([c56cbc0](https://github.com/SteerSpec/strspc-manager/commit/c56cbc08def4b887806e60dad38db2f2f1278232))
* address PR review feedback ([8b3d00e](https://github.com/SteerSpec/strspc-manager/commit/8b3d00e17ffabecca3fe670fc88b1942cd7da362))
* address second round of PR [#13](https://github.com/SteerSpec/strspc-manager/issues/13) review comments ([3fd4660](https://github.com/SteerSpec/strspc-manager/commit/3fd46609093631db4f63f3e7641471ae39b37e62))
* address second round of PR review feedback ([0a52704](https://github.com/SteerSpec/strspc-manager/commit/0a52704bcfa92d60193906b3716bb899e1e7eb19))
* clarify Date doc comment wording ([4cf0fd1](https://github.com/SteerSpec/strspc-manager/commit/4cf0fd1b0314b4179aa4d3d2cc3ee9c57f89675b))
* include CLI entry point missed by overly broad gitignore ([2e1fdee](https://github.com/SteerSpec/strspc-manager/commit/2e1fdee44e1cc64d966f1a0aef9d547d2fa859b9))
* pin goimports version for reproducible formatting ([da090b4](https://github.com/SteerSpec/strspc-manager/commit/da090b44313edfc8f0b03c48924ec061985804df))
* reject backslashes and add cache path containment check ([78feff5](https://github.com/SteerSpec/strspc-manager/commit/78feff5e30330157a1774648d595c251406144a9))
* reject empty/dot schema paths, Windows rename compat, path validation tests ([744ce26](https://github.com/SteerSpec/strspc-manager/commit/744ce268d2c69d7ecc8aa9049951d49ea381072e))
* reject symlinks in cache reads, add ruleeval.New validation tests ([8d02fb3](https://github.com/SteerSpec/strspc-manager/commit/8d02fb3576939d1bdaadd1ef72874f4c0630a44b))
* symlink-aware containment, return error from New, nil client guard ([b2b8258](https://github.com/SteerSpec/strspc-manager/commit/b2b8258f04f897123393cb507b3efe403ba5c6a0))
* untrack beads credential key and add to gitignore ([404bb3e](https://github.com/SteerSpec/strspc-manager/commit/404bb3e73efb08b1d447a01119fd8e5cf2a6723e))
* use filepath.Rel containment check, enforce provider requirement ([546716c](https://github.com/SteerSpec/strspc-manager/commit/546716cdcb742db70c4acb569d3d515863e36984))
* use PAT for release-please to trigger GoReleaser workflow ([ab6e7f8](https://github.com/SteerSpec/strspc-manager/commit/ab6e7f83fcca0ace6ac5a61cdbcc34c29f4b93f3))
* use v2 config key linters.settings instead of linters-settings ([3fabdbf](https://github.com/SteerSpec/strspc-manager/commit/3fabdbf78bb0123c4486ec1f2917910d79bb091e))


### Documentation

* add architecture and Realm sections to README ([513f224](https://github.com/SteerSpec/strspc-manager/commit/513f2240b167ca90f4d961e56dbb8e28f770ecb2))
* add architecture, modules, and Realm sections to README ([e90d4f4](https://github.com/SteerSpec/strspc-manager/commit/e90d4f4efad45a758213938cee4a5ca1cd5f1b6e))
* add rule sources section with steerspec.dev URLs ([63333ad](https://github.com/SteerSpec/strspc-manager/commit/63333ad1fbc18372fdba68fafdb3431e6f3e8bc6))
* update README with mermaid diagram, packages, and realm schema ([1860826](https://github.com/SteerSpec/strspc-manager/commit/186082614975fac568a9c05b3230bafcf9ee1152))
