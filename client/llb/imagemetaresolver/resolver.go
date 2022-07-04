package imagemetaresolver

import (
	"context"
	"net/http"
	"sync"

	"github.com/containerd/containerd/platforms"
	"github.com/containerd/containerd/remotes"
	"github.com/containerd/containerd/remotes/docker"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/moby/buildkit/util/imageutil"
	"github.com/moby/buildkit/version"
	"github.com/moby/locker"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

var defaultImageMetaResolver llb.ImageMetaResolver
var defaultImageMetaResolverOnce sync.Once

var WithDefault = imageOptionFunc(func(ii *llb.ImageInfo) {
	llb.WithMetaResolver(Default()).SetImageOption(ii)
})

type imageMetaResolverOpts struct {
	platform *ocispecs.Platform
}

type ImageMetaResolverOpt func(o *imageMetaResolverOpts)

func WithDefaultPlatform(p *ocispecs.Platform) ImageMetaResolverOpt {
	return func(o *imageMetaResolverOpts) {
		o.platform = p
	}
}

func New(with ...ImageMetaResolverOpt) llb.ImageMetaResolver {
	var opts imageMetaResolverOpts
	for _, f := range with {
		f(&opts)
	}
	headers := http.Header{}
	headers.Set("User-Agent", version.UserAgent())
	return &imageMetaResolver{
		resolver: docker.NewResolver(docker.ResolverOptions{
			Client:  http.DefaultClient,
			Headers: headers,
		}),
		platform: opts.platform,
		buffer:   contentutil.NewBuffer(),
		cache:    map[string]resolveResult{},
		locker:   locker.New(),
	}
}

func Default() llb.ImageMetaResolver {
	defaultImageMetaResolverOnce.Do(func() {
		defaultImageMetaResolver = New()
	})
	return defaultImageMetaResolver
}

type imageMetaResolver struct {
	resolver remotes.Resolver
	buffer   contentutil.Buffer
	platform *ocispecs.Platform
	locker   *locker.Locker
	cache    map[string]resolveResult
}

type resolveResult struct {
	data  []byte
	mdgst digest.Digest
	dgst  digest.Digest
}

func (imr *imageMetaResolver) ResolveImageConfig(ctx context.Context, ref string, opt llb.ResolveImageConfigOpt) (digest.Digest, digest.Digest, []byte, error) {
	imr.locker.Lock(ref)
	defer imr.locker.Unlock(ref)

	switch t := opt.Type.(type) {
	case *llb.ResolveConfigType:
		platform := t.Platform
		if platform == nil {
			platform = imr.platform
		}

		k := imr.key("config", ref, platform)
		if res, ok := imr.cache[k]; ok {
			return res.mdgst, res.dgst, res.data, nil
		}

		mdgst, dgst, config, err := imageutil.Config(ctx, ref, imr.resolver, imr.buffer, nil, platform)
		if err != nil {
			return "", "", nil, err
		}
		imr.cache[k] = resolveResult{mdgst: mdgst, dgst: dgst, data: config}
		return mdgst, dgst, config, nil
	case *llb.ResolveManifestType:
		platform := t.Platform
		if platform == nil {
			platform = imr.platform
		}

		k := imr.key("manifest", ref, platform)
		if res, ok := imr.cache[k]; ok {
			return res.mdgst, res.dgst, res.data, nil
		}

		mdgst, dgst, config, err := imageutil.Manifest(ctx, ref, imr.resolver, imr.buffer, nil, platform)
		if err != nil {
			return "", "", nil, err
		}
		imr.cache[k] = resolveResult{mdgst: mdgst, dgst: dgst, data: config}
		return mdgst, dgst, config, nil
	case *llb.ResolveIndexType:
		k := imr.key("index", ref, nil)
		if res, ok := imr.cache[k]; ok {
			return res.mdgst, res.dgst, res.data, nil
		}

		mdgst, dgst, config, err := imageutil.Index(ctx, ref, imr.resolver, imr.buffer, nil)
		if err != nil {
			return "", "", nil, err
		}
		imr.cache[k] = resolveResult{mdgst: mdgst, dgst: dgst, data: config}
		return mdgst, dgst, config, nil
	}

	panic("uh oh")
}

func (imr *imageMetaResolver) key(tp string, ref string, platform *ocispecs.Platform) string {
	ref = tp + ref
	if platform != nil {
		ref += platforms.Format(*platform)
	}
	return ref
}

type imageOptionFunc func(*llb.ImageInfo)

func (fn imageOptionFunc) SetImageOption(ii *llb.ImageInfo) {
	fn(ii)
}
