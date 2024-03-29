package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
	"krzyzanowski.dev/p2pchat/common"
)

type Account struct {
	nickname string
	passHash []byte
}

type Context struct {
	idCounter     int
	idCounterLock sync.RWMutex
	peersList     []*Peer
	peersListLock sync.RWMutex
	accounts      map[string]*Account
	accountsLock  sync.RWMutex
}

type HandlerContext struct {
	peer *Peer
	*Context
	resFromClient chan common.RFrame
	reqFromClient chan common.RFrame
	rToClient     chan common.RFrame
}

func NewHandlerContext(peer *Peer, srvCtx *Context) *HandlerContext {
	return &HandlerContext{
		peer,
		srvCtx,
		make(chan common.RFrame),
		make(chan common.RFrame),
		make(chan common.RFrame),
	}
}

func (hdlCtx *HandlerContext) clientHandler(hdlWg *sync.WaitGroup) error {
	defer hdlWg.Done()

	for {
		reqFrame := <-hdlCtx.reqFromClient
		var res common.Response
		var err error

		if reqFrame.ID == common.AuthReqID {
			res, err = hdlCtx.handleAuth(&reqFrame)
		} else if reqFrame.ID == common.ListPeersReqID {
			res, err = hdlCtx.handleListPeers(&reqFrame)
		} else if reqFrame.ID == common.EchoReqID {
			res, err = hdlCtx.handleEcho(&reqFrame)
		}

		if err != nil {
			logger.Errorf("could not handle request ID=%d", reqFrame.ID)
			return err
		}

		resFrame, err := common.ResponseFrameFrom(res)
		if err != nil {
			logger.Errorf("could not create frame from response")
			return err
		}

		hdlCtx.rToClient <- resFrame
	}
}

func (hdlCtx *HandlerContext) clientWriter(hdlWg *sync.WaitGroup) error {
	defer hdlWg.Done()

	for {
		rFrame := <-hdlCtx.rToClient
		resJsonBytes, err := json.Marshal(rFrame)
		if err != nil {
			logger.Errorf("error marshalling frame to json")
			return err
		}

		logger.Debugf("sending %s", string(resJsonBytes))
		err = hdlCtx.peer.conn.WriteMessage(websocket.TextMessage, resJsonBytes)
		if err != nil {
			logger.Errorf("error writing rframe")
			return err
		}
	}
}

func (hdlCtx *HandlerContext) clientReader(hdlWg *sync.WaitGroup) error {
	defer hdlWg.Done()

	for {
		messType, messBytes, err := hdlCtx.peer.conn.ReadMessage()
		if err != nil {
			return err
		}

		if messType != 1 {
			err := hdlCtx.peer.conn.WriteMessage(websocket.CloseUnsupportedData, []byte("Only JSON text is supported"))
			if err != nil {
				logger.Debugf("[Server] error sending unsupported data close message")
				return err
			}
		}

		logger.Debugf("got message text: %s", strings.Trim(string(messBytes), "\n"))
		var rFrame common.RFrame
		json.Unmarshal(messBytes, &rFrame)
		logger.Debugf("unmarshalled request frame (ID=%d)", rFrame.ID)

		if rFrame.ID > 128 {
			logger.Debug("it is response frame", "id", rFrame.ID)
			hdlCtx.resFromClient <- rFrame
		} else {
			logger.Debug("it is request frame", "id", rFrame.ID)
			hdlCtx.reqFromClient <- rFrame
		}
	}
}

func (hdlCtx *HandlerContext) sendRequest(req common.Request) error {
	rf, err := common.RequestFrameFrom(req)
	if err != nil {
		return err
	}

	hdlCtx.rToClient <- rf
	return nil
}

func (hdlCtx *HandlerContext) getResponseFrame() common.RFrame {
	return <-hdlCtx.resFromClient
}

type Peer struct {
	id         int
	conn       *websocket.Conn
	hasAccount bool
	account    *Account
}

var logger = log.NewWithOptions(os.Stdout, log.Options{
	ReportTimestamp: true,
	TimeFormat:      time.TimeOnly,
	Prefix:          "⚙️  Server",
})

func init() {
	if common.IsProd {
		logger.SetLevel(log.InfoLevel)
	} else {
		logger.SetLevel(log.DebugLevel)
	}
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

func (srvCtx *Context) removePeer(peer *Peer) {
	srvCtx.peersListLock.Lock()
	peerSliceRemove(&srvCtx.peersList, peerSliceIndexOf(srvCtx.peersList, peer.id))
	srvCtx.peersListLock.Unlock()
}

func handleDisconnection(handlerCtx *HandlerContext) {
	handlerCtx.removePeer(handlerCtx.peer)
	logger.Infof("%s disconnected", handlerCtx.peer.conn.RemoteAddr())
}

func (hdlCtx *HandlerContext) handleEcho(reqFrame *common.RFrame) (res common.Response, err error) {
	echoReq, err := common.RequestFromFrame[common.EchoRequest](*reqFrame)
	if err != nil {
		logger.Error("could not read request from frame")
		return nil, err
	}

	echoRes := common.EchoResponse(echoReq)
	return echoRes, nil
}

func (hdlCtx *HandlerContext) handleListPeers(reqFrame *common.RFrame) (res common.Response, err error) {
	// Currently list peers request is empty, so we can ignore it - we won't use it
	_, err = common.RequestFromFrame[common.ListPeersRequest](*reqFrame)
	if err != nil {
		logger.Error("could not read request from frame")
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

func (hdlCtx *HandlerContext) handleAuth(reqFrame *common.RFrame) (res common.Response, err error) {
	authReq, err := common.RequestFromFrame[common.AuthRequest](*reqFrame)
	if err != nil {
		logger.Error("could not read request from frame")
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

func (srvCtx *Context) printConnectedPeers() {
	srvCtx.peersListLock.RLock()
	logger.Debug("displaying all connections:")

	for _, p := range srvCtx.peersList {
		nick := "-"

		if p.hasAccount {
			nick = p.account.nickname
		}

		logger.Debugf("ID#%d, Addr:%s, Auth:%t, Nick:%s", p.id, p.conn.RemoteAddr(), p.hasAccount, nick)
	}

	srvCtx.peersListLock.RUnlock()
}

func (srvCtx *Context) addPeer(peer *Peer) {
	srvCtx.idCounterLock.Lock()
	srvCtx.idCounter++
	peer.id = srvCtx.idCounter
	srvCtx.idCounterLock.Unlock()
	srvCtx.peersListLock.Lock()
	srvCtx.peersList = append(srvCtx.peersList, peer)
	srvCtx.peersListLock.Unlock()
}

func (srvCtx *Context) wsapiHandler(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorf("upgrade failed")
		return
	}

	peer := NewPeer(conn)
	srvCtx.addPeer(peer)
	handlerCtx := NewHandlerContext(peer, srvCtx)
	defer handleDisconnection(handlerCtx)
	defer conn.Close()
	logger.Infof("%s connected", conn.RemoteAddr())

	var handlerWg sync.WaitGroup
	handlerWg.Add(3)
	go handlerCtx.clientWriter(&handlerWg)
	go handlerCtx.clientHandler(&handlerWg)
	go handlerCtx.clientReader(&handlerWg)

	logger.Debug("sending echo request...")
	handlerCtx.sendRequest(common.EchoRequest{EchoByte: 123})
	logger.Debug("sent")
	echoResF := handlerCtx.getResponseFrame()
	logger.Debug("got response")
	echoRes, err := common.ResponseFromFrame[common.EchoResponse](echoResF)
	if err != nil {
		logger.Error(err)
		return
	}
	logger.Debug("test echo done", "byteSent", 123, "byteReceived", echoRes.EchoByte)

	handlerWg.Wait()
}

func RunServer() {
	srvCtx := &Context{peersList: make([]*Peer, 0), accounts: make(map[string]*Account)}

	go func() {
		for {
			srvCtx.printConnectedPeers()
			time.Sleep(time.Second * 5)
		}
	}()

	http.HandleFunc("/wsapi", srvCtx.wsapiHandler)
	logger.Info("Starting server...")
	http.ListenAndServe(":8080", nil)
}
