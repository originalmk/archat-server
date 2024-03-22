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

	"golang.org/x/crypto/bcrypt"
)

type Account struct {
	nickname string
	passHash []byte
}

type ServerContext struct {
	peersList     []*Peer
	peersListLock sync.RWMutex
	accounts      map[string]*Account
	accountsLock  sync.RWMutex
}

type HandlerContext struct {
	peer   *Peer
	srvCtx *ServerContext
}

type Peer struct {
	id         int
	conn       net.Conn
	hasAccount bool
	account    *Account
}

type PeerInfo struct {
	ID           int    `json:"id"`
	Addr         string `json:"addr"`
	HasNickaname bool   `json:"hasNickname"`
	Nickname     string `json:"nickname"`
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

type AuthRequest struct {
	Nickname string
	Password string
}

type AuthResponse struct {
	IsSuccess bool
}

const (
	echoRID      = 1
	listPeersRID = 2
	authRID      = 3
)

func peerSliceIndexOf(s []*Peer, id int) int {
	i := 0
	var p *Peer
	for i, p = range s {
		if p.id == id {
			break
		}
	}
	return i
}

func peerSliceRemove(s *[]*Peer, i int) {
	(*s)[i] = (*s)[len(*s)-1]
	*s = (*s)[:len(*s)-1]
}

func handleDisconnection(handlerCtx *HandlerContext) {
	handlerCtx.srvCtx.peersListLock.Lock()
	p := handlerCtx.srvCtx.peersList[peerSliceIndexOf(handlerCtx.srvCtx.peersList, handlerCtx.peer.id)]
	log.Printf("[Server] %s disconnected\n", p.conn.RemoteAddr())
	peerSliceRemove(&handlerCtx.srvCtx.peersList, peerSliceIndexOf(handlerCtx.srvCtx.peersList, handlerCtx.peer.id))
	handlerCtx.srvCtx.peersListLock.Unlock()
}

func handlePeer(handlerCtx *HandlerContext) {
	br := bufio.NewReader(handlerCtx.peer.conn)
	bw := bufio.NewWriter(handlerCtx.peer.conn)

	for {
		reqBytes, err := br.ReadBytes('\n')

		if err == io.EOF {
			handleDisconnection(handlerCtx)
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
			resBytes, err = handleEcho(handlerCtx, reqJsonBytes)
		} else if operationCode == listPeersRID {
			resBytes, err = handleListPeers(handlerCtx, reqJsonBytes)
		} else if operationCode == authRID {
			resBytes, err = handleAuth(handlerCtx, reqJsonBytes)
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

func handleEcho(_ *HandlerContext, reqBytes []byte) (resBytes []byte, err error) {
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

func handleListPeers(handlerCtx *HandlerContext, reqBytes []byte) (resBytes []byte, err error) {
	var listPeersReq ListPeersRequest
	err = json.Unmarshal(reqBytes, &listPeersReq)

	if err != nil {
		return nil, err
	}

	handlerCtx.srvCtx.peersListLock.RLock()
	peersFreeze := make([]*Peer, len(handlerCtx.srvCtx.peersList))
	copy(peersFreeze, handlerCtx.srvCtx.peersList)
	handlerCtx.srvCtx.peersListLock.RUnlock()
	listPeersRes := ListPeersResponse{make([]PeerInfo, 0)}

	for _, peer := range peersFreeze {
		listPeersRes.PeersInfo = append(
			listPeersRes.PeersInfo,
			PeerInfo{peer.id, peer.conn.RemoteAddr().String(), peer.hasAccount, peer.account.nickname},
		)
	}

	resBytes, err = json.Marshal(listPeersRes)

	if err != nil {
		return nil, err
	}

	return resBytes, nil
}

func handleAuth(handlerCtx *HandlerContext, reqBytes []byte) (resBytes []byte, err error) {
	var authReq AuthRequest
	err = json.Unmarshal(reqBytes, &authReq)

	if err != nil {
		return nil, err
	}

	// Check if account already exists
	handlerCtx.srvCtx.accountsLock.RLock()
	account, ok := handlerCtx.srvCtx.accounts[authReq.Nickname]
	handlerCtx.srvCtx.accountsLock.RUnlock()
	var authRes AuthResponse

	if ok {
		// Check if password matches
		if bcrypt.CompareHashAndPassword(account.passHash, []byte(authReq.Password)) == nil {
			authRes = AuthResponse{true}
			handlerCtx.srvCtx.peersListLock.Lock()
			handlerCtx.peer.hasAccount = true
			handlerCtx.peer.account = account
			handlerCtx.srvCtx.peersListLock.Unlock()
		} else {
			authRes = AuthResponse{false}
		}
	} else {
		authRes = AuthResponse{true}
		passHash, err := bcrypt.GenerateFromPassword([]byte(authReq.Password), bcrypt.DefaultCost)

		if err != nil {
			authRes = AuthResponse{false}
		} else {
			newAcc := Account{authReq.Nickname, passHash}
			handlerCtx.srvCtx.accountsLock.Lock()
			handlerCtx.srvCtx.accounts[newAcc.nickname] = &newAcc
			handlerCtx.srvCtx.accountsLock.Unlock()
			handlerCtx.srvCtx.peersListLock.Lock()
			handlerCtx.peer.hasAccount = true
			handlerCtx.peer.account = &newAcc
			handlerCtx.srvCtx.peersListLock.Unlock()
		}
	}

	resBytes, err = json.Marshal(authRes)

	if err != nil {
		return nil, err
	}

	return resBytes, nil
}

func printConnectedPeers(srvCtx *ServerContext) {
	srvCtx.peersListLock.RLock()
	log.Println("[Server] displaying all connections:")

	for _, p := range srvCtx.peersList {
		nick := "-"

		if p.hasAccount {
			nick = p.account.nickname
		}

		log.Printf("[Server] ID#%d, Addr:%s, Auth:%t, Nick:%s\n", p.id, p.conn.RemoteAddr(), p.hasAccount, nick)
	}

	srvCtx.peersListLock.RUnlock()
}

func runServer() {
	idCounter := 0
	srvCtx := &ServerContext{peersList: make([]*Peer, 0), accounts: make(map[string]*Account)}
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
		newPeer := Peer{idCounter, c, false, nil}
		srvCtx.peersListLock.Lock()
		srvCtx.peersList = append(srvCtx.peersList, &newPeer)
		srvCtx.peersListLock.Unlock()
		go handlePeer(&HandlerContext{&newPeer, srvCtx})
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

	authReq := AuthRequest{"maciek", "9maciek1"}
	reqBytes, _ = json.Marshal(authReq)
	bw.WriteByte(authRID)
	bw.Write(reqBytes)
	bw.WriteByte('\n')
	bw.Flush()
	resBytes, _ = br.ReadBytes('\n')
	var authRes AuthResponse
	json.Unmarshal(resBytes, &authRes)
	log.Printf("[Client] authenticated: %t\n", authRes.IsSuccess)

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
		log.Printf("[Client] Peer#%d from %s, hasNick: %t, nick: %s", peer.ID, peer.Addr, peer.HasNickaname, peer.Nickname)
	}

	time.Sleep(time.Second * 10)
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
