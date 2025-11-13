package auction

import (
	pb "Mandatory-Assignment-5/proto"
	"context"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
)

type Auction struct {
	pb.UnimplementedAuctionServer

	mu            sync.Mutex
	address       string
	auctionIsOver bool
	highestBid    *pb.Bid
}

func Main() {
	auction := &Auction{
		address:       "localhost:50051",
		highestBid:    &pb.Bid{BidAmount: 0},
		auctionIsOver: false,
	}

	go auction.StartServer()

	time.Sleep(2 * time.Minute)

	auction.mu.Lock()
	auction.auctionIsOver = true
	auction.mu.Unlock()

	log.Println("Auction has ended with:", auction.highestBid.GetId(), "who won @", auction.highestBid.GetBidAmount(), "DKK")
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

// --- RPC CALLS ---
func (a *Auction) TryBid(ctx context.Context, bid *pb.Bid) (*pb.Ack, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Sets highest bid to the TryBid received by nodes
	if a.auctionIsOver {
		return &pb.Ack{Type: pb.Ack_EXCEPTION}, nil
	} else if a.highestBid.GetBidAmount() < bid.GetBidAmount() {
		a.highestBid = bid
		return &pb.Ack{Type: pb.Ack_SUCCESS}, nil
	}

	return &pb.Ack{Type: pb.Ack_FAIL}, nil
}

func (a *Auction) TryResult(ctx context.Context, empty *pb.Empty) (*pb.Outcome, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.auctionIsOver {
		return &pb.Outcome{
			Type: &pb.Outcome_Result{
				Result: &pb.Result{
					HighestBid: a.highestBid.GetBidAmount(), Id: a.highestBid.GetId(),
				},
			},
		}, nil
	} else {
		return &pb.Outcome{Type: &pb.Outcome_HighestBid{HighestBid: a.highestBid.GetBidAmount()}}, nil
	}
}
