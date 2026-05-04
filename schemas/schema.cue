package schemas

import (
	"github.com/meigma/imgcli/schemas/core"
	incusosprovider "github.com/meigma/imgcli/schemas/providers/incusos"
)

#Config: {
	apiVersion: "imgcli.meigma.io/v0alpha1"
	kind:       "ImagePlan"

	image: core.#Image

	output?:  core.#OutputDefaults @go(,optional=nillable)
	publish?: core.#PublishIntent  @go(,optional=nillable)

	incusos?: incusosprovider.#Config @go(,optional=nillable)
}

#Image: core.#Image

#OutputDefaults: core.#OutputDefaults

#PublishIntent: core.#PublishIntent

#ArtifactIntent: core.#ArtifactIntent

#ResolvedArtifact: core.#ResolvedArtifact

#ResolvedPlan: core.#ResolvedPlan

#Name: core.#Name

#VariantName: core.#VariantName

#ArtifactKey: core.#ArtifactKey

#Architecture: core.#Architecture

#ArtifactFormat: core.#ArtifactFormat
