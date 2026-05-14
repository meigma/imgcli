package core

#Name: =~"^[a-z0-9][a-z0-9._-]*[a-z0-9]$" | error("name must start and end with lowercase alphanumeric characters and contain only lowercase letters, digits, dots, underscores, or hyphens")

#VariantName: =~"^[A-Za-z0-9][A-Za-z0-9._-]*$" | error("variant name must start with an alphanumeric character and contain only letters, digits, dots, underscores, or hyphens")

#ArtifactKey: =~"^[A-Za-z0-9][A-Za-z0-9._-]*$" | error("artifact key must start with an alphanumeric character and contain only letters, digits, dots, underscores, or hyphens")

#Architecture: "amd64" | "arm64" | error("architecture must be one of: amd64, arm64")

#ArtifactFormat: "raw" | "raw.gz" | "qcow2" | "qcow2.gz" | "iso" | error("artifact format must be one of: raw, raw.gz, qcow2, qcow2.gz, iso")

#ProviderName: "incusos" | error("provider must be incusos")

#Image: {
	name: #Name

	description?: string

	labels?: [string]:      string
	annotations?: [string]: string
}

#OutputDefaults: {
	// Defaults to ./dist unless overridden by CLI flag or config.
	dir?: string | *"dist"
}

#PublishIntent: {
	// Optional override if imgsrv image identity should differ from image.name.
	imageName?: #Name

	labels?: [string]:      string
	annotations?: [string]: string
}

#ArtifactIntent: {
	variant: #VariantName

	provider: #ProviderName
	os:       string

	architecture: #Architecture
	format:       #ArtifactFormat

	mediaType?: string
	filename?:  string

	labels?: [string]:      string
	annotations?: [string]: string
}

#ResolvedArtifact: {
	artifactKey: #ArtifactKey

	imageName: string
	version?:  string

	variant:      #VariantName
	provider:     #ProviderName
	os:           string
	architecture: #Architecture
	format:       #ArtifactFormat

	mediaType: string
	path:      string

	labels?: [string]:      string
	annotations?: [string]: string

	source?: #ResolvedArtifactSource @go(,optional=nillable)

	// Populated after build.
	digest?: string
	size?:   int
}

#ResolvedArtifactSource: {
	version: string
	url:     string @go(URL)
	digest:  string
	size:    int
}

#ResolvedPlan: {
	image: #Image

	// Supplied by CLI or release environment. Not required in the config file.
	version?: string

	outputDir: string

	artifacts: {
		[ArtifactKey=#ArtifactKey]: #ResolvedArtifact & {
			artifactKey: ArtifactKey
		}
	} @go(,type=map[ArtifactKey]ResolvedArtifact)
}
