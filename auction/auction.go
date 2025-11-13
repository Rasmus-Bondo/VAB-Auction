package auction

import (
	pb "Mandatory-Assignment-5/proto"
	"log"
	"net"
	"sync"

	"google.golang.org/grpc"
)

type Auction struct {
	pb.UnimplementedAuctionServer

	mu         sync.Mutex
	address    string
	highestBid pb.Bid
}

func (a *Auction) StartServer() {
	list, err := net.Listen("tcp", a.address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	server := grpc.NewServer()
	pb.RegisterAuctionServer(server, a)

	log.Printf("Auction server listening on %s", a.address)

	if err := server.Serve(list); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func Main() {
	auction := &Auction{
		address:    "localhost:50051",
		highestBid: pb.Bid{BidAmount: 0},
	}
	auction.StartServer()
}
