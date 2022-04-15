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

type Backend struct {
	Id               string
	Addr             string
	I                *I
	Codesearch       pb.CodeSearchClient
	Available        bool
	UnavailableSince time.Time
	UnavailableCode  codes.Code
	InitialTreesSet  bool
	sync.Mutex
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
	}
	return bk, nil
}

func (bk *Backend) Start() {
	if bk.I == nil {
		bk.I = &I{Name: bk.Id}
	}
	go bk.poll()
}

// Use quickPoll to see if we should call Info!
func (bk *Backend) poll() {
	for {
		quickInfo, e := bk.Codesearch.QuickInfo(context.Background(), &pb.Empty{}, grpc.FailFast(true))
		bk.Lock()
		// If the backend index hash changed out on us, get the detailed info
		if e == nil {
			log.Printf("available")
			newTime := time.Unix(quickInfo.IndexTime, 0)
			if !bk.Available || bk.I.IndexTime.Before(newTime) {
				bk.getInfo()
			}
			bk.Available = true
		} else {
			log.Printf("unavailable!!")
			if bk.Available || bk.UnavailableSince.IsZero() {
				log.Print("setting unavailable info")
				bk.Available = false
				bk.UnavailableSince = time.Now()
				bk.UnavailableCode = grpc.Code(e)
			}
		}
		bk.Unlock()
		time.Sleep(1 * time.Second)
	}
}

func (bk *Backend) getInfo() {
	log.Printf("inside of getInfo")
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
	log.Printf("in refresh")

	// Only refresh the trees if the index has changed
	if !bk.InitialTreesSet {
		log.Print("initial trees are starting to be set")
		bk.InitialTreesSet = true
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
	log.Printf("here")
}
