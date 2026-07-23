# Security Policy

## Supported versions

NeuroVSA is pre-1.0. Security fixes are applied to the latest `main` and the most recent
tagged release.

## Reporting a vulnerability

Please report vulnerabilities **privately** using GitHub's
[Private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)
(the repository's **Security** tab → **Report a vulnerability**). Please do **not** open a
public issue for security reports. We aim to acknowledge reports within a few days.

## Security posture

NeuroVSA is designed to run **locally**. If you deploy the WebSocket API, keep the following
in mind:

- **Local by default.** The API accepts only loopback origins unless started with
  `-allow-all-origins`. Do not expose it to untrusted networks without your own
  authentication and TLS in front of it.
- **Filesystem confinement.** The `/ast` indexer is restricted to `-index-root` (default the
  working directory). Absolute paths and `..` traversal are rejected, and the number of files
  walked is capped (`parser.MaxIndexFiles`).
- **No secrets, no external calls.** The engine performs no network I/O and stores no
  credentials. Persistence is a local memory-mapped file with a validated header.

## Scope

The threat model assumes an operator running the tool on their own machine. Exposing the API
to a shared or public network is outside the default posture and is the operator's
responsibility to secure.
