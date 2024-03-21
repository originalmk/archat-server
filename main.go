package main

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

type ServerContext struct {
	peersList     []Peer
	peersListLock sync.RWMutex
}

type Peer struct {
	id   int
	conn net.Conn
}

type PeerInfo struct {
	ID   int    `json:"id"`
	Addr string `json:"addr"`
}

type EchoRequest struct {
	EchoByte byte `json:"echoByte"`
}

type EchoResponse struct {
	EchoByte byte `json:"echoByte"`
}

type ListPeersRequest struct {
}

type ListPeersResponse struct {
	PeersInfo []PeerInfo `json:"peers"`
}

const (
	echoRID      = 1
	listPeersRID = 2
)

func peerSliceIndexOf(s []Peer, id int) int {
	i := 0
	var p Peer
	for i, p = range s {
		if p.id == id {
			break
		}
	}
	return i
}

func peerSliceRemove(s *[]Peer, i int) {
	(*s)[i] = (*s)[len(*s)-1]
	*s = (*s)[:len(*s)-1]
}

func handleDisconnection(srvCtx *ServerContext, id int) {
	srvCtx.peersListLock.Lock()
	p := srvCtx.peersList[peerSliceIndexOf(srvCtx.peersList, id)]
	log.Printf("[Server] %s disconnected\n", p.conn.RemoteAddr())
	peerSliceRemove(&srvCtx.peersList, peerSliceIndexOf(srvCtx.peersList, id))
	srvCtx.peersListLock.Unlock()
}

func handlePeer(srvCtx *ServerContext, p Peer) {
	br := bufio.NewReader(p.conn)
	bw := bufio.NewWriter(p.conn)

	for {
		reqBytes, err := br.ReadBytes('\n')

		if err == io.EOF {
			handleDisconnection(srvCtx, p.id)
			break
		} else if err != nil {
			log.Println(err)
			break
		}

		if len(reqBytes) <= 1 {
			log.Println("got request without id")
			break
		}

		reqBytes = reqBytes[:len(reqBytes)-1]
		operationCode := reqBytes[0]
		reqJsonBytes := reqBytes[1:]
		var resBytes []byte

		if operationCode == echoRID {
			resBytes, err = handleEcho(srvCtx, reqJsonBytes)
		} else if operationCode == listPeersRID {
			resBytes, err = handleListPeers(srvCtx, reqJsonBytes)
		}

		if err != nil {
			log.Println(err)
			continue
		}

		resBytes = append(resBytes, '\n')
		_, err = bw.Write(resBytes)

		if err != nil {
			log.Println(err)
			continue
		}

		err = bw.Flush()

		if err != nil {
			log.Println(err)
		}
	}
}

func handleEcho(_ *ServerContext, reqBytes []byte) (resBytes []byte, err error) {
	var echoReq EchoRequest
	err = json.Unmarshal(reqBytes, &echoReq)

	if err != nil {
		return nil, err
	}

	echoRes := EchoResponse(echoReq)
	resBytes, err = json.Marshal(echoRes)

	if err != nil {
		return nil, err
	}

	return resBytes, nil
}

func handleListPeers(srvCtx *ServerContext, reqBytes []byte) (resBytes []byte, err error) {
	// For the sake of conciseness -> currently unmarshalling empty slice to empty struct
	var listPeersReq ListPeersRequest
	err = json.Unmarshal(reqBytes, &listPeersReq)

	if err != nil {
		return nil, err
	}

	srvCtx.peersListLock.RLock()
	peersFreeze := make([]Peer, len(srvCtx.peersList))
	copy(peersFreeze, srvCtx.peersList)
	srvCtx.peersListLock.RUnlock()
	listPeersRes := ListPeersResponse{make([]PeerInfo, 0)}

	for _, peer := range peersFreeze {
		listPeersRes.PeersInfo = append(
			listPeersRes.PeersInfo,
			PeerInfo{peer.id, peer.conn.RemoteAddr().String()},
		)
	}

	resBytes, err = json.Marshal(listPeersRes)

	if err != nil {
		return nil, err
	}

	return resBytes, nil
}

func printConnectedPeers(srvCtx *ServerContext) {
	srvCtx.peersListLock.RLock()
	log.Println("[Server] Displaying all connections:")

	for _, p := range srvCtx.peersList {
		log.Printf("[Server] ID#%d: %s\n", p.id, p.conn.RemoteAddr())
	}

	srvCtx.peersListLock.RUnlock()
}

func runServer() {
	idCounter := 0
	srvCtx := &ServerContext{peersList: make([]Peer, 0)}
	ln, err := net.Listen("tcp", ":8080")

	if err != nil {
		log.Println(err)
	}

	go func() {
		for {
			printConnectedPeers(srvCtx)
			time.Sleep(time.Second * 5)
		}
	}()

	for {
		c, err := ln.Accept()

		if err != nil {
			log.Println(err)
			break
		}

		log.Printf("[Server] client connected %s\n", c.RemoteAddr())
		idCounter++
		newPeer := Peer{idCounter, c}
		srvCtx.peersListLock.Lock()
		srvCtx.peersList = append(srvCtx.peersList, newPeer)
		srvCtx.peersListLock.Unlock()
		go handlePeer(srvCtx, newPeer)
	}
}

func runClient() {
	conn, err := net.Dial("tcp", ":8080")

	if err != nil {
		log.Println("[Client] err connecting")
		return
	}

	defer func() {
		_ = conn.Close()
	}()

	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)

	log.Println("[Client] connected to server")
	time.Sleep(time.Second * 1)

	echoReq := EchoRequest{5}
	reqBytes, _ := json.Marshal(echoReq)
	bw.WriteByte(echoRID)
	bw.Write(reqBytes)
	bw.WriteByte('\n')
	bw.Flush()
	resBytes, _ := br.ReadBytes('\n')
	var echoRes EchoResponse
	json.Unmarshal(resBytes, &echoRes)
	log.Printf("[Client] echo sent (5), got %d\n", echoRes.EchoByte)

	listReq := ListPeersRequest{}
	reqBytes, _ = json.Marshal(listReq)
	bw.WriteByte(listPeersRID)
	bw.Write(reqBytes)
	bw.WriteByte('\n')
	bw.Flush()
	resBytes, _ = br.ReadBytes('\n')
	var listRes ListPeersResponse
	json.Unmarshal(resBytes, &listRes)
	log.Println("[Client] printing all peers:")

	for _, peer := range listRes.PeersInfo {
		log.Printf("[Client] Peer#%d from %s", peer.ID, peer.Addr)
	}

	time.Sleep(time.Second * 5)
}

func main() {
	args := os.Args[1:]

	if len(args) != 1 {
		log.Fatalln("You must provide only one argument which is type of " +
			"application: 'server' or 'client'")
	}

	runType := args[0]

	if runType == "client" {
		runClient()
	} else if runType == "server" {
		runServer()
	} else {
		log.Fatalf("Unknown run type %s\n", runType)
	}
}
