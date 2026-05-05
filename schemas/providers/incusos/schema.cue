package incusos

import (
	"github.com/lxc/incus-os/incus-osd/api/seed"
	"github.com/meigma/imgcli/schemas/core"
)

#Channel: "stable" | "testing" | error("IncusOS source channel must be one of: stable, testing")

#Version: =~"^[0-9]{12}$" | error("IncusOS source version must be a 12 digit release timestamp")

// Seed file format version. IncusOS currently expects "1".
#SeedVersion: *"1" | string @go(-)

// SeedApplicationName is the set of IncusOS applications supported by the
// upstream applications seed file.
#SeedApplicationName: "incus" | "incus-ceph" | "incus-linstor" | "migration-manager" | "operations-center" | error("IncusOS seed application must be one of: incus, incus-ceph, incus-linstor, migration-manager, operations-center") @go(-)

// SeedInstallSortOrder selects the target install disk by capacity when more
// than one candidate disk matches the other install target filters.
#SeedInstallSortOrder: "smallest" | "largest" | error("IncusOS install target sort_order must be one of: smallest, largest") @go(-)

// SeedInstallSecurity permits explicitly degraded install security modes.
// Only one degraded-security option may be enabled at a time.
#SeedInstallSecurity: seed.#InstallSecurity & {
	@go(-)

	// missing_tpm allows installation without a physical TPM.
	missing_tpm: *false | bool
	// missing_secure_boot allows installation without Secure Boot.
	missing_secure_boot: *false | bool

	if missing_tpm {
		missing_secure_boot: false | error("IncusOS install security cannot set both missing_tpm and missing_secure_boot")
	}
	if missing_secure_boot {
		missing_tpm: false | error("IncusOS install security cannot set both missing_tpm and missing_secure_boot")
	}
}

// SeedInstallTarget selects the disk to receive the IncusOS installation.
#SeedInstallTarget: seed.#InstallTarget & {
	@go(-)

	// sort_order chooses the smallest or largest matching install target.
	sort_order?: #SeedInstallSortOrder
}

// SeedInstall configures the install.yaml seed file.
#SeedInstall: seed.#Install & {
	@go(-)

	// version is the IncusOS install seed format version.
	version: #SeedVersion
	// force_install ignores existing data on the target install disk.
	force_install: *false | bool
	// force_reboot reboots automatically after installation completes.
	force_reboot: *false | bool
	// security opts into degraded security modes for constrained hardware.
	security?: null | #SeedInstallSecurity
	// target filters the disk selected for installation.
	target?: null | #SeedInstallTarget
}

// SeedApplications configures the applications.yaml seed file.
#SeedApplications: seed.#Applications & {
	@go(-)

	// version is the IncusOS applications seed format version.
	version: #SeedVersion
	// applications lists the IncusOS applications to enable.
	applications: [...seed.#Application & {
		// name identifies an upstream-supported IncusOS application.
		name: #SeedApplicationName
	}]
}

// SeedIncus configures the incus.yaml seed file.
#SeedIncus: {
	@go(-)

	// version is the IncusOS Incus seed format version.
	version: #SeedVersion
	// apply_defaults asks IncusOS to apply upstream Incus defaults.
	apply_defaults: *false | bool
	// preseed is passed through to Incus init preseed handling.
	preseed?: (seed.#Incus & {preseed: _}).preseed @go(-)
}

// SeedMigrationManager configures the migration-manager.yaml seed file.
#SeedMigrationManager: {
	@go(-)

	// version is the Migration Manager seed format version.
	version: #SeedVersion
	// trusted_client_certificates lists PEM-encoded client certificates.
	trusted_client_certificates?: [...string]
	// apply_defaults asks IncusOS to apply upstream Migration Manager defaults.
	apply_defaults: *false | bool
	// preseed is passed through to Migration Manager preseed handling.
	preseed?: (seed.#MigrationManager & {preseed: _}).preseed @go(-)
}

// SeedOperationsCenter configures the operations-center.yaml seed file.
#SeedOperationsCenter: {
	@go(-)

	// version is the Operations Center seed format version.
	version: #SeedVersion
	// trusted_client_certificates lists PEM-encoded client certificates.
	trusted_client_certificates?: [...string]
	// apply_defaults asks IncusOS to apply upstream Operations Center defaults.
	apply_defaults: *false | bool
	// preseed is passed through to Operations Center preseed handling.
	preseed?: (seed.#OperationsCenter & {preseed: _}).preseed @go(-)
}

// SeedNetwork configures the network.yaml seed file.
#SeedNetwork: seed.#Network & {
	@go(-)

	// version is the IncusOS network seed format version.
	version: #SeedVersion
}

// SeedProvider configures the provider.yaml seed file.
#SeedProvider: seed.#Provider & {
	@go(-)

	// version is the IncusOS provider seed format version.
	version: #SeedVersion
}

// SeedUpdate configures the update.yaml seed file.
#SeedUpdate: seed.#Update & {
	@go(-)

	// version is the IncusOS update seed format version.
	version: #SeedVersion
	// auto_reboot allows automatic reboots after update application.
	auto_reboot: *false | bool
	// channel selects the IncusOS update channel.
	channel: #Channel
	// check_frequency controls how often IncusOS checks for updates.
	check_frequency: string
}

// Seed defines the IncusOS seed files to include in a seed archive.
// Each populated field is serialized to the corresponding <name>.yaml file.
#Seed: {
	// Install configures install.yaml for installer behavior.
	install?: #SeedInstall @go(,type=*"github.com/lxc/incus-os/incus-osd/api/seed".Install)
	// Applications configures applications.yaml for bundled IncusOS apps.
	applications?: #SeedApplications @go(,type=*"github.com/lxc/incus-os/incus-osd/api/seed".Applications)
	// Incus configures incus.yaml for Incus preseed behavior.
	incus?: #SeedIncus @go(,type=*"github.com/lxc/incus-os/incus-osd/api/seed".Incus)
	// Network configures network.yaml for system network settings.
	network?: #SeedNetwork @go(,type=*"github.com/lxc/incus-os/incus-osd/api/seed".Network)
	// Provider configures provider.yaml for configuration provider settings.
	provider?: #SeedProvider @go(,type=*"github.com/lxc/incus-os/incus-osd/api/seed".Provider)
	// Update configures update.yaml for system update policy.
	update?: #SeedUpdate @go(,type=*"github.com/lxc/incus-os/incus-osd/api/seed".Update)
	// MigrationManager configures migration-manager.yaml.
	"migration-manager"?: #SeedMigrationManager @go(MigrationManager,type=*"github.com/lxc/incus-os/incus-osd/api/seed".MigrationManager)
	// OperationsCenter configures operations-center.yaml.
	"operations-center"?: #SeedOperationsCenter @go(OperationsCenter,type=*"github.com/lxc/incus-os/incus-osd/api/seed".OperationsCenter)
}

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

	// Seed defines IncusOS install seed files to embed in customized images.
	seed?: #Seed @go(,optional=nillable)

	variants: {
		[VariantName=core.#VariantName]: #Variant & {
			artifact: {
				variant: VariantName
			}
		}
	} @go(,type=map["github.com/meigma/imgcli/schemas/core".VariantName]Variant)
}
