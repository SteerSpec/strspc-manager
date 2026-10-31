# Changelog

## [1.33.0](https://github.com/SteerSpec/strspc-manager/compare/v1.32.0...v1.33.0) (2026-10-31)


### Features

* **entity:** enforce entity visibility ([#191](https://github.com/SteerSpec/strspc-manager/issues/191)) ([cefb1f2](https://github.com/SteerSpec/strspc-manager/commit/cefb1f28d1c76000d33ba6c121e75d49375811d2))

## [1.32.0](https://github.com/SteerSpec/strspc-manager/compare/v1.31.0...v1.32.0) (2026-10-31)


### Features

* **result:** export a diagnostic code registry ([#186](https://github.com/SteerSpec/strspc-manager/issues/186)) ([54c92be](https://github.com/SteerSpec/strspc-manager/commit/54c92bed732b00ad9c991cac29d5d60f9cafe29f))
* **rulerefs:** validate rule-id citations in text files ([#184](https://github.com/SteerSpec/strspc-manager/issues/184)) ([0183467](https://github.com/SteerSpec/strspc-manager/commit/0183467199220f95a451bbe3d9077059508329ca))

## [1.31.0](https://github.com/SteerSpec/strspc-manager/compare/v1.30.0...v1.31.0) (2026-10-31)


### Features

* **realmresolve:** warn on dead targets and check qualified refs in bodies ([#180](https://github.com/SteerSpec/strspc-manager/issues/180)) ([7abd309](https://github.com/SteerSpec/strspc-manager/commit/7abd309c9c4f9b2c83509b5b59719a6af1e1205c))
* **ruleeval:** read the rules an extends note names with the rule ([#181](https://github.com/SteerSpec/strspc-manager/issues/181)) ([21cf1d8](https://github.com/SteerSpec/strspc-manager/commit/21cf1d8ae59729aa4c69862a5baa5f4ec1b9a116))

## [1.30.0](https://github.com/SteerSpec/strspc-manager/compare/v1.29.0...v1.30.0) (2026-10-31)


### Features

* **realmresolve:** validate qualified references in relation notes ([#175](https://github.com/SteerSpec/strspc-manager/issues/175)) ([e68579d](https://github.com/SteerSpec/strspc-manager/commit/e68579d8cab716e8859c4c13c05cb2973fcba151))


### Documentation

* polish the README and bring it up to date ([#174](https://github.com/SteerSpec/strspc-manager/issues/174)) ([fb2fdd0](https://github.com/SteerSpec/strspc-manager/commit/fb2fdd06502f836ec0eb7f9a6161e5916d4f803a))

## [1.29.0](https://github.com/SteerSpec/strspc-manager/compare/v1.28.1...v1.29.0) (2026-10-31)


### Features

* **remote:** limit the token to allowed owners ([#170](https://github.com/SteerSpec/strspc-manager/issues/170)) ([10ee7f6](https://github.com/SteerSpec/strspc-manager/commit/10ee7f6a6377d7d222dcebc6bc10373a6342498e))

## [1.28.1](https://github.com/SteerSpec/strspc-manager/compare/v1.28.0...v1.28.1) (2026-10-30)


### Documentation

* correct the 1.28.0 changelog ([e0b3967](https://github.com/SteerSpec/strspc-manager/commit/e0b39677e27dcb4d864d67f3f5b117655f024c81))
* correct the 1.28.0 changelog ([44c6d3b](https://github.com/SteerSpec/strspc-manager/commit/44c6d3bd60cfb1fcacdd06758c8f52038ed3b000))

## [1.28.0](https://github.com/SteerSpec/strspc-manager/compare/v1.27.0...v1.28.0) (2026-10-30)


### Features

* **schema:** resolve schemas from the realm's pinned core version ([db426b9](https://github.com/SteerSpec/strspc-manager/commit/db426b9b995d53d1620c9a2860a17561d2e2cc7b))


### Bug Fixes

* **schema:** stop a schema reading files through $ref ([ed6ccbb](https://github.com/SteerSpec/strspc-manager/commit/ed6ccbb5af699bb7e53d17f8ae43c72cf6fe0749))

## [1.27.0](https://github.com/SteerSpec/strspc-manager/compare/v1.26.0...v1.27.0) (2026-10-30)


### Features

* **realmlint:** check dependency pins and schema drift (RM014-RM016) ([336c8eb](https://github.com/SteerSpec/strspc-manager/commit/336c8ebf71b637f7056d80b758650f6942111c2d))
* **realmlint:** check dependency pins and schema drift (RM014-RM016) ([197735e](https://github.com/SteerSpec/strspc-manager/commit/197735ece76ac8975885e59b3f20625f577f269a))
* **rulediff:** reject a malformed added_by (RD019) ([55b6872](https://github.com/SteerSpec/strspc-manager/commit/55b6872c08dd966ec2fe7618019e42ac4e66a79c))
* **rulediff:** reject a malformed added_by (RD019) ([c0bf37f](https://github.com/SteerSpec/strspc-manager/commit/c0bf37ffddb53886f7bc2a0bab6dccf76689ffdc))
* **rulelint:** check rule bodies (RL019-RL022) ([571ecfa](https://github.com/SteerSpec/strspc-manager/commit/571ecfac412243b4f8223af318028b4029c4b6f7))
* **rulelint:** check rule bodies (RL019-RL022) ([a742dbd](https://github.com/SteerSpec/strspc-manager/commit/a742dbd89354b9e1d5c67880223baa2151cca60f))


### Bug Fixes

* **realmlint:** compare _schema only with the core that was pinned ([1df1aff](https://github.com/SteerSpec/strspc-manager/commit/1df1affa294f6d95f7c04ca5d6e692f5d673f647))
* **rulediff:** enforce DNS length limits on an added_by domain ([d48fec5](https://github.com/SteerSpec/strspc-manager/commit/d48fec514018bb5c7f01691aa3c12c829375550b))
* **rulediff:** validate added_by with stricter grammars ([b55b2cb](https://github.com/SteerSpec/strspc-manager/commit/b55b2cb94891946b32622ee74966cb0602cd1211))
* **rulediff:** validate the domain labels of an added_by address ([25c3306](https://github.com/SteerSpec/strspc-manager/commit/25c33064513b7e61a8fc196c41a90466270efb38))
* **rulelint:** see a sentence ending in a closing quote or bracket ([acdd9d4](https://github.com/SteerSpec/strspc-manager/commit/acdd9d473dc7c150fbd5ef4c85b406b834f95e63))
* **rulelint:** tighten the RL021 and RL022 detectors ([78dfb25](https://github.com/SteerSpec/strspc-manager/commit/78dfb25597b2917edf82b3cd3e15bed70728038f))
* **rulelint:** treat initialisms as abbreviations in RL022 ([546f2fe](https://github.com/SteerSpec/strspc-manager/commit/546f2fe5ca68d65a21484be225612f1beb502fc6))


### Documentation

* **spec:** list the body checks in the format linter workflow ([eb47057](https://github.com/SteerSpec/strspc-manager/commit/eb4705722ecd3b55b838b899ecc690cbe67fd71e))

## [1.26.0](https://github.com/SteerSpec/strspc-manager/compare/v1.25.0...v1.26.0) (2026-10-29)


### Features

* **render:** list inherited rules with WithInheritance ([f8ef4b7](https://github.com/SteerSpec/strspc-manager/commit/f8ef4b748af74ffe63c04bd71b30af5b85a0cd3b))

## [1.25.0](https://github.com/SteerSpec/strspc-manager/compare/v1.24.0...v1.25.0) (2026-10-29)


### Features

* **ruleeval:** carry inheritance into the evaluator prompt and cache key ([2e9c467](https://github.com/SteerSpec/strspc-manager/commit/2e9c46786558f34548d68df116219cf9bf4b90f4))
* **ruleeval:** carry inheritance into the evaluator prompt and cache key ([57e90e0](https://github.com/SteerSpec/strspc-manager/commit/57e90e0a3f143d47b7d45b13a7ddea2344642834))


### Bug Fixes

* **ruleeval:** name the inheriting entity for inherited replacements; key cache by entity ([b3e9c8c](https://github.com/SteerSpec/strspc-manager/commit/b3e9c8cd574ecffc7b4067bc4e3d3335e3fc2831))
* **ruleeval:** name the overriding entity in overridden-by markers ([bded9d2](https://github.com/SteerSpec/strspc-manager/commit/bded9d2b2fa55b69a859733990dbc45473dda3d9))

## [1.24.0](https://github.com/SteerSpec/strspc-manager/compare/v1.23.0...v1.24.0) (2026-10-29)


### Features

* **realmresolve:** resolve inheritance chains across realms with RR013-RR016 ([74de352](https://github.com/SteerSpec/strspc-manager/commit/74de3525a92bf050842bf10d75e24743796dc787))
* **realmresolve:** resolve inheritance chains across realms with RR013-RR016 ([6fe8985](https://github.com/SteerSpec/strspc-manager/commit/6fe898534117486963d5062b2035519c4075921f))


### Bug Fixes

* **realmresolve:** report a failed dependency for its sub-realm qualifiers ([1b61b74](https://github.com/SteerSpec/strspc-manager/commit/1b61b7491a8fbdc0c54f9afb1cbe6394b07765d3))
* **realmresolve:** use manifest sub-realms, skip superseded overrides, share failed loads ([639eab8](https://github.com/SteerSpec/strspc-manager/commit/639eab8295364d6f8074bd353a73c2b33ab98801))

## [1.23.0](https://github.com/SteerSpec/strspc-manager/compare/v1.22.0...v1.23.0) (2026-10-29)


### Features

* **entity:** add inherits and overrides fields with RL016-RL018, RD017-RD018 ([8ee610c](https://github.com/SteerSpec/strspc-manager/commit/8ee610c4cba0699de564c89c946822299c348963))
* **entity:** add inherits and overrides fields with RL016-RL018, RD017-RD018 ([06cb975](https://github.com/SteerSpec/strspc-manager/commit/06cb975bea53089622ec9a747b2a4754e2ca801d))


### Bug Fixes

* **realmlint:** lint a directory's entity files together for RL017 ([d24c22c](https://github.com/SteerSpec/strspc-manager/commit/d24c22c8c38c1a16bf0c21d57f27641cdb7e55fd))
* **rulelint:** index superseders across the scan for RL017 ([535643d](https://github.com/SteerSpec/strspc-manager/commit/535643d59c27a43ff0da6cc9e0253de32e0e8548))
* **rulelint:** keep rules live under abandoned superseders and check chains in single files ([b3c8983](https://github.com/SteerSpec/strspc-manager/commit/b3c8983289042aa8ce7b7ec534caf1320e071236))

## [1.22.0](https://github.com/SteerSpec/strspc-manager/compare/v1.21.0...v1.22.0) (2026-10-28)


### Features

* **realmlint:** reject a sub-realm rule_identifier_format (RM013) ([069f3de](https://github.com/SteerSpec/strspc-manager/commit/069f3deff0992af7a2244b0d190267dd4b040acd))
* **render:** honour a realm's rule_identifier_format ([4634334](https://github.com/SteerSpec/strspc-manager/commit/4634334ea175a77c0b506da67d0d1eef7457fdc8))
* **render:** honour a realm's rule_identifier_format ([3338bf6](https://github.com/SteerSpec/strspc-manager/commit/3338bf636e4c94f80729f36676ceae81363acd60))

## [1.21.0](https://github.com/SteerSpec/strspc-manager/compare/v1.20.0...v1.21.0) (2026-10-28)


### Features

* **entityops:** add UpdateNote to change a note's content and/or type ([60e3f1a](https://github.com/SteerSpec/strspc-manager/commit/60e3f1a88fbf547f0cd3b89ddb4f2dc63bd90fba))
* **entityops:** add UpdateNote to change a note's content and/or type ([4374aa0](https://github.com/SteerSpec/strspc-manager/commit/4374aa0f571f5a60b1e7372685a0e3f256b77804))


### Bug Fixes

* **entityops:** keep UpdateNoteContent error order for empty content ([d2be66d](https://github.com/SteerSpec/strspc-manager/commit/d2be66d4a978e295fc6d04ac4841a6aee33a6cde))
* **entityops:** validate only an explicitly supplied note type ([3806460](https://github.com/SteerSpec/strspc-manager/commit/38064602a006c231f61f9e3c449a0badedce9a5f))


### Documentation

* **readme:** describe render without a configurable rule-ID format ([c64970f](https://github.com/SteerSpec/strspc-manager/commit/c64970f4fbd8898eaf2fbe676f518a6a424ba557))
* **readme:** document entityops, remote, render and evalcache packages ([8e92999](https://github.com/SteerSpec/strspc-manager/commit/8e92999beb3f7589e438c7d73c027a9ca83c9226))
* **readme:** document entityops, remote, render and evalcache packages ([52554fc](https://github.com/SteerSpec/strspc-manager/commit/52554fc46d2ba8bfaf832dfdcb0aceae80920d5f))
* **readme:** exclude NewFile from the entityops version-bump sentence ([f83048b](https://github.com/SteerSpec/strspc-manager/commit/f83048b75203614ae808cfaf90f11b420ed5c741))

## [1.20.0](https://github.com/SteerSpec/strspc-manager/compare/v1.19.0...v1.20.0) (2026-10-28)


### Features

* **notes:** enforce note versioning contract (NTE-008..011) ([ef6227f](https://github.com/SteerSpec/strspc-manager/commit/ef6227f7afddf8ea0642451dbf903e2247b24509))
* **notes:** enforce note versioning contract (NTE-008..011) ([7ef9dcd](https://github.com/SteerSpec/strspc-manager/commit/7ef9dcd8f031a5e5678dc8994fab9de088416336)), closes [#123](https://github.com/SteerSpec/strspc-manager/issues/123)


### Bug Fixes

* **rulediff:** report revision drift on append-only notes (RD013) ([62570cb](https://github.com/SteerSpec/strspc-manager/commit/62570cb39b81bbd0b2fff6f1c505ec9d9487b0ab))
* **rulelint:** reject note number 00 (RL015) ([09d7200](https://github.com/SteerSpec/strspc-manager/commit/09d72008ac61be711ddacaa6d39191463500755b))
* **rulelint:** reject rule number 000 (RL005) ([0702c11](https://github.com/SteerSpec/strspc-manager/commit/0702c114ca1d420dc963e0da627c54e233d120fd))
* **rulelint:** reject rule number 000 (RL005) ([3720ffd](https://github.com/SteerSpec/strspc-manager/commit/3720ffd9bed179d00d55e462d3acf3886b007f79))

## [1.19.0](https://github.com/SteerSpec/strspc-manager/compare/v1.18.1...v1.19.0) (2026-10-24)


### Features

* **lint:** enforce MOD-001 and MOD-005 with forbidigo and depguard ([b72af12](https://github.com/SteerSpec/strspc-manager/commit/b72af12e450b195511222800aa6f7de343f308fc))
* **rules:** promote MOD-001..005 to Implemented ([5b1c29e](https://github.com/SteerSpec/strspc-manager/commit/5b1c29eabdf5cf0b85584e6c614c410a122e4e7e))
* **rules:** promote MOD-001..005 to Implemented with deterministic enforcement ([9025f94](https://github.com/SteerSpec/strspc-manager/commit/9025f945e31912cd66ae0b61c370d1a668436c40))

## [1.18.1](https://github.com/SteerSpec/strspc-manager/compare/v1.18.0...v1.18.1) (2026-10-22)


### Bug Fixes

* **entity:** honour a wrapped fs.SkipAll in walkShallow; enable errorlint ([7935d2d](https://github.com/SteerSpec/strspc-manager/commit/7935d2ddf2d83202d20017c72a8233536ec290dd))
* **entity:** honour a wrapped fs.SkipAll in walkShallow; enable errorlint; GOCONV-004/007 → Implemented ([7542bff](https://github.com/SteerSpec/strspc-manager/commit/7542bff8ed8d76d7fcb252aeda2f83c081eecc46))

## [1.18.0](https://github.com/SteerSpec/strspc-manager/compare/v1.17.0...v1.18.0) (2026-10-21)


### Features

* **evalcache:** accept a context in Open and enforce GOCONV-005 ([3e2da82](https://github.com/SteerSpec/strspc-manager/commit/3e2da8290681ffa2edfb71bca61a7e9b7047089b))
* **evalcache:** accept a context in Open and enforce GOCONV-005 with contextcheck and noctx ([a2f077a](https://github.com/SteerSpec/strspc-manager/commit/a2f077a99ae4e2d473d28b539efa22a0482aff84))

## [1.17.0](https://github.com/SteerSpec/strspc-manager/compare/v1.16.5...v1.17.0) (2026-10-21)


### Features

* **rulelint,realmlint:** accept a context in the lint entry points ([dd11bcc](https://github.com/SteerSpec/strspc-manager/commit/dd11bccc645f69f2cf5c7b169f3c8174b4658bfe))
* **rulelint,realmlint:** accept a context in the lint entry points ([b8aedee](https://github.com/SteerSpec/strspc-manager/commit/b8aedee11157d7abc96a24aa5b3163c38b2f1ba1))

## [1.16.5](https://github.com/SteerSpec/strspc-manager/compare/v1.16.4...v1.16.5) (2026-10-21)


### Bug Fixes

* **claude:** keep the SDK error reachable behind ErrUnavailable for 401/403/404 ([a91b67e](https://github.com/SteerSpec/strspc-manager/commit/a91b67e948f0835d99372af4595bd81804d0a7bd))
* **claude:** keep the SDK error reachable behind ErrUnavailable for 401/403/404 ([e11581a](https://github.com/SteerSpec/strspc-manager/commit/e11581aa5ed8e35162c21c6c4d4255f4e9777ac0)), closes [#82](https://github.com/SteerSpec/strspc-manager/issues/82)
* **evalcache:** close the replacement before renaming; lost reopen is ErrLocked ([32218e6](https://github.com/SteerSpec/strspc-manager/commit/32218e63d048223407cc460e7851722201d71063))
* **evalcache:** recreate a corrupt cache through a temp file and rename ([d473d64](https://github.com/SteerSpec/strspc-manager/commit/d473d64b15db1b427fda2b179dc34d54a4d5103e))
* **evalcache:** recreate a corrupt cache through a temp file and rename ([aafe4df](https://github.com/SteerSpec/strspc-manager/commit/aafe4df8ca64bdc2c97d112ee140c125f934f78c)), closes [#98](https://github.com/SteerSpec/strspc-manager/issues/98)

## [1.16.4](https://github.com/SteerSpec/strspc-manager/compare/v1.16.3...v1.16.4) (2026-10-21)


### Documentation

* **code:** document interface methods and anonymous struct fields ([8a2600a](https://github.com/SteerSpec/strspc-manager/commit/8a2600a0f6e744dd93492f121c1f17ed3794a784))
* **code:** give every exported constant and field a doc comment ([6519945](https://github.com/SteerSpec/strspc-manager/commit/651994527e4600cb818cd29e38632436d49ee35a))
* **code:** give every exported constant and field a doc comment ([fe6489f](https://github.com/SteerSpec/strspc-manager/commit/fe6489f57da860b88911bd23f0127afc1db255e3)), closes [#82](https://github.com/SteerSpec/strspc-manager/issues/82)
* **code:** make every field comment leading; fix two inaccurate comments ([1189b3f](https://github.com/SteerSpec/strspc-manager/commit/1189b3fbadab2574d125b2265064b541b17ba10a)), closes [#82](https://github.com/SteerSpec/strspc-manager/issues/82)

## [1.16.3](https://github.com/SteerSpec/strspc-manager/compare/v1.16.2...v1.16.3) (2026-10-21)


### Documentation

* **spec:** describe every diagnostic code and test that none is missing ([795f09b](https://github.com/SteerSpec/strspc-manager/commit/795f09b0682a7c7bddad69a235f926c95026ee9e))
* **spec:** describe every diagnostic code and test that none is missing ([5a8f5eb](https://github.com/SteerSpec/strspc-manager/commit/5a8f5eb6d2a2d4b10327c5b0939f085eda0546ce)), closes [#81](https://github.com/SteerSpec/strspc-manager/issues/81)
* **spec:** document the eval-cache EC001 diagnostic ([0a11224](https://github.com/SteerSpec/strspc-manager/commit/0a1122481a58522b2760a0bf752405259fe617de))
* **spec:** point the dependencies row at the renumbered Remote Sources section ([6fa33e6](https://github.com/SteerSpec/strspc-manager/commit/6fa33e61c1cc3d5662428f08eed84d323e3c2eae))

## [1.16.2](https://github.com/SteerSpec/strspc-manager/compare/v1.16.1...v1.16.2) (2026-10-20)


### Bug Fixes

* **claude:** let the detail step withdraw a verdict; name lifecycle states and fixtures in the prompt ([e2de160](https://github.com/SteerSpec/strspc-manager/commit/e2de1602d6e427b2d44c339a73ef064b2618dca9))
* **claude:** let the detail step withdraw a verdict; name lifecycle states and fixtures in the prompt ([d1e6878](https://github.com/SteerSpec/strspc-manager/commit/d1e68781b8ca9c230fbe5626a389a0089fdd2008)), closes [#87](https://github.com/SteerSpec/strspc-manager/issues/87)
* **claude:** reject a detail without a verdict; keep withdrawal details across chunks ([7de2a89](https://github.com/SteerSpec/strspc-manager/commit/7de2a895be27ebb6db5849a9243b9f2699a6f62e))

## [1.16.1](https://github.com/SteerSpec/strspc-manager/compare/v1.16.0...v1.16.1) (2026-10-20)


### Bug Fixes

* **claude:** keep the decode and SDK errors reachable as wrapped causes ([2d8c7c5](https://github.com/SteerSpec/strspc-manager/commit/2d8c7c59edecef028371b2fb47672066ea989f05))
* **claude:** keep the decode and SDK errors reachable as wrapped causes ([d64f943](https://github.com/SteerSpec/strspc-manager/commit/d64f9437f41190c573b92e9273f6f8aa458db613)), closes [#82](https://github.com/SteerSpec/strspc-manager/issues/82)
* **realmlint:** validate sub-realm manifests and _schema/ like the parent ([7418c56](https://github.com/SteerSpec/strspc-manager/commit/7418c56b37e8d8fdd62a5c6f87651e6e18c7c870))
* **realmlint:** validate sub-realm manifests and _schema/ like the parent ([74a8013](https://github.com/SteerSpec/strspc-manager/commit/74a8013374580f34a7ab2c6b6f08039568b784b6)), closes [#83](https://github.com/SteerSpec/strspc-manager/issues/83)

## [1.16.0](https://github.com/SteerSpec/strspc-manager/compare/v1.15.1...v1.16.0) (2026-10-20)


### Features

* **rules:** add the dev.steerspec.manager realm and dogfood CI ([#76](https://github.com/SteerSpec/strspc-manager/issues/76)) ([2874aff](https://github.com/SteerSpec/strspc-manager/commit/2874aff2a21d75cb305aadb6c515bcef7fecb3d6))


### Bug Fixes

* **ruleeval:** key verdicts by rule id so every rule gets one ([#78](https://github.com/SteerSpec/strspc-manager/issues/78)) ([7e4574c](https://github.com/SteerSpec/strspc-manager/commit/7e4574c606540e7fa846469497b389d307875c2b))

## [1.15.1](https://github.com/SteerSpec/strspc-manager/compare/v1.15.0...v1.15.1) (2026-10-19)


### Bug Fixes

* **realmresolve:** rebase relative dependencies of remote realms onto their origin ([#74](https://github.com/SteerSpec/strspc-manager/issues/74)) ([9b0f02f](https://github.com/SteerSpec/strspc-manager/commit/9b0f02f32ff57fce3ff5daa0066e26247c8b0805))

## [1.15.0](https://github.com/SteerSpec/strspc-manager/compare/v1.14.1...v1.15.0) (2026-10-18)


### Features

* **resolve:** github:// rule sources and remote realm dependencies ([#70](https://github.com/SteerSpec/strspc-manager/issues/70)) ([778e056](https://github.com/SteerSpec/strspc-manager/commit/778e056abf70f9ad668fd2271e8bf560ab17dd6e))
* **ruleeval:** Claude evaluator, batching, evaluation cache ([#71](https://github.com/SteerSpec/strspc-manager/issues/71)) ([cba41c4](https://github.com/SteerSpec/strspc-manager/commit/cba41c446bd067c008c359d85f76d568ede41939))

## [1.14.1](https://github.com/SteerSpec/strspc-manager/compare/v1.14.0...v1.14.1) (2026-03-28)


### Bug Fixes

* match Python canonical JSON in Blake3 hash computation ([a42423c](https://github.com/SteerSpec/strspc-manager/commit/a42423c25350038829fe3dd292755c67e2af97b5))

## [1.14.0](https://github.com/SteerSpec/strspc-manager/compare/v1.13.0...v1.14.0) (2026-03-26)


### Features

* **realmresolve:** add sub-realm discovery and resolution ([28f0678](https://github.com/SteerSpec/strspc-manager/commit/28f0678926d7ba3092f4681372190ac3d808deb3))
* **rulelint:** make LintRealm sub-realm-aware with global cross-refs ([11060f9](https://github.com/SteerSpec/strspc-manager/commit/11060f9d643c1a3bf3164550ec59b8e3d4fff177))
* **rulelint:** make LintRealm sub-realm-aware with global cross-refs ([5cd7ad5](https://github.com/SteerSpec/strspc-manager/commit/5cd7ad5e025a343507d88d56d6a86bb052f9fc6a))


### Bug Fixes

* **realmresolve:** absolute subDir path, richer collision messages, traversal test ([5ec8188](https://github.com/SteerSpec/strspc-manager/commit/5ec8188ba0a95cda075d528e5b496ae0c7c09ed4))
* **realmresolve:** address PR review — WithExcludeDirs, path traversal, testdata ([2523673](https://github.com/SteerSpec/strspc-manager/commit/25236734c3d371733d84d1a7ac0516cb7e33bdf4))
* **realmresolve:** reject volume-prefixed names, fix sibling EUID tracking ([36e1a32](https://github.com/SteerSpec/strspc-manager/commit/36e1a321b68f75499cd7a2dcc259fceefd92f7ae))
* **rulelint:** deduplicate lintDir via lintDirCollect, remove misleading test ([ac59ec7](https://github.com/SteerSpec/strspc-manager/commit/ac59ec7558d898e58dae497ec25437a9f9b783f7))

## [1.13.0](https://github.com/SteerSpec/strspc-manager/compare/v1.12.0...v1.13.0) (2026-03-26)


### Features

* **realmlint:** add WithExcludeDirs walker option and sub-realm validation ([fb5f8fa](https://github.com/SteerSpec/strspc-manager/commit/fb5f8fa2cae224e55fd0befd9e523c62963bbea6))


### Bug Fixes

* **realmlint:** address PR review — path traversal, error detail, EUID scoping ([58be9de](https://github.com/SteerSpec/strspc-manager/commit/58be9de6f6e9f64502d23cbfab7ebd3e215e2599))
* **realmlint:** distinguish ENOENT from other errors in RM008 ([397fb3e](https://github.com/SteerSpec/strspc-manager/commit/397fb3efb6812286b91e234a89f1b7c611e79621))

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
