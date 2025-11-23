package main

import (
	pb "VAB-Auction/grpc"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
)

type AuctionServer struct {
	pb.UnimplementedAuctionServiceServer
	mu        sync.Mutex
	bidders   map[int32]int32 // maps client id's to their last bid on the ongoing auction
	nextID    int32
	timestamp int32

	serverId  int32
	peerAddrs []string
	peers     map[int]pb.AuctionServiceClient

	bidAmount     int32
	bidderID      int32
	isOver        bool // flipped to false når man subscriber (hvis den altså er true) -- starter med at være true
	remainingTime int32

	// replication variables
	isLeader       bool
	alone          bool
	logIndex       int32 // increments every time a change is made to the replication log
	eventLog       *log.Logger
	replicationLog *log.Logger
}

func (s *AuctionServer) SendLog(ctx context.Context, logEntry *pb.LogEntry) (*pb.Ack, error) {
	// sende en command og et index
	if logEntry.Command == "connect" {
		s.Subscribe(ctx, &pb.Empty{})
	} else if logEntry.Command == "start" {
		s.StartAuction(ctx, &pb.Empty{})
		s.replicationLog.Printf("[server=%d][ts=%d][event=received_start][from=leader][logIndex=%d]", s.serverId, s.timestamp, logEntry.Index)
		return &pb.Ack{Ack: "Ack"}, nil
	} else {
		msg := strings.Split(logEntry.Command, " ")

		amount, _ := strconv.Atoi(msg[2])
		id, _ := strconv.Atoi(msg[1])

		s.replicationLog.Printf("[server=%d][ts=%d][event=received_bid][from=leader][client=%d][amount=%d][logIndex=%d]", s.serverId, s.timestamp, id, amount, logEntry.Index)

		bidMsg := &pb.BidMessage{
			Amount: int32(amount),
			Id:     int32(id),
		}
		s.Bid(ctx, bidMsg)

	}
	return &pb.Ack{Ack: "error"}, nil
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

					fmt.Printf("Server %d connected to peer %s (ID=%d)\n", s.serverId, a, peerID)
					s.eventLog.Printf("[server=%d][ts=%d][event=peer_connected][peer=%d][addr=%s]", s.serverId, s.timestamp, peerID, a)
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
				s.eventLog.Printf("[server=%d][ts=%d][event=peer_unresponsive][peer=%d]", s.serverId, s.timestamp, 0)
				fmt.Println("Leader Server is unresponsive taking over as leader", s.serverId)
				s.isLeader = true
				s.alone = true
				break
			} else {
				fmt.Println("Follower checking Leader: ", ack.Ack, s.serverId)
			}
		} else {
			ctx := context.Background()
			ack, err := s.peers[1].HeartBeat(ctx, &pb.Empty{})
			if err != nil || ack == nil {
				s.eventLog.Printf("[server=%d][ts=%d][event=peer_unresponsive][peer=%d]", s.serverId, s.timestamp, 0)
				fmt.Println("Follower Server is unresponsive", s.serverId)
				s.isLeader = false
				s.alone = true
				break
			} else {
				fmt.Println("Leader checking Follower: ", ack.Ack, s.serverId)
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
	currentHighestBid := s.bidAmount

	// if new bid is not higher than previous bid, send back ack saying bid has to be higher. Else update
	if newClientBid > currentHighestBid && !s.isOver {

		s.bidders[clientID] = newClientBid
		s.eventLog.Printf("[server=%d][ts=%d][event=bid_accepted][client=%d][amount=%d]", s.serverId, s.timestamp, clientID, newClientBid)
		s.bidAmount = newClientBid
		s.bidderID = clientID

		s.setTime(20)
		p := fmt.Sprintf("Client bid sucessful. Bidded : %d", newClientBid)
		fmt.Println(p)
		ack.Ack = "Success: Bid accepted"

		if s.isLeader && !s.alone {
			logMsg := &pb.LogEntry{
				Index:   s.logIndex,
				Command: fmt.Sprintf("bid %d %d", bidMessage.Id, bidMessage.Amount),
			}
			s.logIndex++
			s.replicationLog.Printf("[server=%d][ts=%d][event=send_bid][toPeer=%d][client=%d][amount=%d][logIndex=%d]", s.serverId, s.timestamp, 1, bidMessage.Id, bidMessage.Amount, logMsg.Index)
			ctx := context.Background()
			ack, err := s.peers[1].SendLog(ctx, logMsg)
			if err == nil && ack != nil {
				s.eventLog.Printf("[server=%d][ts=%d][event=replication_ack][peer=%d][logIndex=%d]", s.serverId, s.timestamp, 1, logMsg.Index)
			} else {
				s.alone = true
				s.eventLog.Printf("[ID=%d], [Death of Peer] [timestamp: %d] [Peer: %d] [Reason: didnt respond to start command]", s.serverId, s.timestamp, 1)
			}
		}
		s.mu.Unlock()

	} else {
		s.eventLog.Printf("[server=%d][ts=%d][event=bid_rejected][client=%d][amount=%d]", s.serverId, s.timestamp, clientID, newClientBid)
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
	s.eventLog.Printf("[server=%d][ts=%d][event=auction_finished][winner=%d][amount=%d]", s.serverId, s.timestamp, s.bidderID, s.bidAmount)
	s.isOver = true
	s.mu.Unlock()
}

func (s *AuctionServer) Result(ctx context.Context, _ *pb.Empty) (*pb.ResultReply, error) {
	var msg string

	if s.isOver {
		msg = fmt.Sprintf("The winner is %d with a bid of %d", s.bidderID, s.bidAmount)
	} else {
		msg = fmt.Sprintf("The current highest bidder is %d with a bid of %d", s.bidderID, s.bidAmount)
	}

	reply := &pb.ResultReply{
		Outcome: msg,
	}
	return reply, nil
}

func (s *AuctionServer) Subscribe(ctx context.Context, in *pb.Empty) (*pb.IdReply, error) {
	s.mu.Lock()

	s.timestamp++
	s.bidders[s.nextID] = 0

	msg := &pb.IdReply{
		Id: s.nextID,
	}

	if s.isLeader && !s.alone {

		logMsg := &pb.LogEntry{
			Index:   s.logIndex,
			Command: "connect",
		}
		s.replicationLog.Printf("[server=%d][ts=%d][event=send_connect][toPeer=%d][client=%d][logIndex=%d]", s.serverId, s.timestamp, 1, s.nextID, logMsg.Index)
		s.logIndex++
		ctx := context.Background()
		ack, err := s.peers[1].SendLog(ctx, logMsg)
		if err == nil && ack != nil {
			s.eventLog.Printf("[server=%d][ts=%d][event=replication_ack][peer=%d][logIndex=%d]", s.serverId, s.timestamp, 1, logMsg.Index)
		} else {
			s.alone = true
			s.eventLog.Printf("[ID=%d], [Death of Peer] [timestamp: %d] [Peer: %d] [Reason: didnt respond to start command]", s.serverId, s.timestamp, 1)
		}
	}

	fmt.Printf("[%d] Client %d subscribed\n", s.timestamp, s.nextID)
	s.eventLog.Printf("[server=%d][ts=%d][event=client_subscribed][client=%d]", s.serverId, s.timestamp, s.nextID)

	s.nextID++
	s.mu.Unlock()
	return msg, nil
}

func (s *AuctionServer) StartAuction(ctx context.Context, in *pb.Empty) (*pb.Ack, error) {
	s.mu.Lock()
	var msg string

	if s.isOver {

		s.bidAmount = 0
		s.bidderID = -1
		s.isOver = false

		s.setTime(20)

		go s.countDown()

		msg = "New Action started"
		fmt.Println("New auction started")
		s.eventLog.Printf("[ID=%d][Auction_start] [timestamp: %d]", s.serverId, s.timestamp)

		if s.isLeader && !s.alone {
			logMsg := &pb.LogEntry{
				Index:   s.logIndex,
				Command: "start",
			}
			s.replicationLog.Printf("[server=%d][ts=%d][event=send_start][toPeer=%d][logIndex=%d]", s.serverId, s.timestamp, 1, s.logIndex)
			s.logIndex++
			ctx := context.Background()
			ack, err := s.peers[1].SendLog(ctx, logMsg)
			if err == nil && ack != nil {
				s.eventLog.Printf("[server=%d][ts=%d][event=start_sent_ack][peer=%d][logIndex=%d]", s.serverId, s.timestamp, 1, logMsg.Index)
			} else {
				s.alone = true
				s.eventLog.Printf("[ID=%d], [Death of Peer] [timestamp: %d] [Peer: %d] [Reason: didnt respond to start command]", s.serverId, s.timestamp, 1)
			}
		}
		s.mu.Unlock()

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
	// go run server.go --id 0 --port :5050 --peer :5051
	// go run server.go --id 1 --port :5051 --peer :5050
	serverID := flag.Int("id", 0, "Server ID number (0 or 1)")
	port := flag.String("port", ":5050", "Port to listen on, e.g. :5050")
	peer := flag.String("peer", "", "Peer port, e.g. :5051")

	flag.Parse()

	if *peer == "" {
		log.Fatalf("You must specify --peer")
	}

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
	var logPath string
	var eventLogPath string

	if server_id == 0 {
		isleader = true
		logPath = "leader.log"
		eventLogPath = fmt.Sprintf("leader_event_log.log")
	} else {
		isleader = false
		logPath = fmt.Sprintf("follower%d.log", server_id)
		eventLogPath = fmt.Sprintf("follower%d_event_log.log", server_id)
	}
	// Logging setup

	eventFile, _ := os.OpenFile(eventLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	replFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)

	eventLogger := log.New(eventFile, "", log.LstdFlags)
	replicationLogger := log.New(replFile, "REPLICA: ", log.LstdFlags)

	s := &AuctionServer{
		bidders:        make(map[int32]int32),
		nextID:         0,
		bidAmount:      0,
		bidderID:       -1,
		isOver:         true,
		remainingTime:  0,
		serverId:       int32(server_id),
		timestamp:      1,
		isLeader:       isleader,
		peerAddrs:      peerPorts,
		peers:          make(map[int]pb.AuctionServiceClient),
		alone:          false,
		logIndex:       0,
		eventLog:       eventLogger,
		replicationLog: replicationLogger,
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
