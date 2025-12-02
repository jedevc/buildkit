package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/containerd/containerd/v2/core/content/proxy"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/frontend/gateway/grpcclient"
	"github.com/moby/buildkit/session/filesync"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/moby/buildkit/util/bklog"
	_ "github.com/moby/buildkit/util/grpcutil/encoding/proto"
	"github.com/moby/buildkit/util/staticfs"
	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/tonistiigi/fsutil"
	fstypes "github.com/tonistiigi/fsutil/types"
	"google.golang.org/grpc"
)

func main() {
	if err := grpcclient.ExportFromEnvironment(appcontext.Context(), export); err != nil {
		bklog.L.Errorf("fatal error: %+v", err)
		panic(err)
	}
}

type report struct {
	Opts      map[string]string `json:"opts"`
	Target    string            `json:"target"`
	Platforms []string          `json:"platforms"`

	Refs map[string]*reportRef `json:"refs"`
}

type reportRef struct {
	Config json.RawMessage `json:"config"`

	AllFiles []string `json:"all_files"`

	Layers     []digest.Digest            `json:"layers"`
	LayerFiles map[digest.Digest][]string `json:"layer_files"`
}

func export(ctx context.Context, c client.Client, conn *grpc.ClientConn, target exptypes.ExporterTarget, result *client.Result) (err error) {
	opts := c.BuildOpts().Opts
	if opts == nil {
		opts = map[string]string{}
	}
	store := proxy.NewContentStore(conn)

	report := &report{
		Opts:   opts,
		Target: string(target),
		Refs:   map[string]*reportRef{},
	}

	ps, err := exptypes.ParsePlatforms(result.Metadata)
	if err != nil {
		return err
	}
	for _, p := range ps.Platforms {
		report.Platforms = append(report.Platforms, p.ID)
		ref, ok := result.FindRef(p.ID)
		if !ok {
			return errors.Errorf("no ref for platform %s", p.ID)
		}

		reportRef := &reportRef{}
		report.Refs[p.ID] = reportRef

		config := exptypes.ParseKey(result.Metadata, exptypes.ExporterImageConfigKey, &p)
		reportRef.Config = config

		err := walkDir(ctx, ref, "/", func(path string, info *fstypes.Stat) error {
			reportRef.AllFiles = append(reportRef.AllFiles, path)
			return nil
		})
		if err != nil {
			return errors.Wrapf(err, "failed to walk ref for platform %s", p.ID)
		}

		descs, err := ref.GetRemote(ctx)
		if err != nil {
			return errors.Wrapf(err, "failed to get remote descs for platform %s", p.ID)
		}

		reportRef.LayerFiles = map[digest.Digest][]string{}
		for _, desc := range descs {
			reportRef.Layers = append(reportRef.Layers, desc.Digest)

			err := func() (rerr error) {
				r, err := store.ReaderAt(ctx, desc)
				if err != nil {
					return errors.Wrap(err, "failed to get reader for exported content")
				}
				defer func() {
					err := r.Close()
					if rerr == nil {
						rerr = err
					}
				}()

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
					reportRef.LayerFiles[desc.Digest] = append(reportRef.LayerFiles[desc.Digest], hdr.Name)
				}

				return nil
			}()
			if err != nil {
				return err
			}
		}
	}

	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return errors.Wrap(err, "failed to marshal report")
	}
	out = append(out, '\n')
	fmt.Fprint(os.Stderr, string(out))

	switch target {
	case exptypes.ExporterTargetNone:
	case exptypes.ExporterTargetFile:
		client := filesync.NewFileSendClient(conn)
		cc, err := client.DiffCopy(ctx)
		if err != nil {
			return err
		}
		w := filesync.NewStreamWriter(cc)
		_, err = fmt.Fprint(w, string(out))
		if err != nil {
			return err
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

		fs := staticfs.NewFS()
		fs.Add("report.json", &fstypes.Stat{Mode: 0644}, out)
		return errors.WithStack(fsutil.Send(cc.Context(), cc, fs, nil))
	default:
		return errors.Errorf("unsupported export target: %s", target)
	}

	return nil
}

func walkDir(ctx context.Context, ref client.Reference, root string, fn func(path string, info *fstypes.Stat) error) error {
	entries, err := ref.ReadDir(ctx, client.ReadDirRequest{Path: root})
	if err != nil {
		return err
	}
	for _, entry := range entries {
		entryPath := path.Join(root, entry.Path)
		if entry.IsDir() {
			entryPath += "/"
		}
		if err := fn(entryPath, entry); err != nil {
			return err
		}
		if entry.IsDir() {
			if err := walkDir(ctx, ref, entryPath, fn); err != nil {
				return err
			}
		}
	}
	return nil
}
