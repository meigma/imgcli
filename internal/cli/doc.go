// Package cli adapts Cobra, Viper, and terminal IO to imgcli commands.
//
// New commands should be built as constructor functions that receive the shared
// runtime, then registered from NewRootCommand. Flags that participate in
// configuration precedence should define a key constant and use bindConfigFlag
// so flags, IMGCLI_* environment variables, config files, and defaults resolve
// consistently.
package cli
