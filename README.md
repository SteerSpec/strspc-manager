<div align="center">

<img src="https://steerspec.dev/assets/logo.svg" alt="SteerSpec logo" width="96" height="96">

# SteerSpec Rule Manager

**The enforcement engine of SteerSpec: a Go library for engineers who need to validate, diff, evaluate and resolve rules in their own tools.**

[![CI](https://github.com/SteerSpec/strspc-manager/actions/workflows/ci.yml/badge.svg)](https://github.com/SteerSpec/strspc-manager/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/SteerSpec/strspc-manager?color=161B22&labelColor=161B22)](https://github.com/SteerSpec/strspc-manager/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/SteerSpec/strspc-manager.svg)](https://pkg.go.dev/github.com/SteerSpec/strspc-manager)
[![Go](https://img.shields.io/github/go-mod/go-version/SteerSpec/strspc-manager?color=161B22&labelColor=161B22)](go.mod)
[![License](https://img.shields.io/badge/License-Apache%202.0-161B22?labelColor=161B22)](LICENSE)

[Install](#install) · [Where to go](#where-to-go) · [Governance](#governance) · [Security](#security) · [Community](#community)

</div>

Every package under `src/` that has statements keeps at least 85% statement coverage, which CI
enforces with [`scripts/coverage-gate.sh`](https://github.com/SteerSpec/strspc-manager/blob/v1.41.0/scripts/coverage-gate.sh). The lint, history
and refs modules are [stateless](https://github.com/SteerSpec/strspc-manager/blob/v1.41.0/docs/SPEC.md#62-module-breakdown)
and the diff module compares two versions of a file. The verdicts of an AI
provider in `ruleeval` depend on the model, and with no provider configured `ruleeval` runs static
checks only.

## Install

```bash
go get github.com/SteerSpec/strspc-manager@latest
```

Lint a Realm against the schemas of the core version it pins:

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

func main() {
	fetcher := schema.New(schema.ForRealm("rules")...)
	linter := rulelint.New(rulelint.WithSchemaFetcher(fetcher))

	res := linter.LintRealm(context.Background(), "rules")
	for _, d := range res.Diagnostics {
		fmt.Println(d)
	}
	if !res.OK() {
		os.Exit(1)
	}
}
```

Most users want [`strspc`](https://github.com/SteerSpec/strspc-CLI), the command line built on this
library. Releases also ship a `strspc-manager` binary with `version` and `realm-lint`.

## Where to go

| I want to | Go to |
| --------- | ----- |
| Run rule checks from a terminal | [strspc-CLI](https://github.com/SteerSpec/strspc-CLI) |
| See how the manager fits the platform | [Architecture](docs/REFERENCE.md#architecture) |
| Find the package for a task | [Packages](docs/REFERENCE.md#packages) |
| Evaluate code against rules with an AI provider | [Evaluation providers](docs/REFERENCE.md#evaluation-providers) |
| Know which schemas and rules the manager fetches | [Rule sources](docs/REFERENCE.md#rule-sources) |
| Understand Realms | [Realms](docs/REFERENCE.md#realms) |
| Read the specification | [`docs/SPEC.md`](docs/SPEC.md) |
| Look up a diagnostic code | [`docs/diagnostic-codes.json`](docs/diagnostic-codes.json) |
| Read the Go API documentation | [pkg.go.dev](https://pkg.go.dev/github.com/SteerSpec/strspc-manager) |
| Build, test and lint | [Development](docs/DEVELOPMENT.md#commands) |
| Understand branching and releases | [Branching and releases](docs/DEVELOPMENT.md#branching-and-releases) |

## Governance

Open source software by Consulare.

Apache 2.0, see [LICENSE](LICENSE) and [NOTICE](NOTICE). Pull requests target `develop` and use
[Conventional Commits](https://www.conventionalcommits.org/). The workflow is in
[Branching and releases](docs/DEVELOPMENT.md#branching-and-releases) and the
[SteerSpec contributing guide](https://github.com/SteerSpec/.github/blob/8aaccf4f890389006b2222b7f99b0bbe62c6ae58/CONTRIBUTING.md).

## Security

Do not open a public issue for a vulnerability. Report it privately through GitHub: on this
repository, choose **Security**, then **Report a vulnerability**. The
[organization security policy](https://github.com/SteerSpec/.github/blob/8aaccf4f890389006b2222b7f99b0bbe62c6ae58/SECURITY.md)
applies.

## Community

Questions and bug reports go to the
[issue tracker](https://github.com/SteerSpec/strspc-manager/issues). Everyone taking part follows the
[code of conduct](https://github.com/SteerSpec/.github/blob/8aaccf4f890389006b2222b7f99b0bbe62c6ae58/CODE_OF_CONDUCT.md).
