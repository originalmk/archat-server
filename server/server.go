package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
	"krzyzanowski.dev/p2pchat/common"
)

type Account struct {
	nickname string
	passHash []byte
}

type ServerContext struct {
	idCounter     int
	idCounterLock sync.RWMutex
	peersList     []*Peer
	peersListLock sync.RWMutex
	accounts      map[string]*Account
	accountsLock  sync.RWMutex
}

type HandlerContext struct {
	peer *Peer
	*ServerContext
}

type Peer struct {
	id         int
	conn       *websocket.Conn
	hasAccount bool
	account    *Account
}

func NewPeer(conn *websocket.Conn) *Peer {
	return &Peer{-1, conn, false, nil}
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

func (srvCtx *ServerContext) removePeer(peer *Peer) {
	srvCtx.peersListLock.Lock()
	peerSliceRemove(&srvCtx.peersList, peerSliceIndexOf(srvCtx.peersList, peer.id))
	srvCtx.peersListLock.Unlock()
}

func handleDisconnection(handlerCtx *HandlerContext) {
	handlerCtx.removePeer(handlerCtx.peer)
	log.Printf("[Server] %s disconnected\n", handlerCtx.peer.conn.RemoteAddr())
}

func (hdlCtx *HandlerContext) handleEcho(reqFrame *common.RequestFrame) (res common.Response, err error) {
	echoReq, err := common.RequestFromFrame[common.EchoRequest](*reqFrame)
	if err != nil {
		log.Println("[Server] could not read request from frame")
		return nil, err
	}

	echoRes := common.EchoResponse(echoReq)
	return echoRes, nil
}

func (hdlCtx *HandlerContext) handleListPeers(reqFrame *common.RequestFrame) (res common.Response, err error) {
	// Currently list peers request is empty, so we can ignore it - we won't use it
	_, err = common.RequestFromFrame[common.ListPeersRequest](*reqFrame)
	if err != nil {
		log.Println("[Server] could not read request from frame")
		return nil, err
	}

	hdlCtx.peersListLock.RLock()
	peersFreeze := make([]*Peer, len(hdlCtx.peersList))
	copy(peersFreeze, hdlCtx.peersList)
	hdlCtx.peersListLock.RUnlock()
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

	return listPeersRes, nil
}

func (hdlCtx *HandlerContext) handleAuth(reqFrame *common.RequestFrame) (res common.Response, err error) {
	authReq, err := common.RequestFromFrame[common.AuthRequest](*reqFrame)
	if err != nil {
		log.Println("[Server] could not read request from frame")
		return nil, err
	}

	// Check if account already exists
	hdlCtx.accountsLock.RLock()
	account, ok := hdlCtx.accounts[authReq.Nickname]
	hdlCtx.accountsLock.RUnlock()
	var authRes *common.AuthResponse

	if ok {
		// Check if password matches
		if bcrypt.CompareHashAndPassword(account.passHash, []byte(authReq.Password)) == nil {
			authRes = &common.AuthResponse{IsSuccess: true}
			hdlCtx.peersListLock.Lock()
			hdlCtx.peer.hasAccount = true
			hdlCtx.peer.account = account
			hdlCtx.peersListLock.Unlock()
		} else {
			authRes = &common.AuthResponse{IsSuccess: false}
		}
	} else {
		authRes = &common.AuthResponse{IsSuccess: true}
		passHash, err := bcrypt.GenerateFromPassword([]byte(authReq.Password), bcrypt.DefaultCost)

		if err != nil {
			authRes = &common.AuthResponse{IsSuccess: false}
		} else {
			newAcc := Account{authReq.Nickname, passHash}
			hdlCtx.accountsLock.Lock()
			hdlCtx.accounts[newAcc.nickname] = &newAcc
			hdlCtx.accountsLock.Unlock()
			hdlCtx.peersListLock.Lock()
			hdlCtx.peer.hasAccount = true
			hdlCtx.peer.account = &newAcc
			hdlCtx.peersListLock.Unlock()
		}
	}

	return authRes, nil
}

func (srvCtx *ServerContext) printConnectedPeers() {
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

func (hdlCtx *HandlerContext) handleRequest(reqJsonBytes []byte) error {
	log.Printf("[Server] got message text: %s\n", strings.Trim(string(reqJsonBytes), "\n"))
	var reqFrame common.RequestFrame
	json.Unmarshal(reqJsonBytes, &reqFrame)
	log.Printf("[Server] unmarshalled request frame (ID=%d)\n", reqFrame.ID)
	var res common.Response
	var err error

	if reqFrame.ID == common.AuthRID {
		res, err = hdlCtx.handleAuth(&reqFrame)
	} else if reqFrame.ID == common.ListPeersRID {
		res, err = hdlCtx.handleListPeers(&reqFrame)
	} else if reqFrame.ID == common.EchoRID {
		res, err = hdlCtx.handleEcho(&reqFrame)
	}

	if err != nil {
		log.Printf("[Server] could not handle request ID=%d\n", reqFrame.ID)
		return err
	}

	resFrame, err := common.ResponseFrameFrom(res)
	if err != nil {
		log.Println("[Server] could not create frame from response")
		return err
	}

	resJsonBytes, err := json.Marshal(resFrame)
	if err != nil {
		log.Println("[Server] error marshalling frame to json")
		return err
	}

	log.Printf("[Server] sending %s\n", string(resJsonBytes))
	err = hdlCtx.peer.conn.WriteMessage(websocket.TextMessage, resJsonBytes)
	if err != nil {
		log.Println("[Server] error writing response frame")
		return err
	}

	return nil
}

func (srvCtx *ServerContext) addPeer(peer *Peer) {
	srvCtx.idCounterLock.Lock()
	srvCtx.idCounter++
	peer.id = srvCtx.idCounter
	srvCtx.idCounterLock.Unlock()
	srvCtx.peersListLock.Lock()
	srvCtx.peersList = append(srvCtx.peersList, peer)
	srvCtx.peersListLock.Unlock()
}

func (srvCtx *ServerContext) wsapiHandler(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("[Server] upgrade failed")
		return
	}

	peer := NewPeer(conn)
	srvCtx.addPeer(peer)
	handlerCtx := &HandlerContext{peer, srvCtx}
	defer handleDisconnection(handlerCtx)
	defer conn.Close()
	log.Printf("[Server] %s connected\n", conn.RemoteAddr())

	for {
		messType, messBytes, err := conn.ReadMessage()
		if err != nil {
			break
		}

		if messType != 1 {
			err := conn.WriteMessage(websocket.CloseUnsupportedData, []byte("Only JSON text is supported"))
			if err != nil {
				log.Println("[Server] error sending close message due to unsupported data")
			}

			return
		}

		err = handlerCtx.handleRequest(messBytes)
		if err != nil {
			log.Println(err)
			break
		}
	}
}

func RunServer() {
	srvCtx := &ServerContext{peersList: make([]*Peer, 0), accounts: make(map[string]*Account)}

	go func() {
		for {
			srvCtx.printConnectedPeers()
			time.Sleep(time.Second * 5)
		}
	}()

	http.HandleFunc("/wsapi", srvCtx.wsapiHandler)
	http.ListenAndServe(":8080", nil)
}
