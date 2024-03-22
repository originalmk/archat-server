package server

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"krzyzanowski.dev/p2pchat/common"
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

		if operationCode == common.EchoRID {
			resBytes, err = handleEcho(handlerCtx, reqJsonBytes)
		} else if operationCode == common.ListPeersRID {
			resBytes, err = handleListPeers(handlerCtx, reqJsonBytes)
		} else if operationCode == common.AuthRID {
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
	var echoReq common.EchoRequest
	err = json.Unmarshal(reqBytes, &echoReq)

	if err != nil {
		return nil, err
	}

	echoRes := common.EchoResponse(echoReq)
	resBytes, err = json.Marshal(echoRes)

	if err != nil {
		return nil, err
	}

	return resBytes, nil
}

func handleListPeers(handlerCtx *HandlerContext, reqBytes []byte) (resBytes []byte, err error) {
	var listPeersReq common.ListPeersRequest
	err = json.Unmarshal(reqBytes, &listPeersReq)

	if err != nil {
		return nil, err
	}

	handlerCtx.srvCtx.peersListLock.RLock()
	peersFreeze := make([]*Peer, len(handlerCtx.srvCtx.peersList))
	copy(peersFreeze, handlerCtx.srvCtx.peersList)
	handlerCtx.srvCtx.peersListLock.RUnlock()
	listPeersRes := common.ListPeersResponse{PeersInfo: make([]common.PeerInfo, 0)}

	for _, peer := range peersFreeze {
		listPeersRes.PeersInfo = append(
			listPeersRes.PeersInfo,
			common.PeerInfo{
				ID:           peer.id,
				Addr:         peer.conn.RemoteAddr().String(),
				HasNickaname: peer.hasAccount,
				Nickname:     peer.account.nickname,
			},
		)
	}

	resBytes, err = json.Marshal(listPeersRes)

	if err != nil {
		return nil, err
	}

	return resBytes, nil
}

func handleAuth(handlerCtx *HandlerContext, reqBytes []byte) (resBytes []byte, err error) {
	var authReq common.AuthRequest
	err = json.Unmarshal(reqBytes, &authReq)

	if err != nil {
		return nil, err
	}

	// Check if account already exists
	handlerCtx.srvCtx.accountsLock.RLock()
	account, ok := handlerCtx.srvCtx.accounts[authReq.Nickname]
	handlerCtx.srvCtx.accountsLock.RUnlock()
	var authRes common.AuthResponse

	if ok {
		// Check if password matches
		if bcrypt.CompareHashAndPassword(account.passHash, []byte(authReq.Password)) == nil {
			authRes = common.AuthResponse{IsSuccess: true}
			handlerCtx.srvCtx.peersListLock.Lock()
			handlerCtx.peer.hasAccount = true
			handlerCtx.peer.account = account
			handlerCtx.srvCtx.peersListLock.Unlock()
		} else {
			authRes = common.AuthResponse{IsSuccess: false}
		}
	} else {
		authRes = common.AuthResponse{IsSuccess: true}
		passHash, err := bcrypt.GenerateFromPassword([]byte(authReq.Password), bcrypt.DefaultCost)

		if err != nil {
			authRes = common.AuthResponse{IsSuccess: false}
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

func RunServer() {
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
