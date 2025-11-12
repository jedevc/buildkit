package gateway

import (
	"context"
	"fmt"
	"io"

	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/filesync"
	"github.com/moby/buildkit/util/bklog"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type SyncTarget struct {
	Caller     session.Caller
	ExporterID int
}

const (
	keyExporterID = "buildkit-attachable-exporter-id"
)

func (sp *SyncTarget) Register(server *grpc.Server) {
	filesync.RegisterFileSendServer(server, sp)
}

func (sp *SyncTarget) exportCtx(ctx context.Context) context.Context {
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

func (sp *SyncTarget) DiffCopy(stream filesync.FileSend_DiffCopyServer) error {
	method := session.MethodURL(filesync.FileSend_ServiceDesc.ServiceName, "diffcopy")
	if !sp.Caller.Supports(method) {
		return errors.Errorf("method %s not supported by the client", method)
	}

	client := filesync.NewFileSendClient(sp.Caller.Conn())
	ctx := sp.exportCtx(stream.Context())
	cc, err := client.DiffCopy(ctx)
	if err != nil {
		return err
	}

	done := make(chan error)
	go func() {
		for {
			msg, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				close(done)
				return
			}
			if err != nil {
				done <- err
				return
			}
			if err := cc.Send(msg); err != nil {
				done <- err
				return
			}
		}
	}()
	done2 := make(chan error)
	go func() {
		for {
			msg, err := cc.Recv()
			if errors.Is(err, io.EOF) {
				close(done2)
				return
			}
			if err != nil {
				done2 <- err
				return
			}
			if err := stream.Send(msg); err != nil {
				done2 <- err
				return
			}
		}
	}()

	if err := <-done; err != nil {
		return err
	}
	if err := cc.CloseSend(); err != nil {
		return err
	}
	if err := <-done2; err != nil {
		return err
	}

	return nil
}
