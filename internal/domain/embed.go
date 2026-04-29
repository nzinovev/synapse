package domain

import "embed"

//go:embed pipelines
var _pipelinesFS embed.FS

func init() {
	PipelinesFS = _pipelinesFS
}
