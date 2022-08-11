package moby_buildkit_v1_frontend //nolint:revive

import (
	"github.com/moby/buildkit/solver/result"
	"github.com/pkg/errors"
)

func ToAttestationPB(a result.Attestation[*Ref]) (*Attestations_Attestation, error) {
	switch a := a.(type) {
	case *result.InTotoAttestation[*Ref]:
		subjects := []*InToto_Subject{}
		for _, subject := range a.Subjects {
			switch s := subject.(type) {
			case *result.InTotoSubjectRaw:
				subjects = append(subjects, &InToto_Subject{
					Subject: &InToto_Subject_Raw{
						Raw: &InToto_Subject_RawSubject{
							Name:   s.Name,
							Digest: s.Digest,
						},
					},
				})
			case *result.InTotoSubjectSelf:
				subjects = append(subjects, &InToto_Subject{
					Subject: &InToto_Subject_Self{
						Self: &InToto_Subject_SelfSubject{},
					},
				})
			default:
				return nil, errors.Errorf("unknown in toto subject type %T", s)
			}
		}

		intoto := &InToto{
			PredicateType: a.PredicateType,
			PredicatePath: a.PredicatePath,
			PredicateRef:  a.PredicateRef,
			Subjects:      subjects,
		}
		return &Attestations_Attestation{
			Attestation: &Attestations_Attestation_Intoto{intoto},
		}, nil
	default:
		return nil, errors.Errorf("unknown attestation type %T", a)
	}
}

func FromAttestationPB(a *Attestations_Attestation) (result.Attestation[*Ref], error) {
	switch a := a.Attestation.(type) {
	case *Attestations_Attestation_Intoto:
		subjects := []result.InTotoSubject{}
		for _, pbSubject := range a.Intoto.Subjects {
			switch pbSubject := pbSubject.Subject.(type) {
			case *InToto_Subject_Raw:
				subjects = append(subjects, &result.InTotoSubjectRaw{
					Name:   pbSubject.Raw.Name,
					Digest: pbSubject.Raw.Digest,
				})
			case *InToto_Subject_Self:
				subjects = append(subjects, &result.InTotoSubjectSelf{})
			default:
				return nil, errors.Errorf("unknown in toto subject type %T", pbSubject)
			}
		}

		return &result.InTotoAttestation[*Ref]{
			PredicateType: a.Intoto.PredicateType,
			PredicatePath: a.Intoto.PredicatePath,
			PredicateRef:  a.Intoto.PredicateRef,
			Subjects:      subjects,
		}, nil
	default:
		return nil, errors.Errorf("unknown attestation type %T", a)
	}
}
