package incusos

import "github.com/meigma/imgcli/schemas/core"

#Channel: "stable" | "testing" | error("IncusOS source channel must be one of: stable, testing")

#Version: =~"^[0-9]{12}$" | error("IncusOS source version must be a 12 digit release timestamp")

#Source: {
	// Release channel to select from the IncusOS image catalog.
	channel?: #Channel

	// Specific IncusOS release version. Empty means latest in the selected channel.
	version?: #Version
}

#Defaults: {
	// Source image selection defaults applied to variants by provider planning.
	source?: #Source @go(,optional=nillable)
}

#Variant: {
	// Source image selection for this variant.
	source?: #Source @go(,optional=nillable)

	artifact: core.#ArtifactIntent & {
		provider: "incusos"
		os:       "incusos"
	} @go(,type="github.com/meigma/imgcli/schemas/core".ArtifactIntent)
}

#Config: {
	defaults?: #Defaults @go(,optional=nillable)

	variants: {
		[VariantName=core.#VariantName]: #Variant & {
			artifact: {
				variant: VariantName
			}
		}
	} @go(,type=map["github.com/meigma/imgcli/schemas/core".VariantName]Variant)
}
