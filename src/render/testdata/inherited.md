# Entity: SAV

Savings Account

**Inherits:** dev.bank.core:ACCT

**Version:** 0.2.0 | **Hash:** none | **Timestamp:** 2026-10-29T00:00:00Z

## Rules

### [SAV-001.0/D] Withdrawals MUST be capped at three per month.

- **Author:** @alice | **Added:** 2026-10-29

## Inherited rules

- **dev.bank.core:ACCT-001** Every account MUST have an owner. (I, from ACCT in dev.bank.core)
- **dev.bank.core:ACCT-002** Withdrawals MUST NOT exceed the balance. (P, from ACCT in dev.bank.core) *Overridden by SAV-001.*

## Sub-entities

## Entity: SAVJR

Junior Savings

**Parent:** SAV

**Inherits:** SAV

**Version:** 0.1.0 | **Hash:** none | **Timestamp:** 2026-10-29T00:00:00Z

### Inherited rules

- **dev.bank.core:ACCT-001** Every account MUST have an owner. (I, from ACCT in dev.bank.core)
- **dev.bank.core:ACCT-002** Withdrawals MUST NOT exceed the balance. (P, from ACCT in dev.bank.core)

