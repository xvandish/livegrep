package server

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"sync"
	"time"

	pb "github.com/livegrep/livegrep/src/proto/go_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"

	"github.com/livegrep/livegrep/server/config"
)

type Tree struct {
	Name    string
	Version string
	Url     string
}

type I struct {
	Name  string
	Trees []Tree
	sync.Mutex
	IndexTime time.Time
}

type Availability struct {
	IsUp      bool
	DownSince time.Time
	DownCode  codes.Code
	sync.Mutex
}

type Backend struct {
	Id            string
	Addr          string
	I             *I
	Codesearch    pb.CodeSearchClient
	GrpcClient    *grpc.ClientConn
	Up            *Availability
	BackupBackend *Backend
	IsBackup      bool
}

// Should I.... have the backup backend be a seperate backend or a nested backend?
// I think I'm going to tailor it to us

// NewBackend can now be recursively called since BackupBackend can be nested...
// is this a terrible idea?
func NewBackend(be config.Backend) (*Backend, error) {
	opts := []grpc.DialOption{grpc.WithInsecure()}
	if be.MaxMessageSize == 0 {
		be.MaxMessageSize = 10 << 20 // default to 10MiB
	}

	// this reconnect continually retries a connection.
	// This is desirable in our case since we want to reconnect as soon as
	// possible after a disconnect, since we actually expect several disconnects
	// per hour that last up to a minute, and sometimes last up to an hour in error
	// scenarios. The default exponential delay can make it so we don't reconnect
	// to a previously disconnected index until much after is re-available
	shortFlatReconnect := grpc.ConnectParams{
		Backoff: backoff.Config{
			BaseDelay:  20 * time.Millisecond,
			Multiplier: 1,
			Jitter:     1,
			MaxDelay:   100 * time.Millisecond,
		},
		MinConnectTimeout: 100 * time.Millisecond,
	}
	opts = append(opts, grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(be.MaxMessageSize)), grpc.WithConnectParams(shortFlatReconnect))
	client, err := grpc.Dial(be.Addr, opts...)
	if err != nil {
		return nil, err
	}

	var backupBk *Backend
	if be.BackupBackend != nil && be.BackupBackend.Addr != "" {
		backupBk, err = NewBackend(*be.BackupBackend)
		backupBk.IsBackup = true
		if err != nil {
			return nil, err
		}
		// start the backup polling!
		backupBk.Start()
	}
	bk := &Backend{
		Id:            be.Id,
		Addr:          be.Addr,
		I:             &I{Name: be.Id},
		Codesearch:    pb.NewCodeSearchClient(client),
		GrpcClient:    client,
		Up:            &Availability{},
		BackupBackend: backupBk,
	}
	return bk, nil
}

func (bk *Backend) Start() {
	if bk.I == nil {
		bk.I = &I{Name: bk.Id}
	}
	go bk.poll()
}

type BackendStatus struct {
	GrpcStatus   connectivity.State `json:"grpc_status"`
	IndexAge     string             `json:"index_age"`
	IndexName    string             `json:"index_name"`
	BuGrpcStatus connectivity.State `json:"bu_grpc_status"`
	BuIndexAge   string             `json:"bu_index_age"`
	BuIndexName  string             `json:"bu_index_name"`
}

func (bk *Backend) getStatus() *BackendStatus {
	// given a backend, want to print it, ands its backups status
	s := bk.GrpcClient.GetState()
	age := ""
	if bk.grpcReadyOrIdle() {
		age = time.Since(bk.I.IndexTime).Round(time.Minute).String()
	}

	// now get backup info
	if bk.BackupBackend == nil {
		return &BackendStatus{
			GrpcStatus: s,
			IndexAge:   age,
			IndexName:  bk.I.Name,
		}
	}

	bus := bk.BackupBackend.GrpcClient.GetState()
	buAge := ""
	if bk.BackupBackend.grpcReadyOrIdle() {
		buAge = time.Since(bk.BackupBackend.I.IndexTime).Round(time.Minute).String()
	}
	return &BackendStatus{
		GrpcStatus:   s,
		IndexAge:     age,
		IndexName:    bk.I.Name,
		BuGrpcStatus: bus,
		BuIndexAge:   buAge,
		BuIndexName:  bk.BackupBackend.Id,
	}

}

func (bk *Backend) grpcReadyOrIdle() bool {
	s := bk.GrpcClient.GetState()
	// https://cs.github.com/grpc/grpc-go/blob/c672451950653990bd607c8ba08733d6f36d85fc/connectivity/connectivity.go#L51
	// 0 == Idle 2 == Ready
	return s == connectivity.Ready || s == connectivity.Idle
}

// We continuosly poll for QuickInfo every second
// We make requests for detailed info when
//  1. the indexTime we have is different than what quickInfo returns
// This occurs on startup and on codesearch backend reloads
func (bk *Backend) poll() {
	for {
		quickInfo, e := bk.Codesearch.QuickInfo(context.Background(), &pb.Empty{}, grpc.WaitForReady(false))
		bk.Up.Lock()
		// If the backend index hash changed out on us, get the detailed info
		if e == nil {
			newTime := time.Unix(quickInfo.IndexTime, 0)
			if !bk.Up.IsUp || bk.I.IndexTime.Before(newTime) {
				fmt.Printf("quickPoll for be=%s -- fetching getInfo(). was_up=%t new_index_available=%t\n", bk.Id, bk.Up.IsUp, bk.I.IndexTime.Before(newTime))
				bk.getInfo()
			}
			bk.Up.IsUp = true
			bk.Up.DownSince = time.Time{}
			bk.Up.DownCode = 0
		} else {
			if os.Getenv("LOG_BK_QUICKPOLL_FAIL") == "true" {
				fmt.Printf("quickPoll for be=%s -- ERROR: %s\n", bk.Id, grpc.Code(e))
			}
			if bk.Up.IsUp || bk.Up.DownSince.IsZero() {
				bk.Up.IsUp = false
				bk.Up.DownSince = time.Now()
				bk.Up.DownCode = grpc.Code(e)
			}
		}
		bk.Up.Unlock()
		time.Sleep(1 * time.Second)
	}
}

func (bk *Backend) getInfo() {
	info, e := bk.Codesearch.Info(context.Background(), &pb.InfoRequest{}, grpc.FailFast(true))

	if e == nil {
		bk.refresh(info)
	} else {
		log.Printf("getInfo %s: %v", bk.Id, e)
	}
}

func (bk *Backend) refresh(info *pb.ServerInfo) {
	bk.I.Lock()
	defer bk.I.Unlock()

	if info.Name != "" {
		bk.I.Name = info.Name
	}

	newIndexTime := time.Unix(info.IndexTime, 0)
	bk.I.IndexTime = newIndexTime

	if len(info.Trees) > 0 {
		bk.I.Trees = nil
		for _, r := range info.Trees {
			pattern := r.Metadata.UrlPattern
			if v := r.Metadata.Github; v != "" {
				value := v
				base := ""
				_, err := url.ParseRequestURI(value)
				if err != nil {
					base = "https://github.com/" + value
				} else {
					base = value
				}
				pattern = base + "/blob/{version}/{path}#L{lno}"
			}
			bk.I.Trees = append(bk.I.Trees,
				Tree{r.Name, r.Version, pattern})
		}
	}
}
