package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/containerd/containerd/v2/core/leases"
	"github.com/distribution/reference"
	"github.com/moby/buildkit/cache"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/sourceresolver"
	"github.com/moby/buildkit/executor"
	"github.com/moby/buildkit/exporter"
	"github.com/moby/buildkit/exporter/containerimage"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/frontend"
	"github.com/moby/buildkit/frontend/dockerui"
	"github.com/moby/buildkit/frontend/gateway"
	"github.com/moby/buildkit/frontend/gateway/container"
	"github.com/moby/buildkit/frontend/gateway/forwarder"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/filesync"
	"github.com/moby/buildkit/solver"
	opspb "github.com/moby/buildkit/solver/pb"
	"github.com/moby/buildkit/util/apicaps"
	"github.com/moby/buildkit/util/progress/logs"
	"github.com/moby/buildkit/worker"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	fstypes "github.com/tonistiigi/fsutil/types"
)

const (
	keySource = "source"
)

type Opt struct {
	CacheManager   cache.Manager
	SessionManager *session.Manager
	ImageWriter    *containerimage.ImageWriter
	LeaseManager   leases.Manager

	WorkerInfo client.WorkerInfo
}

type gatewayExporter struct {
	opt Opt
}

func New(opt Opt) (exporter.Exporter, error) {
	im := &gatewayExporter{opt: opt}
	return im, nil
}

func (e *gatewayExporter) Resolve(ctx context.Context, id int, frontendAttrs map[string]string, exporterAttrs map[string]string, target exptypes.ExporterTarget) (exporter.ExporterInstance, error) {
	i := &gatewayExporterInstance{
		gatewayExporter: e,
		id:              id,
		frontendAttrs:   frontendAttrs,
		target:          target,
		attrs:           exporterAttrs,
		workerInfo: workerInfo{
			cm:   e.opt.CacheManager,
			info: e.opt.WorkerInfo,
		},
	}

	for k, v := range exporterAttrs {
		switch k {
		case keySource:
			i.image = v

		default:
			if i.meta == nil {
				i.meta = make(map[string][]byte)
			}
			i.meta[k] = []byte(v)
		}
	}
	return i, nil
}

type gatewayExporterInstance struct {
	*gatewayExporter
	id            int
	workerInfo    worker.Infos
	target        exptypes.ExporterTarget
	frontendAttrs map[string]string
	attrs         map[string]string

	image string

	meta map[string][]byte
}

func (e *gatewayExporterInstance) ID() int {
	return e.id
}

func (e *gatewayExporterInstance) Name() string {
	return fmt.Sprintf("exporting using %s", e.image)
}

func (e *gatewayExporterInstance) Type() string {
	return client.ExporterGateway
}

func (e *gatewayExporterInstance) Attrs() map[string]string {
	return e.attrs
}

func (e *gatewayExporterInstance) Target() exptypes.ExporterTarget {
	return e.target
}

func (e *gatewayExporterInstance) Config() *exporter.Config {
	return exporter.NewConfig()
}

// XXX: dedupe with frontend/gateway/gateway.go
func (e *gatewayExporterInstance) getImage(ctx context.Context, llbBridge frontend.FrontendLLBBridge, exec executor.Executor, sessionID string, source string) (st *llb.State, img *dockerspec.DockerOCIImage, mfstDigest digest.Digest, err error) {
	c, err := forwarder.LLBBridgeToGatewayClient(ctx, llbBridge, exec, e.frontendAttrs, nil, e.workerInfo, sessionID, e.opt.SessionManager)
	if err != nil {
		return nil, nil, "", err
	}
	dc, err := dockerui.NewClient(c)
	if err != nil {
		return nil, nil, "", err
	}
	nc, err := dc.NamedContext(source, dockerui.ContextOpt{
		CaptureDigest: &mfstDigest,
	})

	if err != nil {
		return nil, nil, "", err
	}
	if nc != nil {
		var dockerImage *dockerspec.DockerOCIImage
		st, dockerImage, err = nc.Load(ctx)
		if err != nil {
			return nil, nil, "", err
		}
		if dockerImage != nil {
			img = dockerImage
		}
	}
	if st == nil {
		sourceRef, err := reference.ParseNormalizedNamed(source)
		if err != nil {
			return nil, nil, "", err
		}

		imr := sourceresolver.NewImageMetaResolver(llbBridge)
		ref, dgst, config, err := imr.ResolveImageConfig(ctx, reference.TagNameOnly(sourceRef).String(), sourceresolver.Opt{})
		if err != nil {
			return nil, nil, "", err
		}

		sourceRef, err = reference.ParseNormalizedNamed(ref)
		if err != nil {
			return nil, nil, "", err
		}

		mfstDigest = dgst

		if err := json.Unmarshal(config, &img); err != nil {
			return nil, nil, "", err
		}

		if dgst != "" {
			sourceRef, err = reference.WithDigest(sourceRef, dgst)
			if err != nil {
				return nil, nil, "", err
			}
		}

		// src := llb.Image*(sourceRef.String(), &markTypeFrontend{})
		src := llb.Image(sourceRef.String())
		st = &src
	}

	return st, img, mfstDigest, nil
}

func (e *gatewayExporterInstance) Export(ctx context.Context, llbBridge frontend.FrontendLLBBridge, exec executor.Executor, src *exporter.Source, inlineCache exptypes.InlineCache, sessionID string) (_ map[string]string, descref exporter.DescriptorReference, err error) {
	st, img, mfstDigest, err := e.getImage(ctx, llbBridge, exec, sessionID, e.image)
	if err != nil {
		return nil, nil, err
	}
	_ = mfstDigest

	def, err := st.Marshal(ctx)
	if err != nil {
		return nil, nil, err
	}

	res, err := llbBridge.Solve(ctx, frontend.SolveRequest{
		Definition: def.ToPB(),
	}, sessionID)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		ctx := context.WithoutCancel(ctx)
		res.EachRef(func(ref solver.ResultProxy) error {
			return ref.Release(ctx)
		})
	}()
	if res.Ref == nil {
		return nil, nil, errors.Errorf("gateway source didn't return default result")
	}
	frontendDef := res.Ref.Definition()
	r, err := res.Ref.Result(ctx)
	if err != nil {
		return nil, nil, err
	}
	workerRef, ok := r.Sys().(*worker.WorkerRef)
	if !ok {
		return nil, nil, errors.Errorf("invalid ref: %T", r.Sys())
	}
	rootFS, err := workerRef.Worker.CacheManager().New(ctx, workerRef.ImmutableRef, session.NewGroup(sessionID))
	if err != nil {
		return nil, nil, err
	}
	defer rootFS.Release(context.TODO())

	args := []string{"/run"}
	env := []string{}
	cwd := "/"
	if img.Config.Env != nil {
		env = img.Config.Env
	}
	if img.Config.Entrypoint != nil {
		args = img.Config.Entrypoint
	}
	if img.Config.WorkingDir != "" {
		cwd = img.Config.WorkingDir
	}
	i := 0
	for k, v := range e.meta {
		env = append(env, fmt.Sprintf("BUILDKIT_FRONTEND_OPT_%d", i)+"="+k+"="+string(v))
		i++
	}

	env = append(env, "BUILDKIT_SESSION_ID="+sessionID)

	dt, err := json.Marshal(e.workerInfo)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to marshal workers array")
	}
	env = append(env, "BUILDKIT_WORKERS="+string(dt))

	env = append(env, "BUILDKIT_EXPORTEDPRODUCT="+apicaps.ExportedProduct)

	env = append(env, "BUILDKIT_EXPORTER_TARGET="+e.Target().String())

	meta := executor.Meta{
		Env:  env,
		Args: args,
		Cwd:  cwd,
		// ReadonlyRootFS:            readonly,
		RemoveMountStubsRecursive: true,
	}

	if v, ok := img.Config.Labels["moby.buildkit.frontend.network.none"]; ok {
		if ok, _ := strconv.ParseBool(v); ok {
			meta.NetMode = opspb.NetMode_NONE
		}
	}

	// curCaps := getCaps(img.Config.Labels["moby.buildkit.frontend.caps"])
	// addCapsForKnownFrontends(curCaps, mfstDigest)
	// reqCaps := getCaps(opts["frontend.caps"])
	// if len(inputs) > 0 {
	// 	reqCaps["moby.buildkit.frontend.inputs"] = struct{}{}
	// }
	//
	// for c := range reqCaps {
	// 	if _, ok := curCaps[c]; !ok {
	// 		return nil, stack.Enable(grpcerrors.WrapCode(errdefs.NewUnsupportedFrontendCapError(c), codes.Unimplemented))
	// 	}
	// }

	caller, err := e.opt.SessionManager.Get(ctx, sessionID, false)
	if err != nil {
		return nil, nil, err
	}

	lbf := gateway.NewBridgeForwarder(ctx, llbBridge, exec, e.workerInfo, nil, sessionID, e.opt.SessionManager)
	result := src.FrontendResult
	// defer func() {
	// 	result.EachRef(func(ref solver.ResultProxy) error {
	// 		return ref.Release(ctx)
	// 	})
	// }()
	lbf.SetResult(result)

	attachables := []session.Attachable{}
	switch e.target {
	case exptypes.ExporterTargetFile:
		attachables = append(attachables, &SyncTarget[filesync.BytesMessage]{Caller: caller, ExporterID: e.id})
	case exptypes.ExporterTargetDirectory:
		attachables = append(attachables, &SyncTarget[fstypes.Packet]{Caller: caller, ExporterID: e.id})
	}
	attachables = append(attachables, &Store{e.opt.ImageWriter.ContentStore()})
	ctx = lbf.Serve(ctx, attachables...)
	// defer lbf.conn.Close() // XXX:
	// defer lbf.Discard()

	mdmnt, release, err := gateway.MetadataMount(frontendDef)
	if err != nil {
		return nil, nil, err
	}
	if release != nil {
		defer release()
	}
	var mnts []executor.Mount
	if mdmnt != nil {
		mnts = append(mnts, *mdmnt)
	}

	stdout, stderr, flush := logs.NewLogStreams(ctx, os.Getenv("BUILDKIT_DEBUG_EXEC_OUTPUT") == "1")
	defer stdout.Close()
	defer stderr.Close()
	defer func() {
		if err != nil {
			flush()
		}
	}()

	_, err = exec.Run(ctx, "", container.MountWithSession(rootFS, session.NewGroup(sessionID)), mnts, executor.ProcessInfo{Meta: meta, Stdin: lbf.Stdin, Stdout: lbf.Stdout, Stderr: stderr}, nil)
	if err != nil {
		return nil, nil, err

		// if errdefs.IsCanceled(ctx, err) && lbf.isErrServerClosed {
		// 	err = errors.Errorf("frontend grpc server closed unexpectedly")
		// }
		// An existing error (set via Return rpc) takes
		// precedence over this error, which in turn takes
		// precedence over a success reported via Return.
		// lbf.mu.Lock()
		// if lbf.err == nil {
		// 	lbf.result = nil
		// 	lbf.err = err
		// }
		// lbf.mu.Unlock()
	}

	// return nil, nil, fmt.Errorf("not implemented but ran")

	return nil, nil, nil
}

type workerInfo struct {
	cm   cache.Manager
	info client.WorkerInfo
}

func (i workerInfo) DefaultCacheManager() (cache.Manager, error) {
	return i.cm, nil
}

func (i workerInfo) WorkerInfos() []client.WorkerInfo {
	return []client.WorkerInfo{i.info}
}
