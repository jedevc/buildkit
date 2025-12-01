package gateway

import (
	"context"
	"fmt"
	"io"

	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/filesync"
	"github.com/moby/buildkit/util/bklog"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type SyncTarget[Req any] struct {
	Caller     session.Caller
	ExporterID int
}

const (
	// XXX: duped
	keyExporterID = "buildkit-attachable-exporter-id"
)

func (sp *SyncTarget[Req]) Register(server *grpc.Server) {
	filesync.RegisterFileSendServer(server, sp)
}

func (sp *SyncTarget[Req]) exportCtx(ctx context.Context) context.Context {
	opts, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		opts = make(map[string][]string)
	}
	if existingVal, ok := opts[keyExporterID]; ok {
		bklog.G(ctx).Warnf("overwriting grpc metadata key %q from value %+v to %+v", keyExporterID, existingVal, sp.ExporterID)
	}
	opts[keyExporterID] = []string{fmt.Sprint(sp.ExporterID)}
	ctx = metadata.NewOutgoingContext(ctx, opts)

	return ctx
}

// XXX: is there a better way to do this?
func (sp *SyncTarget[Req]) DiffCopy(stream filesync.FileSend_DiffCopyServer) error {
	method := session.MethodURL(filesync.FileSend_ServiceDesc.ServiceName, "diffcopy")
	if !sp.Caller.Supports(method) {
		return errors.Errorf("method %s not supported by the client", method)
	}

	client := filesync.NewFileSendClient(sp.Caller.Conn())
	ctx := sp.exportCtx(stream.Context())

	eg, ctx := errgroup.WithContext(ctx)

	cc, err := client.DiffCopy(ctx)
	if err != nil {
		return err
	}

	eg.Go(func() error {
		for {
			var msg Req
			err := stream.RecvMsg(&msg)
			if errors.Is(err, io.EOF) {
				return cc.CloseSend()
			}
			if err != nil {
				return err
			}
			if err := cc.SendMsg(&msg); err != nil {
				return err
			}
		}
	})
	eg.Go(func() error {
		for {
			var msg Req
			err := cc.RecvMsg(&msg)
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := stream.SendMsg(&msg); err != nil {
				return err
			}
		}
	})
	err = eg.Wait()
	if err != nil {
		cc.CloseSend()
	}
	return err
}
