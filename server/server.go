package main

import (
	pb "VAB-Auction/grpc"
	"context"
	"flag"
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

	id        int32
	peerAddrs []string
	peers     map[int]pb.AuctionServiceClient

	bid           int32
	bidderID      int32
	isOver        bool // flipped to false når man subscriber (hvis den altså er true) -- starter med at være true
	remainingTime int32

	// replication variables
	isLeader       bool
	replicationLog *log.Logger
	alone          bool
}

func (s *AuctionServer) connectToPeers(peerIDs []int) {
	for i, addr := range s.peerAddrs {
		go func(peerID int, a string) {
			for {
				conn, err := grpc.Dial(a, grpc.WithInsecure())
				if err == nil {
					client := pb.NewAuctionServiceClient(conn)
					s.mu.Lock()
					s.peers[peerID] = client
					s.mu.Unlock()

					fmt.Printf("Server %d connected to peer %s (ID=%d)\n", s.id, a, peerID)
					//log.Printf("[Server %d | STATE=%s | T=%d] Connected to peer %s", s.id, s.state, s.timestamp, a)
					return
				}
				time.Sleep(time.Second)
			}
		}(peerIDs[i], addr)
	}
}

func (s *AuctionServer) waitForPeers(total int) {
	for {
		s.mu.Lock()
		if len(s.peers) == total {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		time.Sleep(1000 * time.Millisecond)
	}
}

func (s *AuctionServer) CheckOtherServer() {
	for !s.alone {
		time.Sleep(5 * time.Second)
		if !s.isLeader {
			ctx := context.Background()
			ack, err := s.peers[0].HeartBeat(ctx, &pb.Empty{})
			if err != nil || ack == nil {
				fmt.Println("Leader Server is unresponsive taking over as leader", s.id)
				s.isLeader = true
				s.alone = true
				break
			} else {
				fmt.Println("Follower checking Leader: ", ack.Ack, s.id)
			}
		} else {
			ctx := context.Background()
			ack, err := s.peers[1].HeartBeat(ctx, &pb.Empty{})
			if err != nil || ack == nil {
				fmt.Println("Follower Server is unresponsive", s.id)
				s.isLeader = false
				s.alone = true
				break
			} else {
				fmt.Println("Leader checking Follower: ", ack.Ack, s.id)
			}
		}
	}
}

func (s *AuctionServer) HeartBeat(ctx context.Context, Empty *pb.Empty) (*pb.Ack, error) {
	ack := &pb.Ack{
		Ack: "Alive",
	}
	return ack, nil
}

//func (s *AuctionServer) SendLog(ctx context.Context, logEntry *pb.LogEntry) (*pb.Ack, error) {}

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
	// Define CLI flags
	serverID := flag.Int("id", 0, "Server ID number (0 or 1)")
	port := flag.String("port", ":5050", "Port to listen on, e.g. :5050")
	peer := flag.String("peer", "", "Peer port, e.g. :5051")

	flag.Parse()

	if *peer == "" {
		log.Fatalf("You must specify --peer")
	}

	// Logging setup
	file, err := os.OpenFile("logs.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	log.SetOutput(file)

	// Start ONE server based on flags
	spawnServer(*serverID, *port, []string{*peer}, []int{1 - *serverID})

	// Keep process running
	select {}
}

func spawnServer(server_id int, port string, peerPorts []string, peerIDs []int) {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	var isleader bool

	if server_id == 0 {
		isleader = true
	} else {
		isleader = false
	}

	s := &AuctionServer{
		bidders:       make(map[int32]int32),
		nextID:        0,
		bid:           0,
		bidderID:      -1,
		isOver:        true,
		remainingTime: 0,
		id:            int32(server_id),
		timestamp:     1,
		isLeader:      isleader,
		peerAddrs:     peerPorts,
		peers:         make(map[int]pb.AuctionServiceClient),
		alone:         false,
	}

	grpcServer := grpc.NewServer()
	pb.RegisterAuctionServiceServer(grpcServer, s)

	go func() {
		fmt.Println("Server", server_id, "listening on", port)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	go s.connectToPeers(peerIDs)
	s.waitForPeers(len(peerIDs))
	log.Printf("[Server %d] All peers connected", server_id)
	go s.CheckOtherServer()
}
