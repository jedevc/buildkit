package proc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/docker/distribution/reference"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/frontend"
	"github.com/moby/buildkit/frontend/attest"
	"github.com/moby/buildkit/solver"
	"github.com/moby/buildkit/solver/llbsolver"
	"github.com/pkg/errors"
)

func SBOMProcessor(scannerRef reference.Named, bundleTargets []string) llbsolver.Processor {
	return func(ctx context.Context, res *frontend.Result, s *llbsolver.Solver, j *solver.Job) (*frontend.Result, error) {
		if _, ok := res.Metadata[exptypes.ExporterHasSBOM]; ok {
			return res, nil
		}

		var ps exptypes.Platforms
		platformsBytes, ok := res.Metadata[exptypes.ExporterPlatformsKey]
		if !ok {
			return nil, errors.Errorf("unable to collect multiple refs, missing platforms mapping")
		}
		if len(platformsBytes) > 0 {
			if err := json.Unmarshal(platformsBytes, &ps); err != nil {
				return nil, errors.Wrapf(err, "failed to parse platforms passed to sbom processor")
			}
		}

		bundles := map[string][]string{}
		for _, p := range ps.Platforms {
			bundlesBytes, ok := res.Metadata[fmt.Sprintf("%s/%s", exptypes.ExporterBundles, p.ID)]
			if !ok {
				continue
			}

			if ok && len(bundlesBytes) > 0 {
				var bundle []string
				if err := json.Unmarshal(bundlesBytes, &bundle); err != nil {
					return nil, errors.Wrapf(err, "failed to parse bundles passed to exporter")
				}
				bundles[p.ID] = bundle
			}
		}

		scanner, err := attest.CreateSBOMScanner(ctx, s.Bridge(j), scannerRef)
		if err != nil {
			return nil, err
		}

		if scanner != nil {
			for _, p := range ps.Platforms {
				ref, ok := res.Refs[p.ID]
				if !ok {
					return nil, errors.Errorf("could not find ref %s", p.ID)
				}
				defop, err := llb.NewDefinitionOp(ref.Definition())
				if err != nil {
					return nil, err
				}
				st := llb.NewState(defop)

				var sboms []llb.State
				for _, bundle := range bundleTargets {
					var found *string
					for _, bundle2 := range bundles[p.ID] {
						bundle2parts := strings.Split(bundle2, ":")
						if len(bundle2parts) != 3 {
							continue
						}
						if bundle == bundle2parts[1] {
							found = &bundle2
						}
					}
					if found == nil {
						return nil, errors.Errorf("bundle %q not found", bundle)
					}

					defop, err := llb.NewDefinitionOp(res.Refs[*found].Definition())
					if err != nil {
						return nil, err
					}
					st := llb.NewState(defop)
					sboms = append(sboms, st)
					delete(res.Refs, *found)
				}

				att, st, err := scanner(ctx, p.ID, attest.ScanTarget{State: st, SBOMs: sboms}, nil)
				if err != nil {
					return nil, err
				}

				def, err := st.Marshal(ctx)
				if err != nil {
					return nil, err
				}

				r, err := s.Bridge(j).Solve(ctx, frontend.SolveRequest{
					Definition: def.ToPB(),
				}, j.SessionID)
				if err != nil {
					return nil, err
				}
				res.AddAttestation(p.ID, att, r.Ref)
			}
		}

		return res, nil
	}
}
