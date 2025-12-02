package moby_buildkit_v1_frontend

import (
	"maps"

	"github.com/moby/buildkit/util/compression"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

func DescriptorFromPB(pb *Descriptor) ocispecs.Descriptor {
	if pb == nil {
		return ocispecs.Descriptor{}
	}
	return ocispecs.Descriptor{
		MediaType:   pb.MediaType,
		Size:        pb.Size,
		Digest:      digest.Digest(pb.Digest),
		Annotations: maps.Clone(pb.Annotations),
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

func CompressionFromPB(pb *Compression) compression.Config {
	if pb == nil {
		return compression.New(compression.Default)
	}

	cfg := compression.New(compressTypeFromPB(pb.Type))
	if pb.Force {
		cfg = cfg.SetForce(true)
	}
	if pb.Level != 0 {
		// XXX: this is a lie, 0 is a valid level?
		cfg = cfg.SetLevel(int(pb.Level))
	}
	return cfg
}

func compressTypeFromPB(t Compression_Type) compression.Type {
	switch t {
	case Compression_UNCOMPRESSED:
		return compression.Uncompressed
	case Compression_GZIP:
		return compression.Gzip
	case Compression_ESTARGZ:
		return compression.EStargz
	case Compression_ZSTD:
		return compression.Zstd
	default:
		return nil
	}
}
