---
title: Initial Design
description: Temporary working design for the imgcli prototype.
---

# imgcli Initial Design

`imgcli` is a CLI for building disk image artifacts from CUE configuration and,
eventually, publishing those artifacts to `imgsrv`.

This document is intentionally a starting point for prototype work. It captures
the shared product shape and the first schema direction, but it does not attempt
to fully specify every provider. Provider-specific behavior should be designed
while prototyping each provider.

## Design Posture

`imgcli` should be built incrementally. The first useful prototype is a narrow
end-to-end path that proves configuration evaluation, artifact planning, local
build output, and artifact metadata. The design should stay flexible enough to
learn from that prototype.

Do not treat this document as a complete architecture contract. It should guide
the first implementation slice and then be revised once the prototype exposes
real constraints.

## Product Boundary

`imgcli` is a build and publish client. It reads a CUE configuration file,
resolves one image flavor into one or more concrete artifacts, invokes the
provider-specific build or preparation logic, and writes local outputs.

`imgsrv` is the catalog, storage, serving, and distribution boundary. It owns
image versions, uploaded blobs, release artifacts, artifact attachments,
publishing, aliases, downloads, and future materializations.

The CLI should not know about downstream tools such as Tinkerbell, CAPI, CAPN,
Incus, or deployment automation. Those tools are consumers of the artifacts and
server APIs, not concepts in `imgcli`.

## V0 Prototype Scope

The first provider prototype is IncusOS.

The first output target is local-only. Publishing remains a core product
feature, but it should wait until the `imgsrv` API is stable enough to drive
against.

The useful v0 path is:

1. Read a CUE image configuration.
2. Evaluate it into concrete artifact work.
3. Build or prepare one IncusOS artifact.
4. Write the artifact locally.
5. Write metadata next to the artifact that can later feed `imgsrv` publishing.

The initial prototype should not implement Incus/simplestreams materialization,
generated Incus metadata attachments, or provider schemas for Talos, Flatcar, or
distrobuilder.

## Release Pipeline Workflow

One intended workflow is release-pipeline image publishing:

1. Create one image configuration file.
2. Commit and push it if the configuration is managed in source control.
3. Create a release tag or otherwise select an explicit release version.
4. Run `imgcli` in a release pipeline with an explicit version.

For example:

```sh
imgcli publish --version 1.0.0 --alias latest --alias prod config.cue
```

The configuration file should encode the desired image state, but it should not
hard-code the release version. Version identity comes from the release pipeline,
usually through an explicit `--version` flag.

Aliases are also publish-time intent. An alias is a mutable relationship from a
text label to a published version, so it should be supplied during publish
rather than encoded inside a single image configuration file.

## Configuration Model

One configuration file describes one image flavor and its variants. If an
operator has separate `dev`, `preprod`, and `prod` IncusOS images, those are
separate configuration files.

`imgcli` should not enforce how configuration files are authored. A user may
write CUE directly, compose local CUE defaults, or generate the final CUE from a
higher-level automation plane. The CLI only cares that the final evaluated value
matches the `imgcli` schema.

CUE is the input format because it provides validation and supports natural
deduplication across variants. A user should be able to define common settings
once, then instantiate concrete variants with per-variant overrides.

`imgcli` should eventually publish a CUE module that users can import. That
module should contain:

- the core top-level schema
- provider schema packages
- sane defaults
- helper utilities only when they prove useful

The first prototype does not need to publish that module. It should structure
the repo so publishing the module later is straightforward.

## Provider Boundary

The only shared abstraction should be the top-level `imgcli` and `imgsrv`
configuration. Provider-specific configuration belongs under provider-owned
top-level fields such as `incusos`, `talos`, `flatcar`, or `distrobuilder`.

Each provider owns its own detailed schema and implementation. The shared layer
only needs enough information to plan, build, describe, and eventually publish
concrete artifacts.

Provider variants should expose a small common artifact envelope. The provider
can keep all detailed build inputs beside or beneath that envelope.

## Implementation Architecture

`imgcli` is a Go CLI built on Cobra and Viper. Cobra owns the command tree,
flags, help, version, and subcommand execution. Viper owns environment/config
binding and final configuration loading.

The initial implementation should use a small `main` package that wires signal
cancellation and calls `ExecuteContext`. Command construction should live behind
an internal CLI package. Viper should use an explicit instance rather than the
package-global singleton so command tests stay isolated.

Core code should use strong domain types instead of passing strings through the
system for image names, variants, artifact keys, architectures, formats,
digests, output paths, and publish versions.

The internal architecture should follow ports and adapters:

- core planning and build orchestration are application logic
- provider backends are adapters behind provider ports
- filesystem output, downloads, cache access, external commands, and future
  `imgsrv` clients are adapters
- Cobra/Viper command parsing and terminal rendering are CLI adapters

The core should not depend on Cobra, Viper, Charmbracelet, concrete filesystem
details, external command runners, or HTTP clients directly.

## Logging and Terminal UX

Core code should accept `*slog.Logger` as the logging contract. The CLI edge can
adapt that logger to Charmbracelet output for a better terminal experience.

The intended Charmbracelet packages are:

- `charm.land/log` for human-readable structured logging
- Lip Gloss for styled summaries and terminal presentation
- Huh for prompts or forms where interactivity is useful

All color and interactivity must be disableable by flags. Non-interactive CI
execution should be a first-class path, and commands must not require prompts
when enough flags or environment values have been supplied.

## Initial Shared Schema

The first schema has two layers:

- an author-facing CUE schema
- a normalized resolved plan that `imgcli` builds from

The author-facing schema should remain friendly to CUE composition. The resolved
plan should be implementation-oriented and much less flexible.

### Authoring Schema

```cue
#Config: {
    apiVersion: "imgcli.meigma.io/v0alpha1"
    kind:       "ImagePlan"

    image: #Image

    output?:  #OutputDefaults
    publish?: #PublishIntent

    incusos?:       _
    talos?:         _
    flatcar?:       _
    distrobuilder?: _
}

#Image: {
    name: #Name

    description?: string

    labels?:      [string]: string
    annotations?: [string]: string
}

#OutputDefaults: {
    // Defaults to ./dist unless overridden by CLI flag or config.
    dir?: string | *"dist"
}

#PublishIntent: {
    // Optional override if imgsrv image identity should differ from image.name.
    imageName?: #Name

    labels?:      [string]: string
    annotations?: [string]: string
}
```

Provider schemas can then expose a common artifact envelope without forcing all
providers into the same internal shape:

```cue
incusos: {
    defaults?: _

    variants: [VariantName=#VariantName]: {
        artifact: #ArtifactIntent & {
            variant:  VariantName
            provider: "incusos"
            os:       "incusos"
        }

        // IncusOS-specific fields are designed during the provider prototype.
    }
}

#ArtifactIntent: {
    variant: #VariantName

    provider: "incusos" | "talos" | "flatcar" | "distrobuilder"
    os:       string

    architecture: #Architecture
    format:       #ArtifactFormat

    mediaType?: string
    filename?:  string

    labels?:      [string]: string
    annotations?: [string]: string
}
```

Variant identity must align with `imgsrv`. `imgsrv` does not yet have a final
variant scheme, so `imgcli` should treat variant names as provisional inputs to
release artifact identity rather than inventing a server-incompatible model.

### Resolved Plan

The resolved plan is what `imgcli plan` prints and what `imgcli build` executes.
It should represent concrete work, not the user's raw CUE authoring structure.

```cue
#ResolvedPlan: {
    image: #Image

    // Supplied by CLI or release environment. Not required in the config file.
    version?: string

    outputDir: string

    artifacts: [ArtifactKey=#ArtifactKey]: {
        artifactKey: ArtifactKey

        imageName: string
        version?:  string

        variant:      #VariantName
        provider:     string
        os:           string
        architecture: #Architecture
        format:       #ArtifactFormat

        mediaType: string
        path:      string

        labels?:      [string]: string
        annotations?: [string]: string

        // Populated after build.
        digest?: string
        size?:   int
    }
}
```

`artifactKey` is the local handle for one concrete release artifact. It may
match the variant name in simple one-artifact-per-variant cases, but the schema
should not require that forever. A future variant may produce multiple artifacts
or attachments.

Initial scalar constraints can stay conservative:

```cue
#Name:        =~"^[a-z0-9][a-z0-9._-]*[a-z0-9]$"
#VariantName: =~"^[A-Za-z0-9][A-Za-z0-9._-]*$"
#ArtifactKey: =~"^[A-Za-z0-9][A-Za-z0-9._-]*$"

#Architecture: "amd64" | "arm64"

#ArtifactFormat: "raw" |
    "raw.gz" |
    "qcow2" |
    "qcow2.gz" |
    "iso"
```

Publish-time aliases should still use similarly conservative clean-text
validation, but aliases are CLI/API inputs rather than CUE image configuration.

## Commands

The initial command set is:

- `plan`
- `build`
- `publish`

`plan` evaluates the config and prints the resolved concrete work. It should
show at least the image name, supplied version, variants, providers,
architectures, formats, output paths, upstream source when known, and whether
publishing is configured.

`build` evaluates the config, constructs the same plan, creates artifacts, and
writes metadata next to each artifact.

`publish` implies `build`. Once `imgsrv` publishing exists, it will build the
artifacts, upload or declare them through `imgsrv`, and apply publish-time
options such as `--version` and `--alias`.

Command results should go to stdout. Logs, diagnostics, progress, prompts, and
human status messages should go to stderr. Human-readable output is the default,
and commands that print structured results should support JSON output with a
flag for automation.

## Local Output

Local output should be deterministic and friendly to CI artifacts. The starting
layout is:

```text
dist/
  <image-name>/
    <version-or-dev>/
      <variant>/
        image.<format>
        artifact.json
```

`artifact.json` is the bridge between local-only v0 and future `imgsrv`
publishing. It should record the metadata that publishing will need later:

- image identity
- version, if supplied
- artifact key
- variant
- provider
- operating system
- architecture
- format
- media type
- file path
- size
- digest
- labels and annotations

`imgcli` should also establish an XDG-compatible cache path for downloads,
intermediate files, and reusable build state.

## Future Publish Flow

For `imgsrv` integration, `imgcli` should need only client connection details:

- server address
- API token

Those values belong in CLI flags, environment variables, or CI secrets, not in
the image config.

The concrete publish protocol belongs to `imgsrv`. `imgcli` should follow the
server API once it exists rather than designing a separate publishing workflow.

Longer term, `imgsrv` may generate materialization-specific attachments. For
example, an Incus materialization may need a `qcow2` artifact plus Incus metadata
attached to it. `imgcli` should preserve enough artifact metadata for that
future flow, but v0 should not try to implement materializations.

## Open Questions

- The project is called `imgcli`; earlier examples used `imgctl`. This document
  uses `imgcli` until the binary name is intentionally changed.
- `imgsrv` and `imgcli` need to settle a shared variant and release artifact
  identity scheme.
- The IncusOS provider schema should be designed during the IncusOS prototype,
  not in this shared design pass.
