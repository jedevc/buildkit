package attest

import (
	"context"
	"encoding/json"
	"fmt"
	"path"

	"github.com/docker/distribution/reference"
	"github.com/moby/buildkit/client/llb"
	gatewaypb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/result"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

// Scanner is a function type for scanning the contents of a state and
// returning a new attestation and state representing the scan results.
//
// A scanner is designed a scan a single state, however, additional states can
// also be attached, for attaching additional information, such as scans of
// build-contexts or multi-stage builds. Handling these separately allows the
// scanner to optionally ignore these or to mark them as such in the
// attestation.
type Scanner func(ctx context.Context, name string, ref llb.State, extras map[string]llb.State) (result.Attestation, llb.State, error)

func CreateSBOMScanner(ctx context.Context, resolver llb.ImageMetaResolver, scanner reference.Named) (Scanner, error) {
	if scanner == nil {
		return nil, nil
	}

	scanner = reference.TagNameOnly(scanner)
	_, dt, err := resolver.ResolveImageConfig(ctx, scanner.String(), llb.ResolveImageConfigOpt{})
	if err != nil {
		return nil, err
	}

	var cfg ocispecs.Image
	if err := json.Unmarshal(dt, &cfg); err != nil {
		return nil, err
	}
	if len(cfg.Config.Cmd) == 0 {
		return nil, fmt.Errorf("scanner %s does not have cmd", scanner.String())
	}

	return func(ctx context.Context, name string, ref llb.State, extras map[string]llb.State) (result.Attestation, llb.State, error) {
		srcDir := "/run/src/"
		outDir := "/run/out/"

		args := []string{}
		args = append(args, cfg.Config.Entrypoint...)
		args = append(args, cfg.Config.Cmd...)
		runsbom := llb.Image(scanner.String()).Run(
			llb.Dir(cfg.Config.WorkingDir),
			llb.AddEnv("BUILDKIT_SCAN_SOURCE", path.Join(srcDir, "core")),
			llb.AddEnv("BUILDKIT_SCAN_SOURCE_EXTRAS", path.Join(srcDir, "extras/")),
			llb.AddEnv("BUILDKIT_SCAN_DESTINATION", outDir),
			llb.Args(args),
			llb.WithCustomName(fmt.Sprintf("[%s] generating sbom using %s", name, scanner.String())))

		runsbom.AddMount(path.Join(srcDir, "core"), ref)
		for k, extra := range extras {
			runsbom.AddMount(path.Join(srcDir, "extras", k), extra)
		}

		stsbom := runsbom.AddMount(outDir, llb.Scratch())
		return result.Attestation{
			Kind: gatewaypb.AttestationKindBundle,
		}, stsbom, nil
	}, nil
}
