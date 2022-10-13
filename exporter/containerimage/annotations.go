package containerimage

import (
	"fmt"

	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/containerd/containerd/platforms"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
)

type Annotations struct {
	Index              map[string]string
	IndexDescriptor    map[string]string
	Manifest           map[string]string
	ManifestDescriptor map[string]string
}

// AnnotationsGroup is a map of annotations keyed by the reference key
type AnnotationsGroup map[string]*Annotations

func ParseAnnotations(kvs map[string][]byte) (AnnotationsGroup, error) {
	ag := AnnotationsGroup{}
	for k, v := range kvs {
		if err := ag.Load(k, string(v)); err != nil {
			return nil, err
		}
	}
	return ag, nil
}

func (ag AnnotationsGroup) Load(k, v string) error {
	a, ok, err := exptypes.ParseAnnotationKey(k)
	if !ok {
		// FIXME: uh oh
		return nil
	}
	if err != nil {
		return err
	}

	p := a.PlatformString()

	if ag[p] == nil {
		ag[p] = &Annotations{
			IndexDescriptor:    make(map[string]string),
			Index:              make(map[string]string),
			Manifest:           make(map[string]string),
			ManifestDescriptor: make(map[string]string),
		}
	}

	switch a.Type {
	case exptypes.AnnotationIndex:
		ag[p].Index[a.Key] = string(v)
	case exptypes.AnnotationIndexDescriptor:
		ag[p].IndexDescriptor[a.Key] = string(v)
	case exptypes.AnnotationManifest:
		ag[p].Manifest[a.Key] = string(v)
	case exptypes.AnnotationManifestDescriptor:
		ag[p].ManifestDescriptor[a.Key] = string(v)
	default:
		return fmt.Errorf("unrecognized annotation type %s", a.Type)
	}
	return nil
}

func (ag AnnotationsGroup) Platform(p *ocispecs.Platform) *Annotations {
	res := &Annotations{
		IndexDescriptor:    make(map[string]string),
		Index:              make(map[string]string),
		Manifest:           make(map[string]string),
		ManifestDescriptor: make(map[string]string),
	}

	ps := []string{""}
	if p != nil {
		ps = append(ps, platforms.Format(*p))
	}

	for _, a := range ag {
		for k, v := range a.Index {
			res.Index[k] = v
		}
		for k, v := range a.IndexDescriptor {
			res.IndexDescriptor[k] = v
		}
	}
	for _, pk := range ps {
		if _, ok := ag[pk]; !ok {
			continue
		}

		for k, v := range ag[pk].Manifest {
			res.Manifest[k] = v
		}
		for k, v := range ag[pk].ManifestDescriptor {
			res.ManifestDescriptor[k] = v
		}
	}
	return res
}

func (ag AnnotationsGroup) Merge(other AnnotationsGroup) AnnotationsGroup {
	if other == nil {
		return ag
	}

	for k, v := range other {
		if _, ok := ag[k]; ok {
			ag[k].merge(v)
		} else {
			ag[k] = v
		}
	}
	return ag
}

func (a *Annotations) merge(other *Annotations) {
	if other == nil {
		return
	}

	for k, v := range other.Index {
		a.Index[k] = v
	}
	for k, v := range other.IndexDescriptor {
		a.IndexDescriptor[k] = v
	}
	for k, v := range other.Manifest {
		a.Manifest[k] = v
	}
	for k, v := range other.ManifestDescriptor {
		a.ManifestDescriptor[k] = v
	}
}
