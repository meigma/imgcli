# Contributing

Thank you for your interest in contributing to `imgcli`. This guide covers
questions, bug reports, feature requests, and pull requests.
For private vulnerability reporting, use [SECURITY.md](SECURITY.md) instead of public channels.

## Asking Questions

Use [GitHub Discussions](https://github.com/meigma/imgcli/discussions) for usage
questions, troubleshooting, and general discussion.

## Reporting Bugs

Report non-security bugs through
[GitHub Issues](https://github.com/meigma/imgcli/issues).
Include the following details when possible:

- version, commit, or environment details
- steps to reproduce
- expected behavior
- actual behavior
- logs, screenshots, or a minimal reproduction

If you are reporting a security issue, stop and follow [SECURITY.md](SECURITY.md) instead.

## Proposing Features

Use [GitHub Issues](https://github.com/meigma/imgcli/issues) or
[GitHub Discussions](https://github.com/meigma/imgcli/discussions) for feature
requests and design proposals. For larger changes, describe the problem, the
proposed approach, and any compatibility or migration concerns before starting
implementation.

## Pull Requests

Unless the repository documents a different process, contributors should:

1. Keep changes focused and scoped to a single problem.
2. Add or update tests when behavior changes.
3. Update documentation when user-facing behavior changes.
4. Describe the change clearly in the pull request.
5. Make sure CI passes before requesting review.

## Local Setup

```sh
npm ci --prefix docs
```

Useful project commands:

```sh
npm --prefix docs run typecheck
npm --prefix docs run build
moon ci --summary minimal
```

## License and Ownership

Unless otherwise stated, contributions are accepted under the same dual license
as the repository: Apache License 2.0 or MIT, at your option. This repository
does not currently require a CLA or DCO sign-off.
