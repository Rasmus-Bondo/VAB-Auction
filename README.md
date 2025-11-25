# VAB-Auction-House: Readme
## Steps to run the program:
1. Clone the repo
2. Open 3 terminal windows
3. In the first window move to the server folder then copy and paste this:
```bash
cd server
go run server.go --id 0 --port :5050 --peer :5051
```
4. In the second window move to the server folder then copy and paste this: 
```bash
cd server
go run server.go --id 1 --port :5051 --peer :5050
```
5. In the third window move to client folder then copy and paste this:
```bash
cd ..
cd client
go run client.go
```
6. (optional) works with multiple clients
## **Important Warning**
It is important that the two server commands are run within 3 seconds of each other, otherwise replication will not function correctly. 
