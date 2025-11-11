package main

import (
	"context"
	"fmt"
	"os"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/frontend/gateway/grpcclient"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/moby/buildkit/util/bklog"
	_ "github.com/moby/buildkit/util/grpcutil/encoding/proto"
	"github.com/pkg/errors"
)

func main() {
	if err := grpcclient.RunFromEnvironment(appcontext.Context(), export); err != nil {
		bklog.L.Errorf("fatal error: %+v", err)
		panic(err)
	}
}

func export(ctx context.Context, c client.Client) (_ *client.Result, err error) {
	st := llb.Image("busybox:latest")

	def, err := st.Marshal(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal state")
	}

	r, err := c.Solve(ctx, client.SolveRequest{
		Definition: def.ToPB(),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to solve")
	}

	stats, err := r.Ref.ReadDir(ctx, client.ReadDirRequest{Path: "/"})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read dir")
	}

	for _, fi := range stats {
		path := fi.Path
		if fi.IsDir() {
			path += "/"
		}
		fmt.Fprintln(os.Stderr, path)
	}

	return nil, nil
}
