/**
- Skal kunne joine, publish og leave som de vil
- Hver klient viser og logger beskeder + timestamps.
*/

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

	"google.golang.org/grpc"
)

func main() {
	timestamp := 0
	file, err := os.OpenFile("logs.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("[Lamport=%d][Client] | Event=Error | Message= %v", timestamp, err)
	}
	log.SetOutput(file)

	conn, err := grpc.Dial("localhost:5060", grpc.WithInsecure())
	if err != nil {
		timestamp++
		log.Fatalf("[Lamport=%d][Client] | Event=Error | Message= %v", timestamp, err)
	}
	defer conn.Close()

	client := pb.NewAuctionServiceClient(conn)
	timestamp++
	log.Printf("[Lamport=%d][Client] | Event=Connect | Message=Connected to server", timestamp)

	ctx := context.Background()
	idReply, err := client.Subscribe(ctx, &pb.Empty{})
	for err != nil {
		idReply, err = client.Subscribe(ctx, &pb.Empty{})
	}
	clientID := idReply.Id

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Connected to VAB Auction! — start bidding or seeing results!")
	fmt.Println("Type 'bid <amount>' to bid on the current auction, 'result' to see the result of the latest auction or 'start' to start an auction")
	for {
		var text string
		text, _ = reader.ReadString('\n')
		text = strings.Replace(text, "\n", "", -1)
		parts := strings.Split(text, " ")
		if len(parts) == 1 {
			if parts[0] == "result" {
				Result(client)
			} else if parts[0] == "start" {
				StartAuction(client)
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
			fmt.Println("Type 'bid <amount>' to bid on the current auction or 'result' to see the result of the latest auction")
		}

	}

}

func Bid(client pb.AuctionServiceClient, clientID int32, bid int) {
	ctx := context.Background()
	ack, err := client.Bid(ctx, &pb.BidMessage{Amount: int32(bid), Id: clientID})
	if ack != nil && err == nil {
		fmt.Println("Bid sent")
	} else {
		fmt.Println("there was an error")
		if ack == nil {
			fmt.Println("Ack is nil")
		}
	}
}

func Result(client pb.AuctionServiceClient) {
	ctx := context.Background()
	msg, err := client.Result(ctx, &pb.Empty{})
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(msg.Outcome)
	}
}

func StartAuction(client pb.AuctionServiceClient) {
	ctx := context.Background()
	ack, err := client.StartAuction(ctx, &pb.Empty{})
	if ack != nil && err == nil {
		fmt.Println("Auction started")
	} else {
		fmt.Println("there was an error")
		if ack == nil {
		}
	}
}
