package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/containerd/containerd/v2/core/content/proxy"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/frontend/gateway/grpcclient"
	"github.com/moby/buildkit/session/filesync"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/moby/buildkit/util/bklog"
	_ "github.com/moby/buildkit/util/grpcutil/encoding/proto"
	"github.com/moby/buildkit/util/staticfs"
	"github.com/pkg/errors"
	"github.com/tonistiigi/fsutil"
	"github.com/tonistiigi/fsutil/types"
	"google.golang.org/grpc"
)

func main() {
	if err := grpcclient.ExportFromEnvironment(appcontext.Context(), export); err != nil {
		bklog.L.Errorf("fatal error: %+v", err)
		panic(err)
	}
}

func export(ctx context.Context, c client.Client, conn *grpc.ClientConn, target exptypes.ExporterTarget, result *client.Result) (err error) {
	opts := c.BuildOpts().Opts
	if opts == nil {
		opts = map[string]string{}
	}

	var subdir string
	if v, ok := opts["subdir"]; ok {
		subdir = v
	}

	ps, err := exptypes.ParsePlatforms(result.Metadata)
	if err != nil {
		return err
	}
	if len(ps.Platforms) > 1 {
		return errors.New("multiple platforms not supported in this sample")
	}

	if result.Ref == nil {
		return errors.New("no ref in export result")
	}
	descs, err := result.Ref.GetRemote(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get remote descs")
	}
	fmt.Fprintln(os.Stderr, "exported descriptor:", descs)

	ccl := proxy.NewContentStore(conn)
	for _, desc := range descs {
		r, err := ccl.ReaderAt(ctx, desc)
		if err != nil {
			return errors.Wrap(err, "failed to get reader for exported content")
		}
		defer r.Close()

		sr := io.NewSectionReader(r, 0, r.Size())
		rr, err := gzip.NewReader(sr)
		if err != nil {
			return errors.Wrap(err, "failed to create gzip reader")
		}
		tr := tar.NewReader(rr)

		for {
			hdr, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return errors.Wrap(err, "failed to read tar header")
			}
			fmt.Fprintln(os.Stderr, "exported file:", hdr.Name)
		}

		r.Close()
	}

	stats, err := result.Ref.ReadDir(ctx, client.ReadDirRequest{Path: subdir})
	if err != nil {
		return errors.Wrap(err, "failed to read dir")
	}

	switch target {
	case exptypes.ExporterTargetNone:
	case exptypes.ExporterTargetFile:
		client := filesync.NewFileSendClient(conn)
		cc, err := client.DiffCopy(ctx)
		if err != nil {
			return err
		}
		w := filesync.NewStreamWriter(cc)

		for _, fi := range stats {
			path := fi.Path
			if fi.IsDir() {
				path += "/"
			}
			fmt.Fprintln(os.Stderr, path)
			_, err = fmt.Fprintln(w, path)
			if err != nil {
				return err
			}
		}

		if err := w.Close(); err != nil {
			return err
		}
	case exptypes.ExporterTargetDirectory:
		client := filesync.NewFileSendClient(conn)
		cc, err := client.DiffCopy(ctx)
		if err != nil {
			return err
		}

		w := &bytes.Buffer{}
		for _, fi := range stats {
			path := fi.Path
			if fi.IsDir() {
				path += "/"
			}
			fmt.Fprintln(os.Stderr, path)
			_, err = fmt.Fprintln(w, path)
			if err != nil {
				return err
			}
		}

		fs := staticfs.NewFS()
		fs.Add("foo", &types.Stat{Mode: 0644}, w.Bytes())
		return errors.WithStack(fsutil.Send(cc.Context(), cc, fs, nil))
	default:
		return errors.Errorf("unsupported export target: %s", target)
	}

	return nil
}
