package server

import (
	"context"
	"log"
	"net/url"
	"sync"
	"time"

	pb "github.com/livegrep/livegrep/src/proto/go_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
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
	Id         string
	Addr       string
	I          *I
	Codesearch pb.CodeSearchClient
	Up         *Availability
}

func NewBackend(id string, addr string) (*Backend, error) {
	client, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	bk := &Backend{
		Id:         id,
		Addr:       addr,
		I:          &I{Name: id},
		Codesearch: pb.NewCodeSearchClient(client),
		Up:         &Availability{},
	}
	return bk, nil
}

func (bk *Backend) Start() {
	if bk.I == nil {
		bk.I = &I{Name: bk.Id}
	}
	go bk.poll()
}

// We continuosly poll for QuickInfo every second
// We make requests for detailed info when
//  1. the indexTime we have is different than what quickInfo returns
// This occurs on startup and on codesearch backend reloads
func (bk *Backend) poll() {
	for {
		quickInfo, e := bk.Codesearch.QuickInfo(context.Background(), &pb.Empty{}, grpc.FailFast(true))
		bk.Up.Lock()
		// If the backend index hash changed out on us, get the detailed info
		if e == nil {
			newTime := time.Unix(quickInfo.IndexTime, 0)
			if !bk.Up.IsUp || bk.I.IndexTime.Before(newTime) {
				bk.getInfo()
			}
			bk.Up.IsUp = true
			bk.Up.DownSince = time.Time{}
			bk.Up.DownCode = 0
		} else {
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
		log.Printf("refresh %s: %v", bk.Id, e)
	}
}

func (bk *Backend) refresh(info *pb.ServerInfo) {
	bk.I.Lock()
	defer bk.I.Unlock()

	if info.Name != "" {
		bk.I.Name = info.Name
	}

	bk.I.IndexTime = time.Unix(info.IndexTime, 0)
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
