package main

import (
	pb "VAB-Auction/grpc"
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var client pb.AuctionServiceClient

func main() {
	file, err := os.OpenFile("logs.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("[Client] | Event=Error | Message= %v", err)
	}
	log.SetOutput(file)

	conn, err := connectWithFailover([]string{":5050", ":5051"})
	if err != nil {
		log.Fatalf("No servers available: %v", err)
	}

	client = pb.NewAuctionServiceClient(conn)

	log.Printf("[Client] | Event=Connect | Message=Connected to server")

	ctx := context.Background()
	idReply, err := client.Subscribe(ctx, &pb.Empty{})
	for err != nil {
		idReply, err = client.Subscribe(ctx, &pb.Empty{})
	}
	clientID := idReply.Id

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Connected to VAB Auction with ID == %d == \n", clientID)
	fmt.Println("Type 'bid <amount>' to bid on the current auction, 'result' to see the result of the latest auction or 'start' to start an auction")
	for {
		var text string
		text, _ = reader.ReadString('\n')
		text = strings.ToLower(text)
		text = strings.TrimSpace(text)
		parts := strings.Split(text, " ")
		if len(parts) == 1 {
			if parts[0] == "result" {
				Result(client)
			} else if parts[0] == "start" {
				StartAuction(client)
			} else {
				fmt.Println("Input not recognized please try again")
				fmt.Println("Type 'bid <amount>' to bid on the current auction, 'result' to see the result of the latest auction or 'start' to start an auction")
			}
		} else if len(parts) == 2 && parts[0] == "bid" {
			clientBid, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Println("Bid must be an integer")
				continue
			} else {
				Bid(client, clientID, clientBid)
			}
		} else {
			fmt.Println("Input not recognized please try again")
			fmt.Println("Type 'bid <amount>' to bid on the current auction, 'result' to see the result of the latest auction or 'start' to start an auction")
		}

	}
}

func connectWithFailover(addrs []string) (*grpc.ClientConn, error) {
	var lastErr error

	for _, addr := range addrs {
		conn, err := grpc.Dial(
			addr,
			grpc.WithInsecure(),
			grpc.WithBlock(),
			grpc.WithTimeout(2*time.Second),
		)

		if err == nil {
			fmt.Println("Connected to", addr)
			return conn, nil
		}

		fmt.Println("Failed to connect to", addr, "=>", err)
		lastErr = err
	}

	return nil, fmt.Errorf("all connection attempts failed: %w", lastErr)
}

func Bid(client pb.AuctionServiceClient, clientID int32, bid int) {
	fmt.Println("Bid sent")
	ctx := context.Background()
	ack, err := client.Bid(ctx, &pb.BidMessage{Amount: int32(bid), Id: clientID})

	if ack != nil && err == nil {
		if ack.Ack == "success" {
			fmt.Println("Bid accepted")
		} else {
			fmt.Println("Bid not accepted:", ack.Ack)
		}
	} else if err != nil {
		fmt.Println("There was an error connecting to the server:", err)
		connectToFollower()
	}

}

func Result(client pb.AuctionServiceClient) {
	ctx := context.Background()
	msg, err := client.Result(ctx, &pb.Empty{})

	if msg != nil && err == nil {
		fmt.Println(msg.Outcome)
	} else if err != nil {
		fmt.Println("There was an error connecting to the server:", err)
		connectToFollower()
	}
}

func StartAuction(client pb.AuctionServiceClient) {
	ctx := context.Background()
	ack, err := client.StartAuction(ctx, &pb.Empty{})
	if ack != nil && err == nil {
		fmt.Println(ack)
	} else if err != nil {
		fmt.Println("there was an error connecting to the server:", err)
		connectToFollower()
	}
}

func connectToFollower() {
	fmt.Println("Checking heartbeat of Leader server")
	ctx := context.Background()
	ack, err := client.HeartBeat(ctx, &pb.Empty{})
	if err == nil && ack != nil {
		fmt.Println("Connection to Leader server restored, please try again")
		return
	}
	fmt.Println("Heartbeat failed:", err)
	fmt.Println("Trying to connect to follower server")
	sc := status.Code(err)
	if sc != codes.Unavailable && sc != codes.DeadlineExceeded {
		log.Println(err)
		return
	}
	time.Sleep(1 * time.Second)
	conn, dialErr := grpc.Dial("localhost:5051", grpc.WithInsecure())
	if dialErr != nil {
		log.Fatalf("[Client] Error connecting to replica")
	}

	followerClient := pb.NewAuctionServiceClient(conn)
	isLeader, err := followerClient.IsLeader(context.Background(), &pb.Empty{})
	if err == nil && isLeader.GetIsLeader() {
		client = followerClient
		fmt.Println("Successfully connected to the follower server, please try again")
	}

}
