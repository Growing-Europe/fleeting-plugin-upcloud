# Security Policy

## Reporting a vulnerability

**Please do not open a public issue for security vulnerabilities.**

Report privately via GitHub's [private vulnerability reporting](https://github.com/Growing-Europe/fleeting-plugin-upcloud/security/advisories/new)
("Report a vulnerability" under the repository's **Security** tab). We aim to
acknowledge a report within 5 business days and to provide a remediation
timeline after triage.

When reporting, please include:

- the affected version (tag or commit),
- a description of the issue and its impact,
- reproduction steps or a proof of concept, if available.

## Scope

This plugin handles an UpCloud API token and provisions billable cloud servers.
Reports we are especially interested in:

- credential handling (token logged, written to disk, or committed),
- a code path that creates servers/storage without a matching delete (a billing
  leak),
- privilege or input-validation issues in the provider interface.

## Supported versions

This project is in early development (pre-`v1.0.0`). Security fixes are applied
to the latest released minor version. Until `v1.0.0`, only the most recent
release is supported.

## Disclosure

We follow coordinated disclosure: we will work with you on a fix and a public
advisory, and credit you unless you prefer to remain anonymous.
