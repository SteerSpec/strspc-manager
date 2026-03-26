# Changelog

## [1.12.0](https://github.com/SteerSpec/strspc-manager/compare/v1.11.0...v1.12.0) (2026-03-26)


### Features

* **entity:** add SubRealms field and spec sub-realm section ([1496322](https://github.com/SteerSpec/strspc-manager/commit/1496322b6eefe8315a59f92e4ee815889b90a532))
* **entity:** add SubRealms field and spec sub-realm section ([4236eed](https://github.com/SteerSpec/strspc-manager/commit/4236eed37b75def72ce3a67fccf070f2bac40ba1))

## [1.11.0](https://github.com/SteerSpec/strspc-manager/compare/v1.10.2...v1.11.0) (2026-03-25)


### Features

* **realmresolve:** implement local path dependency resolution ([999482f](https://github.com/SteerSpec/strspc-manager/commit/999482f9ee7fd00295a50de0938f2703afcf267c))
* **rulelint:** add LintRealm for recursive cross-entity validation ([1f80312](https://github.com/SteerSpec/strspc-manager/commit/1f80312893890574763e7ed89653221c54da4493))
* **rulelint:** add LintRealm for recursive cross-entity validation ([9bf24ec](https://github.com/SteerSpec/strspc-manager/commit/9bf24ec1182ede25c18b490564d220046fa8b33d))


### Bug Fixes

* **realmresolve:** address PR [#56](https://github.com/SteerSpec/strspc-manager/issues/56) review — security, correctness, completeness ([39f34a1](https://github.com/SteerSpec/strspc-manager/commit/39f34a1efd89ac97c8050aa0ac5a5b4bc48f5c65))
* **realmresolve:** early ctx check and stricter cancellation test ([78e69db](https://github.com/SteerSpec/strspc-manager/commit/78e69db4e733560896c1ce5e98b126a41a0bf63d))
* **rulelint:** address PR [#57](https://github.com/SteerSpec/strspc-manager/issues/57) review comments ([713f773](https://github.com/SteerSpec/strspc-manager/commit/713f773455a4b355edbd99ecdc53513bdd022462))


### Documentation

* **realmresolve:** fix EUIDs comment to reflect absolute paths ([c243399](https://github.com/SteerSpec/strspc-manager/commit/c243399a8e659b6c683eeae102a39ea851250e51))

## [1.10.2](https://github.com/SteerSpec/strspc-manager/compare/v1.10.1...v1.10.2) (2026-03-25)


### Bug Fixes

* **entity:** address PR [#50](https://github.com/SteerSpec/strspc-manager/issues/50) review comments ([c37038e](https://github.com/SteerSpec/strspc-manager/commit/c37038efae7f66e4a5e7e1dfd900b6520eaea894))
* **entity:** address PR [#50](https://github.com/SteerSpec/strspc-manager/issues/50) review round 2 ([1b24a37](https://github.com/SteerSpec/strspc-manager/commit/1b24a377ef6dc0a0ec9bd3fd9d744b1a1c848074))


### Refactoring

* **entity:** extract shared WalkEntityFiles utility ([92b08bf](https://github.com/SteerSpec/strspc-manager/commit/92b08bf2f24735e570286ce0334c96f574b9631b))
* **entity:** extract shared WalkEntityFiles utility ([a308132](https://github.com/SteerSpec/strspc-manager/commit/a308132675d9725d42da43129d66a41338f5bf57))

## [1.10.1](https://github.com/SteerSpec/strspc-manager/compare/v1.10.0...v1.10.1) (2026-03-25)


### Refactoring

* **entity:** move state constants and transition logic from entityops to entity ([58d50c6](https://github.com/SteerSpec/strspc-manager/commit/58d50c63a38ce78533f535634643a41db249acf2)), closes [#45](https://github.com/SteerSpec/strspc-manager/issues/45)
* **entity:** move state constants from entityops to entity ([5a3d413](https://github.com/SteerSpec/strspc-manager/commit/5a3d413ffd30ed520e9f1cb89701a4057dada82f))


### Documentation

* design spec for moving state constants to entity package ([4d78461](https://github.com/SteerSpec/strspc-manager/commit/4d784618fbead9a7bf1cdf16e74e248dfc7ef110))

## [1.10.0](https://github.com/SteerSpec/strspc-manager/compare/v1.9.0...v1.10.0) (2026-03-25)


### Features

* **ruleeval:** implement evaluation loop with pluggable AI providers ([44e25f5](https://github.com/SteerSpec/strspc-manager/commit/44e25f58ad49446b0b843368d83356da68ebe3bf))
* **ruleeval:** implement evaluation loop with pluggable AI providers ([618b101](https://github.com/SteerSpec/strspc-manager/commit/618b1015924a1fad991522da75bce312c6ea3e50)), closes [#3](https://github.com/SteerSpec/strspc-manager/issues/3)


### Bug Fixes

* **ruleeval:** address Copilot review — nil guard, unknown verdict, message fix ([3f55a0e](https://github.com/SteerSpec/strspc-manager/commit/3f55a0eb6038df61796ddcc50f265317706849ae))


### Refactoring

* **ruleeval:** use entityops state constants instead of string literals ([d68587d](https://github.com/SteerSpec/strspc-manager/commit/d68587d3b664d69bab9f5b2877661a52ae283bc1))

## [1.9.0](https://github.com/SteerSpec/strspc-manager/compare/v1.8.0...v1.9.0) (2026-03-25)


### Features

* **ruleresolve:** implement local-path rule resolution (MVP) ([9d01ec2](https://github.com/SteerSpec/strspc-manager/commit/9d01ec253a77bf969828cdd91dd9067867adebee))
* **ruleresolve:** implement local-path rule resolution (MVP) ([b54e341](https://github.com/SteerSpec/strspc-manager/commit/b54e341a46fcda106d02ae977af72120ea3f3497)), closes [#4](https://github.com/SteerSpec/strspc-manager/issues/4)


### Bug Fixes

* **ruleresolve:** address PR review feedback ([ad403de](https://github.com/SteerSpec/strspc-manager/commit/ad403de944848f424a3841fd653098488df3f083))
* **ruleresolve:** address PR review feedback (round 2) ([0fb837b](https://github.com/SteerSpec/strspc-manager/commit/0fb837bcc249ba99b189f765b6d77d0fb5abf5e4))
* **ruleresolve:** address PR review feedback (round 3) ([6248caf](https://github.com/SteerSpec/strspc-manager/commit/6248caf71e7361632d0636a2a4152a65e0e7bd37))
* **ruleresolve:** canonicalize ResolvedSource for collision detection ([aa1eefe](https://github.com/SteerSpec/strspc-manager/commit/aa1eefe1f03398a7c0a6384e40aa32cfe471dbec))
* **ruleresolve:** clean up RSV000 message and assert code in test ([abf21f4](https://github.com/SteerSpec/strspc-manager/commit/abf21f451ee400c8f695b53880b6d569d49ba5da))
* **ruleresolve:** resolve BaseDir at construction, use file path in RSV005 ([963f170](https://github.com/SteerSpec/strspc-manager/commit/963f1706fb338d7b100649cb853ca96c0f50eebc))

## [1.8.0](https://github.com/SteerSpec/strspc-manager/compare/v1.7.1...v1.8.0) (2026-03-24)


### Features

* **rulediff:** add Compare/CompareNew/CompareDir package-level API ([0706f16](https://github.com/SteerSpec/strspc-manager/commit/0706f16b9fd144eeb8dcb3a6b9cc5a8fde6af0cf))
* **rulediff:** implement 12 lifecycle checks (§7.2) ([d0cd9e7](https://github.com/SteerSpec/strspc-manager/commit/d0cd9e7f646525a4a8aebf456e1acaae39f1d0c7))
* **rulediff:** implement 12 lifecycle checks (§7.2) ([781e315](https://github.com/SteerSpec/strspc-manager/commit/781e315f1b2bd955dc4fd52ce3e3cea61e2365ae)), closes [#2](https://github.com/SteerSpec/strspc-manager/issues/2)


### Bug Fixes

* **rulediff:** address second-round Copilot review comments ([66dadf7](https://github.com/SteerSpec/strspc-manager/commit/66dadf75e27120d07e37f13b1e6574bf0df62c23))
* **rulediff:** address three Copilot PR review comments ([4e7e56f](https://github.com/SteerSpec/strspc-manager/commit/4e7e56f06f4c1be62223f2c1b70f0d5d84cfd601))
* **rulediff:** scope RD006 to transition and add RD008 positive test ([85b54bc](https://github.com/SteerSpec/strspc-manager/commit/85b54bc5c3d424736c9b40490ac97f47265276af))
* **rulediff:** scope RD007/RD009 to root entity and clarify error message ([213b1af](https://github.com/SteerSpec/strspc-manager/commit/213b1af047f07a5c9655256cae045823fa514b04))

## [1.7.1](https://github.com/SteerSpec/strspc-manager/compare/v1.7.0...v1.7.1) (2026-03-23)


### Bug Fixes

* address PR review comments in CLAUDE.md ([adf8c1f](https://github.com/SteerSpec/strspc-manager/commit/adf8c1f19689455e89a18f0f46e6af9400b8a348))


### Documentation

* add Rule Manager Spec and CLAUDE.md ([e1509af](https://github.com/SteerSpec/strspc-manager/commit/e1509af3b342256aeb9b53f96d27fa97a44191fa))
* add Rule Manager Spec and CLAUDE.md ([cd7a59f](https://github.com/SteerSpec/strspc-manager/commit/cd7a59fb448c4c13d77c5617663fae80236e39a4))

## [1.7.0](https://github.com/SteerSpec/strspc-manager/compare/v1.6.0...v1.7.0) (2026-03-23)


### Features

* **entityops:** add entity/rule mutation logic ([a0fe296](https://github.com/SteerSpec/strspc-manager/commit/a0fe29692fb5712b3f7f0704c4beb5025c73e61b))
* **entityops:** add entity/rule mutation logic ([806a6fb](https://github.com/SteerSpec/strspc-manager/commit/806a6fb59d8feb576d0967cd4de2fc16abe05409)), closes [#22](https://github.com/SteerSpec/strspc-manager/issues/22)


### Bug Fixes

* **entityops:** address PR review comments ([c44f63d](https://github.com/SteerSpec/strspc-manager/commit/c44f63d97d8399152ff7ed87479d6a173d8971f4))
* **entityops:** filter NextRuleNumber by entity ID, improve semver errors ([3e6021f](https://github.com/SteerSpec/strspc-manager/commit/3e6021f99650335acc1b41ca3d5d4ec2907fbfad))
* **entityops:** nil-check ordering and doc clarity ([ffe7487](https://github.com/SteerSpec/strspc-manager/commit/ffe748767f182dceab7ca28701fcee37f3293627))
* **entityops:** strict semver parsing, validate entity ID before mutations ([78aa543](https://github.com/SteerSpec/strspc-manager/commit/78aa54397b4cd695bcd430d578acf80cdf813c8a))

## [1.6.0](https://github.com/SteerSpec/strspc-manager/compare/v1.5.0...v1.6.0) (2026-03-23)


### Features

* **entity:** export hash computation for reuse ([cd664f7](https://github.com/SteerSpec/strspc-manager/commit/cd664f742957ba84f0e1ef87a3430cf7bc025af2))
* **entity:** export hash computation for reuse ([2b72833](https://github.com/SteerSpec/strspc-manager/commit/2b728337954e029aee104764be4e3e6cd4f67d05)), closes [#23](https://github.com/SteerSpec/strspc-manager/issues/23)


### Bug Fixes

* **entity:** check ComputeHash errors in all test subtests ([fdd9220](https://github.com/SteerSpec/strspc-manager/commit/fdd9220ea9b8c57b2e1f910dc58e5c9a87d4e76e))

## [1.5.0](https://github.com/SteerSpec/strspc-manager/compare/v1.4.0...v1.5.0) (2026-03-23)


### Features

* **realmlint:** recursive entity scanning via filepath.WalkDir ([89b147c](https://github.com/SteerSpec/strspc-manager/commit/89b147c9159b8d178724d8c867971e91c2ceb7f7))
* **realmlint:** recursive entity scanning via filepath.WalkDir ([0d11417](https://github.com/SteerSpec/strspc-manager/commit/0d114176695908f497dbb19907e764143d52ae08)), closes [#27](https://github.com/SteerSpec/strspc-manager/issues/27)


### Bug Fixes

* **realmlint:** address PR review comments ([4169712](https://github.com/SteerSpec/strspc-manager/commit/4169712f94ab7b5a6e580e1308432f7e376fe70d))
* **realmlint:** address second round of PR review comments ([f6e1c4b](https://github.com/SteerSpec/strspc-manager/commit/f6e1c4b2c6fd1d529839e1243d8c7950ee1c39e5))
* **realmlint:** address third round of PR review comments ([1d211ef](https://github.com/SteerSpec/strspc-manager/commit/1d211eff523f1250c033ad93ffafffe8df7e0e04))
* **realmlint:** assert nested entity was actually processed ([353c1cd](https://github.com/SteerSpec/strspc-manager/commit/353c1cdf01b856b357bd02e7e71e515e5231c7f3))

## [1.4.0](https://github.com/SteerSpec/strspc-manager/compare/v1.3.0...v1.4.0) (2026-03-22)


### Features

* add Source field to RealmDep for dependency resolution ([7c419fc](https://github.com/SteerSpec/strspc-manager/commit/7c419fc759d5d74b811c465039b541f9b5e38bdd))

## [1.3.0](https://github.com/SteerSpec/strspc-manager/compare/v1.2.0...v1.3.0) (2026-03-22)


### Features

* **realmlint:** implement Realm directory validation ([1480248](https://github.com/SteerSpec/strspc-manager/commit/1480248d4b97bcf8a4d23f1e18b85ef55535b024))
* **realmlint:** implement Realm directory validation (RM001-RM007) ([fc3a4de](https://github.com/SteerSpec/strspc-manager/commit/fc3a4dea213a226c6243646dfec43674b96ddb2f)), closes [#8](https://github.com/SteerSpec/strspc-manager/issues/8)
* **schema:** add entity schema detection API ([0d09a6f](https://github.com/SteerSpec/strspc-manager/commit/0d09a6f375c2570506706377d14a39536220dbaa))
* **schema:** add entity schema detection API ([485cb99](https://github.com/SteerSpec/strspc-manager/commit/485cb9961a642cf8d59689830c0bf1b9281b10ab)), closes [#24](https://github.com/SteerSpec/strspc-manager/issues/24)


### Bug Fixes

* **realmlint:** address CI failures — gofmt, unused func, missing _schema gitkeep ([2cbf2cd](https://github.com/SteerSpec/strspc-manager/commit/2cbf2cd67617684d54f76d7559345cec3c068a50))
* **realmlint:** address PR review comments ([e46c286](https://github.com/SteerSpec/strspc-manager/commit/e46c286d21a1333bbd8dbd1980957f8cf4ddc4b6))
* **realmlint:** address second round of PR review comments ([2f5f1d2](https://github.com/SteerSpec/strspc-manager/commit/2f5f1d2eb8d2e15d7de47cd6199a06ca089a0c7d))
* **realmlint:** address third round of PR review comments ([fc3c051](https://github.com/SteerSpec/strspc-manager/commit/fc3c051316ff5dd5a0899a82e98570b95c2b5269))
* **realmlint:** clarify package doc re optional entity validation ([997eeee](https://github.com/SteerSpec/strspc-manager/commit/997eeee1b5b105ce746a914b91cf2cd0271fc18e))
* **realmlint:** use explicit Severity.String() in diagnostic output ([7e0f49c](https://github.com/SteerSpec/strspc-manager/commit/7e0f49ce08875febc7eeb5d602795e56412efefa))
* **schema:** nil guard, tighten IsEntitySchemaAnyVersion matching ([f6b6b75](https://github.com/SteerSpec/strspc-manager/commit/f6b6b75fe54a646b4f45f3970c7451c67cdda5dc))
* **schema:** reject empty version in IsEntitySchemaAnyVersion URL branch ([723c3e3](https://github.com/SteerSpec/strspc-manager/commit/723c3e328e8824a4694f71caefcb7d0a5c908f1c))
* **schema:** tighten IsEntitySchema matching and clean up tests ([db4f3d4](https://github.com/SteerSpec/strspc-manager/commit/db4f3d41525d6fe8f99c465c56a153a522c621c3))

## [1.2.0](https://github.com/SteerSpec/strspc-manager/compare/v1.1.0...v1.2.0) (2026-03-22)


### Features

* **rulelint:** implement all 13 business-rule checks ([e8c8dad](https://github.com/SteerSpec/strspc-manager/commit/e8c8dadf864e093377e29106d7b85ce8df91cb0d))
* **rulelint:** implement all 13 business-rule checks from §7.1 ([62b6643](https://github.com/SteerSpec/strspc-manager/commit/62b66439ea3b68fd987dee749ed5af0b00189f2f)), closes [#1](https://github.com/SteerSpec/strspc-manager/issues/1)


### Bug Fixes

* **rulelint:** address fourth round of PR review comments ([5cfe3bf](https://github.com/SteerSpec/strspc-manager/commit/5cfe3bf5e450624da0e4fc4161b0e23d3ab2c078))
* **rulelint:** address PR review comments ([580e61d](https://github.com/SteerSpec/strspc-manager/commit/580e61d3acab1b23040bf131a72ca0c6830e6b9b))
* **rulelint:** address second round of PR review comments ([65458d8](https://github.com/SteerSpec/strspc-manager/commit/65458d8fa3f8788bcec200f2a572b73ad3cc63e2))
* **rulelint:** address third round of PR review comments ([a892e31](https://github.com/SteerSpec/strspc-manager/commit/a892e319e31ca2ec3c85de089dc1638ec6171f57))
* **rulelint:** check fmt.Fprint return value in test helper ([eee0599](https://github.com/SteerSpec/strspc-manager/commit/eee05991aea546af8f95759b1bb859d499619ad9))
* **rulelint:** correct gofmt alignment in var block and Config struct ([7beccf5](https://github.com/SteerSpec/strspc-manager/commit/7beccf551bab7004e5fe443edcffccdf486e3991))
* **rulelint:** lint all JSON files in LintDir, use RL000 for nil input ([3962f7c](https://github.com/SteerSpec/strspc-manager/commit/3962f7c59149bd67872521541256155b7d0ad759))
* **rulelint:** replace sync.Once with sync.Mutex for thread-safe schema caching ([3b93013](https://github.com/SteerSpec/strspc-manager/commit/3b93013fbe9d57bc16d14108a93cf74cf6962814))

## [1.1.0](https://github.com/SteerSpec/strspc-manager/compare/v1.0.0...v1.1.0) (2026-03-22)


### Features

* add src/render package for markdown rendering ([d66270b](https://github.com/SteerSpec/strspc-manager/commit/d66270bd60b235c1b423b9ecf48f86f07232f93b)), closes [#16](https://github.com/SteerSpec/strspc-manager/issues/16)


### Bug Fixes

* add export comment for FormatMarkdown const block ([b31bd75](https://github.com/SteerSpec/strspc-manager/commit/b31bd750dacab639d8d9cc1f830d3de049101e36))
* clamp heading depth minimum to 1 to prevent panic ([5d9a4f0](https://github.com/SteerSpec/strspc-manager/commit/5d9a4f0bc14a95a6e70a4bd69800b20488cdc8ad))

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
