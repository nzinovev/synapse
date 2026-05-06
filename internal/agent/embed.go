package agent

import "embed"

//go:embed agents
var _bundledAgentsFS embed.FS

// BundledAgentsFS is the embedded filesystem containing all bundled agent definitions.
var BundledAgentsFS embed.FS

func init() {
	BundledAgentsFS = _bundledAgentsFS
}
