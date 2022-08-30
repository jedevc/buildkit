package proc

import (
	"context"
	"encoding/json"

	"github.com/docker/distribution/reference"
	intoto "github.com/in-toto/in-toto-golang/in_toto"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/frontend"
	gatewaypb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/frontend/sbom"
	"github.com/moby/buildkit/solver"
	"github.com/moby/buildkit/solver/llbsolver"
	"github.com/pkg/errors"
)

func SBOMProcessor(scannerRef reference.Named) llbsolver.Processor {
	return func(ctx context.Context, res *frontend.Result, s *llbsolver.Solver, j *solver.Job) (*frontend.Result, error) {
		platformsBytes, ok := res.Metadata[exptypes.ExporterPlatformsKey]
		if !ok {
			return nil, errors.Errorf("unable to collect multiple refs, missing platforms mapping")
		}

		var ps exptypes.Platforms
		if len(platformsBytes) > 0 {
			if err := json.Unmarshal(platformsBytes, &ps); err != nil {
				return nil, errors.Wrapf(err, "failed to parse platforms passed to sbom processor")
			}
		}

		var keys []string
	refloop:
		for _, p := range ps.Platforms {
			for _, attestation := range res.Attestations[p.ID] {
				switch attestation.Kind {
				case gatewaypb.AttestationKindInToto:
					if attestation.InToto.PredicateType == intoto.PredicateSPDX {
						continue refloop
					}
				}
			}
			keys = append(keys, p.ID)
		}

		toState := func(ctx context.Context, ref solver.ResultProxy) (llb.State, error) {
			defop, err := llb.NewDefinitionOp(ref.Definition())
			if err != nil {
				return llb.State{}, err
			}
			return llb.NewState(defop), nil
		}
		fromState := func(ctx context.Context, ref llb.State) (solver.ResultProxy, error) {
			def, err := ref.Marshal(ctx)
			if err != nil {
				return nil, err
			}

			res, err := s.Bridge(j).Solve(ctx, frontend.SolveRequest{
				Definition: def.ToPB(),
			}, j.SessionID)
			if err != nil {
				return nil, err
			}
			return res.Ref, nil
		}
		scanner := sbom.CreateScanner(scannerRef, toState, fromState)

		if err := scanner(ctx, s.Bridge(j), res, keys); err != nil {
			return nil, err
		}

		return res, nil
	}
}
