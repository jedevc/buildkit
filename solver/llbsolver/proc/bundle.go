package proc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/frontend"
	gatewaypb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver"
	"github.com/moby/buildkit/solver/llbsolver"
	"github.com/moby/buildkit/solver/result"
	"github.com/pkg/errors"
)

func BundleProcessor(bundleTargets []string) llbsolver.Processor {
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

		for _, p := range ps.Platforms {
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
				res.AddAttestation(p.ID, result.Attestation{
					Kind: gatewaypb.AttestationKindBundle,
				}, res.Refs[*found])
				delete(res.Refs, *found)
			}
		}

		return res, nil
	}
}
