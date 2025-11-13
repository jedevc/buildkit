package gateway

import (
	api "github.com/containerd/containerd/api/services/content/v1"
	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/plugins/services/content/contentserver"
	"google.golang.org/grpc"
)

type Store struct {
	store content.Store
}

func (s *Store) Register(server *grpc.Server) {
	// XXX: filter content based on result of build
	service := contentserver.New(s.store)
	api.RegisterContentServer(server, service)
}
