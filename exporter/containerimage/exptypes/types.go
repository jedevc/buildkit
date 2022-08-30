package exptypes

import (
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	ExporterConfigDigestKey      = "config.digest"
	ExporterImageDigestKey       = "containerimage.digest"
	ExporterImageConfigKey       = "containerimage.config"
	ExporterImageConfigDigestKey = "containerimage.config.digest"
	ExporterImageDescriptorKey   = "containerimage.descriptor"
	ExporterInlineCache          = "containerimage.inlinecache"
	ExporterBuildInfo            = "containerimage.buildinfo"
	ExporterPlatformsKey         = "refs.platforms"
)

type Platforms struct {
	Platforms []Platform
}

func (ps *Platforms) IDs() []string {
	ids := make([]string, len(ps.Platforms))
	for i, p := range ps.Platforms {
		ids[i] = p.ID
	}
	return ids
}

type Platform struct {
	ID       string
	Platform ocispecs.Platform
}
