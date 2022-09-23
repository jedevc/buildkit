package attest

import (
	"encoding/json"
	"io"

	gatewaypb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/result"
	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

type Bundle []Attestation

type Attestation struct {
	Kind string `json:"kind"`

	Path string `json:"path"`

	InToto InTotoAttestation `json:"in-toto,omitempty"`
}

type InTotoAttestation struct {
	PredicateType string          `json:"predicate-type"`
	Subjects      []InTotoSubject `json:"subjects,omitempty"`
}

type InTotoSubject struct {
	Kind string `json:"kind"`

	Name   string          `json:"name"`
	Digest []digest.Digest `json:"digest"`
}

func Load(r io.Reader) (Bundle, error) {
	var bundle Bundle
	dec := json.NewDecoder(r)
	for {
		var att Attestation
		err := dec.Decode(&att)
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		bundle = append(bundle, att)
	}

	if len(bundle) == 0 {
		return nil, errors.New("empty attestation bundle")
	}
	return bundle, nil
}

func (bundle Bundle) Unpack() ([]result.Attestation, error) {
	atts := make([]result.Attestation, len(bundle))
	for i, att := range bundle {
		kind, ok := AttestationKinds[att.Kind]
		if !ok {
			return nil, errors.Errorf("unknown attestation kind %q in bundle", kind)
		}

		switch kind {
		case gatewaypb.AttestationKindBundle:
			atts[i] = result.Attestation{
				Kind: kind,
				Path: att.Path,
			}
		case gatewaypb.AttestationKindInToto:
			subjects := make([]result.InTotoSubject, len(att.InToto.Subjects))
			for i, sub := range att.InToto.Subjects {
				kind, ok := InTotoSubjectKinds[sub.Kind]
				if !ok {
					return nil, errors.Errorf("unknown in-toto subject kind %q in bundle", kind)
				}
				subjects[i] = result.InTotoSubject{
					Kind:   kind,
					Name:   sub.Name,
					Digest: sub.Digest,
				}
			}

			atts[i] = result.Attestation{
				Kind: kind,
				Path: att.Path,
				InToto: result.InTotoAttestation{
					PredicateType: att.InToto.PredicateType,
					Subjects:      subjects,
				},
			}
		}
	}

	return atts, nil
}

var AttestationKinds = map[string]gatewaypb.AttestationKind{
	"bundle":  gatewaypb.AttestationKindBundle,
	"in-toto": gatewaypb.AttestationKindInToto,
}

var InTotoSubjectKinds = map[string]gatewaypb.InTotoSubjectKind{
	"self": gatewaypb.InTotoSubjectKindSelf,
	"raw":  gatewaypb.InTotoSubjectKindRaw,
}
