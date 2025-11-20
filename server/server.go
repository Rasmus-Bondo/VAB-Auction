package main

import (
	pb "VAB-Auction/grpc"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
)

/*
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

	bid           int32
	bidderID      int32
	isOver        bool // flipped to false når man subscriber (hvis den altså er true) -- starter med at være true
	remainingTime int32

	// replication variables
	isLeader       bool
	replicationLog *log.Logger
}

func newServer() *AuctionServer {
	return &AuctionServer{
		bidders:       make(map[int32]int32),
		timestamp:     1,
		nextID:        0,
		bid:           0,
		bidderID:      -1,
		isOver:        true,
		remainingTime: 0,
	}
}
func (s *AuctionServer) SendLog(ctx context.Context, logEntry *pb.LogEntry) (*pb.Ack, error) {

}

/*
Method:  bid
Inputs:  amount (an int)
Outputs: ack
Comment: given a bid, returns an outcome among {fail, success or exception}
*/
func (s *AuctionServer) Bid(ctx context.Context, bidMessage *pb.BidMessage) (*pb.Ack, error) {
	var ack *pb.Ack
	ack = &pb.Ack{
		Ack: "",
	}

	if s.isOver {
		fmt.Println("Bid failed. No auction running.")
		ack.Ack = "Fail: No auction running."
		return ack, nil
	}

	clientID := bidMessage.Id
	_, exists := s.bidders[clientID]

	if !exists {
		fmt.Println("Client making bid doesnt exist")
		ack.Ack = "Fail: Bidder not subscribed"
		return ack, nil
	}

	s.mu.Lock()
	newClientBid := bidMessage.Amount
	currentHighestBid := s.bid

	// if new bid is not higher than previous bid, send back ack saying bid has to be higher. Else update
	if newClientBid > currentHighestBid && !s.isOver {

		s.bidders[clientID] = newClientBid
		s.bid = newClientBid
		s.bidderID = clientID
		s.mu.Unlock()

		s.setTime(20)
		p := fmt.Sprintf("Client bid sucessful. Bidded : %d", newClientBid)
		fmt.Println(p)
		ack.Ack = "Success: Bid accepted"

	} else {
		s.mu.Unlock()
		fmt.Println("Bid failed. Current bid not higher than highest bid.")
		ack.Ack = "Fail: Bid not higher than current highest bid."
	}

	return ack, nil
}

func (s *AuctionServer) countDown() {

	fmt.Println("\n")
	fmt.Println("==========================")
	fmt.Println("Auction Started.")
	for s.remainingTime > 0 {
		fmt.Println(s.remainingTime, "seconds left.")
		time.Sleep(time.Second)
		s.mu.Lock()
		s.decreaseTime(1)
		s.mu.Unlock()
	}
	fmt.Println("Auction over")
	fmt.Println("==========================")
	fmt.Println("\n")
	s.mu.Lock()
	s.isOver = true
	s.mu.Unlock()
}

/*
Method:  result
Inputs:  void
Outputs: outcome
Comment:  if the auction is over, it returns the result, else highest bid.
*/
func (s *AuctionServer) Result(ctx context.Context, _ *pb.Empty) (*pb.ResultReply, error) {
	var msg string

	if s.isOver {
		msg = fmt.Sprintf("The winner is %d with a bid of %d", s.bidderID, s.bid)
	} else {
		msg = fmt.Sprintf("The current highest bidder is %d with a bid of %d", s.bidderID, s.bid)
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
	var msg string

	if s.isOver {

		s.bid = 0
		s.bidderID = -1
		s.isOver = false

		s.setTime(20)
		s.mu.Unlock()

		go s.countDown()

		msg = "New Action started"
		fmt.Println("New auction started")

	} else {
		s.mu.Unlock()
		msg = "There is currently an active auction. Try again later"
		fmt.Println("There is currently an active auction")
	}

	ack := &pb.Ack{
		Ack: msg,
	}
	return ack, nil
}

func (s *AuctionServer) setTime(time int32) {
	s.remainingTime = time
}

func (s *AuctionServer) decreaseTime(time int32) {
	s.remainingTime -= time
}

func main() {
	lis, err := net.Listen("tcp", ":5060")
	file, err := os.OpenFile("logs.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("[Lamport=0][Server] | Event=Error | Message= %v", err)
	}
	log.SetOutput(file)

	grpcServer := grpc.NewServer()
	pb.RegisterAuctionServiceServer(grpcServer, newServer())

	fmt.Println("Auction Server running on port 5060...")
	log.Printf("[Lamport=1][Server] | Event=Listening | Message=Server listening at %v", lis.Addr())
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("[Lamport=1][Server] | Event=Error | Message= %v", err)
	}
	log.Printf("[Lamport=1][Server] | Event=Shutdown | Message=Server shutting down")
}
