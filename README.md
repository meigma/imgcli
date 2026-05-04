# imgcli

`imgcli` is a prototype CLI for building disk image artifacts from Git-tracked
CUE configuration and, later, publishing those artifacts to `imgsrv`.

The repository is still in the design and scaffolding stage. The current working
design is intentionally lightweight and lives in [docs/docs/design.md](docs/docs/design.md).

## Quick Start

There is not an installable `imgcli` binary yet. The working repository content
today is the documentation site and project scaffolding.

### Prerequisites

- Node.js `22.22.2`
- npm
- Moon, if you want to run the same task graph used by CI

### Build the docs

```sh
npm ci --prefix docs
npm --prefix docs run build
```

### Run the docs locally

```sh
npm --prefix docs run start
```

## Usage

The intended CLI workflow is Git-as-truth image publishing:

```sh
imgcli publish --version 1.0.0 --alias latest --alias prod config.cue
```

That command is design intent, not current behavior. The v0 prototype will start
with local-only `plan` and `build` paths for one IncusOS artifact, then add
publishing once the `imgsrv` API is stable enough to target.

## Configuration

`imgcli` will read one CUE image configuration file, resolve it into concrete
artifact work, and write deterministic local outputs under `dist/` by default.

Release versions and aliases are publish-time inputs. They should come from the
release pipeline, usually a Git tag plus explicit CLI flags, rather than being
hard-coded into the CUE file.

## Documentation

- Documentation site source: [docs/docs](docs/docs)
- Working design: [docs/docs/design.md](docs/docs/design.md)

## Support

Use [GitHub Discussions](https://github.com/meigma/imgcli/discussions) for
questions and general support. Use
[GitHub Issues](https://github.com/meigma/imgcli/issues) for non-security bug
reports.

Do not report vulnerabilities in public channels. See [SECURITY.md](SECURITY.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines, local setup expectations, and pull request workflow.

## Security

See [SECURITY.md](SECURITY.md) for supported versions and the private vulnerability reporting path.

## License

`imgcli` is dual-licensed under the Apache License 2.0 or the MIT License, at
your option. See [LICENSE](LICENSE), [LICENSE-APACHE](LICENSE-APACHE), and
[LICENSE-MIT](LICENSE-MIT).
