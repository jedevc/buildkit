package moby_buildkit_v1_frontend

import (
	"maps"

	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

func DescriptorFromPB(pbDesc *Descriptor) ocispecs.Descriptor {
	if pbDesc == nil {
		return ocispecs.Descriptor{}
	}
	return ocispecs.Descriptor{
		MediaType:   pbDesc.GetMediaType(),
		Size:        pbDesc.GetSize(),
		Digest:      digest.Digest(pbDesc.GetDigest()),
		Annotations: maps.Clone(pbDesc.GetAnnotations()),
	}
}

func DescriptorToPB(desc ocispecs.Descriptor) *Descriptor {
	return &Descriptor{
		MediaType:   desc.MediaType,
		Size:        desc.Size,
		Digest:      desc.Digest.String(),
		Annotations: maps.Clone(desc.Annotations),
	}
}
