package incusos

import "github.com/meigma/imgcli/schemas/core"

#Variant: {
	artifact: core.#ArtifactIntent & {
		provider: "incusos"
		os:       "incusos"
	} @go(,type="github.com/meigma/imgcli/schemas/core".ArtifactIntent)
}

#Config: {
	defaults?: _

	variants: {
		[VariantName=core.#VariantName]: #Variant & {
			artifact: {
				variant: VariantName
			}
		}
	} @go(,type=map["github.com/meigma/imgcli/schemas/core".VariantName]Variant)
}
