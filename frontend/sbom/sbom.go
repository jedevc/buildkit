package sbom

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/docker/distribution/reference"
	intoto "github.com/in-toto/in-toto-golang/in_toto"
	"github.com/moby/buildkit/client/llb"
	gatewaypb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/result"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type Scanner[T any] func(ctx context.Context, resolver llb.ImageMetaResolver, target *result.Result[T], keys []string) error

func CreateScanner[T any](scanner reference.Named, toState func(context.Context, T) (llb.State, error), fromState func(context.Context, llb.State) (T, error)) Scanner[T] {
	if scanner == nil {
		return nil
	}

	return func(ctx context.Context, resolver llb.ImageMetaResolver, target *result.Result[T], keys []string) error {
		scanner = reference.TagNameOnly(scanner)
		_, dt, err := resolver.ResolveImageConfig(ctx, scanner.String(), llb.ResolveImageConfigOpt{})
		if err != nil {
			return err
		}

		var cfg ocispecs.Image
		if err := json.Unmarshal(dt, &cfg); err != nil {
			return err
		}
		if len(cfg.Config.Cmd) == 0 {
			return fmt.Errorf("scanner %s does not have cmd", scanner.String())
		}

		if len(keys) == 0 {
			return nil
		}

		srcDir := "/run/src/"
		outDir := "/run/out/"

		eg, ctx := errgroup.WithContext(ctx)
		for _, k := range keys {
			k := k
			ref, ok := target.Refs[k]
			if !ok {
				return errors.Errorf("could not find ref %s", k)
			}

			st, err := toState(ctx, ref)
			if err != nil {
				return err
			}

			eg.Go(func() error {
				runsbom := llb.Image(scanner.String()).Run(
					llb.Dir(cfg.Config.WorkingDir),
					llb.AddEnv("BUILDKIT_SCAN_SOURCES", srcDir),
					llb.AddEnv("BUILDKIT_SCAN_DESTINATIONS", outDir),
					llb.Args(cfg.Config.Cmd),
					llb.WithCustomName(fmt.Sprintf("[%s] generating sbom using %s", k, scanner.String())))

				kp := strings.ReplaceAll(k, "/", "-")
				runsbom.AddMount(path.Join(srcDir, kp), st)
				stsbom := runsbom.AddMount(path.Join(outDir, kp), llb.Scratch())

				r, err := fromState(ctx, stsbom)
				if err != nil {
					return err
				}

				target.AddAttestation(k, result.Attestation{
					Kind: gatewaypb.AttestationKindInToto,
					Path: "/spdx.json",
					InToto: result.InTotoAttestation{
						PredicateType: intoto.PredicateSPDX,
					},
				}, r)
				return nil
			})
		}
		return eg.Wait()
	}
}
