package main

import (
	"context"
	"fmt"
	"os"

	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/frontend/gateway/grpcclient"
	"github.com/moby/buildkit/session/filesync"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/moby/buildkit/util/bklog"
	_ "github.com/moby/buildkit/util/grpcutil/encoding/proto"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
)

func main() {
	client, conn, err := grpcclient.NewFromEnvironment()
	if err != nil {
		bklog.L.Errorf("fatal error: %+v", err)
		panic(err)
	}

	err = export(appcontext.Context(), client, conn)
	if err != nil {
		bklog.L.Errorf("fatal error: %+v", err)
		panic(err)
	}
}

func export(ctx context.Context, c client.Client, conn *grpc.ClientConn) (err error) {
	result, err := c.Export(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get export")
	}
	if result == nil {
		return errors.New("no export result")
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

	stats, err := result.Ref.ReadDir(ctx, client.ReadDirRequest{Path: "/"})
	if err != nil {
		return errors.Wrap(err, "failed to read dir")
	}

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

	return nil
}
