package proc

import (
	"context"
	"encoding/json"

	"github.com/docker/distribution/reference"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/frontend"
	"github.com/moby/buildkit/frontend/attest"
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
		scanner := attest.CreateSBOMScanner(scannerRef, toState, fromState)

		if err := scanner(ctx, s.Bridge(j), res, ps.IDs()); err != nil {
			return nil, err
		}

		return res, nil
	}
}
