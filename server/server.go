package main

import (
	pb "VAB-Auction/grpc"
	"context"
	"fmt"
	"log"
	"sync"
)

/**
Auction starter ikke før 1 person subscriber
Der er nogen der skal starte auc ved at kalde startAuc
efter 10 sekunder uden bids slutter en auc

auc kan kun starte igen hvis en ny subscriber joiner og auctionen er afsluttet

*/

type AuctionServer struct {
	pb.UnimplementedAuctionServiceServer
	mu        sync.Mutex
	bidders   map[int32]int32 // maps client id's to their last bid on the ongoing auction
	nextID    int32
	timestamp int32
	isLeader  bool

	auction struct {
		bid      int32
		bidderID int32
		isOver   bool // flipped to false når man subscriber (hvis den altså er true) -- starter med at være true
	}
}

func newServer() *AuctionServer {
	return &AuctionServer{
		bidders:   make(map[int32]int32),
		timestamp: 1,
		nextID:    0,
	}
}

/*
Method:  bid
Inputs:  amount (an int)
Outputs: ack
Comment: given a bid, returns an outcome among {fail, success or exception}
*/
func (s *AuctionServer) Bid(ctx context.Context, bidMessage *pb.BidMessage) (*pb.Ack, error) {
	clientID := bidMessage.Id
	_, exists := s.bidders[clientID]
	if !exists {
		ack := &pb.Ack{
			Ack: "Bidder not subscribed",
		}
		return ack, nil
	}

	newClientBid := bidMessage.Amount
	currClientBid := s.bidders[clientID]
	currentHighestBid := s.auction.bid

	s.mu.Lock()
	defer s.mu.Unlock()
	// if new bid is not higher than previous bid, send back ack saying bid has to be higher. Else update
	if newClientBid > (currentHighestBid & currClientBid) {
		s.bidders[clientID] = newClientBid
		s.auction.bid = newClientBid
		s.auction.bidderID = clientID

		ack := &pb.Ack{
			Ack: "Bid was Accepted",
		}
		return ack, nil
	} else {
		ack := &pb.Ack{
			Ack: "Bid was Denied",
		}
		return ack, nil
	}
}

/*
Start timer på 10 sec
hvis vi ikk modtager en bid før de 10 sekunder er overstået, slut auc
*/
func countDown() {

}

/*
Method:  result
Inputs:  void
Outputs: outcome
Comment:  if the auction is over, it returns the result, else highest bid.
*/
func (s *AuctionServer) Result(ctx context.Context, _ *pb.Empty) (*pb.ResultReply, error) {
	var msg string

	if s.auction.isOver {
		msg = fmt.Sprintf("The winner is %s with a bid of %d", s.auction.bidderID, s.auction.bid)
	} else {
		msg = fmt.Sprintf("The current highest bidder is %s with a bid of %d", s.auction.bidderID, s.auction.bid)
	}

	reply := &pb.ResultReply{
		Outcome: msg,
	}
	return reply, nil
}

/*
Subscribe a client to the service
1. give it an id
2. make change to local data
3. send that change of the local data to the Followers/slaves when done (Write it to changelog and send message to followers abt the
change in the log.... )
*/
func (s *AuctionServer) Subscribe(ctx context.Context, in *pb.Empty) (*pb.IdReply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.timestamp++
	s.bidders[s.nextID] = 0

	msg := &pb.IdReply{
		Id: s.nextID,
	}

	fmt.Printf("[%d] Client %d subscribed\n", s.timestamp, s.nextID)
	log.Printf("[%d] Client %d subscribed\n", s.timestamp, s.nextID)

	s.nextID++
	return msg, nil
}

func (s *AuctionServer) StartAuction(ctx context.Context, in *pb.Empty) (*pb.Ack, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var msg string

	if s.auction.isOver {
		s.auction.bid = 0
		s.auction.bidderID = -1
		s.auction.isOver = false

		msg = "New Action started"
	} else {
		msg = "There is currently an active auction. Try again later"
	}

	ack := &pb.Ack{
		Ack: msg,
	}
	return ack, nil
}
