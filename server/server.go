package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
	"krzyzanowski.dev/archat/common"
)

type Peer struct {
	id         int
	conn       *websocket.Conn
	hasAccount bool
	account    *Account
}

func NewPeer(conn *websocket.Conn) *Peer {
	return &Peer{-1, conn, false, nil}
}

func (p *Peer) NicknameOrEmpty() string {
	if p.hasAccount {
		return p.account.nickname
	} else {
		return ""
	}
}

type Account struct {
	nickname string
	passHash []byte
}

func NewInitiation(abA string, abB string) *common.Initiation {
	return &common.Initiation{AbANick: abA, AbBNick: abB, Stage: common.InitiationStageA}
}

type Context struct {
	idCounter           int
	idCounterLock       sync.RWMutex
	peersList           []*Peer
	peersListLock       sync.RWMutex
	accounts            map[string]*Account
	accountsLock        sync.RWMutex
	initiations         []*common.Initiation
	initiationsLock     sync.RWMutex
	handlerContexts     []*HandlerContext
	handlerContextsLock sync.RWMutex
}

func NewContext() *Context {
	return &Context{
		peersList:   make([]*Peer, 0),
		accounts:    make(map[string]*Account),
		initiations: make([]*common.Initiation, 0),
	}
}

// Remember to lock before calling
func (ctx *Context) getPeerByNick(nick string) (*Peer, error) {
	for _, peer := range ctx.peersList {
		if peer.hasAccount && peer.account.nickname == nick {
			return peer, nil
		}
	}
	return nil, errors.New("peer not found")
}

// Remember to lock before calling
func (ctx *Context) getCtxByNick(nick string) (*HandlerContext, error) {
	idx := slices.IndexFunc[[]*HandlerContext, *HandlerContext](
		ctx.handlerContexts,
		func(handlerContext *HandlerContext) bool {
			return handlerContext.peer.hasAccount && handlerContext.peer.account.nickname == nick
		})

	if idx != -1 {
		return ctx.handlerContexts[idx], nil
	}

	return nil, errors.New("not found")
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

func (hdlCtx *HandlerContext) clientHandler(syncCtx context.Context) error {
handleNext:
	for {
		select {
		case <-syncCtx.Done():
			return nil
		case reqFrame := <-hdlCtx.reqFromClient:
			var res common.Response
			var err error

			if reqFrame.ID == common.AuthReqID {
				res, err = hdlCtx.handleAuth(&reqFrame)
			} else if reqFrame.ID == common.ListPeersReqID {
				res, err = hdlCtx.handleListPeers(&reqFrame)
			} else if reqFrame.ID == common.EchoReqID {
				res, err = hdlCtx.handleEcho(&reqFrame)
			} else if reqFrame.ID == common.StartChatAReqID {
				res, err = hdlCtx.handleChatStartA(&reqFrame)
			} else if reqFrame.ID == common.StartChatCReqID {
				res, err = hdlCtx.handleChatStartC(&reqFrame)
			} else {
				logger.Warnf("can't handle request of ID=%d", reqFrame.ID)
				continue
			}

			if err != nil {
				logger.Errorf("could not handle request ID=%d", reqFrame.ID)
				return err
			}

			if res == nil {
				logger.Debugf("request without response ID=%d", reqFrame.ID)
				continue handleNext
			}

			resFrame, err := common.ResponseFrameFrom(res)

			if err != nil {
				logger.Errorf("could not create frame from response")
				return err
			}

			hdlCtx.rToClient <- resFrame
		}
	}
}

func (hdlCtx *HandlerContext) clientWriter(syncCtx context.Context) error {
	for {
		select {
		case <-syncCtx.Done():
			return nil
		case rFrame := <-hdlCtx.rToClient:
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
}

func (hdlCtx *HandlerContext) clientReader(syncCtx context.Context) error {
	for {
		select {
		case <-syncCtx.Done():
			return nil
		default:
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
			err = json.Unmarshal(messBytes, &rFrame)

			if err != nil {
				return err
			}

			logger.Debugf("unmarshalled request frame (ID=%d)", rFrame.ID)

			if rFrame.IsResponse() {
				logger.Debug("it is response frame", "id", rFrame.ID)
				hdlCtx.resFromClient <- rFrame
			} else {
				logger.Debug("it is request frame", "id", rFrame.ID)
				hdlCtx.reqFromClient <- rFrame
			}
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

type Matcher[T any] func(*T) bool

func (ctx *Context) removePeer(peer *Peer) {
	ctx.handlerContextsLock.Lock()
	ctx.peersListLock.Lock()
	ctx.initiationsLock.Lock()

	ctx.handlerContexts = slices.DeleteFunc[[]*HandlerContext, *HandlerContext](
		ctx.handlerContexts,
		func(h *HandlerContext) bool {
			return h.peer.id == peer.id
		})

	ctx.peersList = slices.DeleteFunc[[]*Peer, *Peer](
		ctx.peersList,
		func(p *Peer) bool {
			return p.id == peer.id
		})

	ctx.initiations = slices.DeleteFunc[[]*common.Initiation, *common.Initiation](
		ctx.initiations,
		func(i *common.Initiation) bool {
			return peer.hasAccount && (peer.account.nickname == i.AbANick || peer.account.nickname == i.AbBNick)
		})

	// TODO: Inform the other side about peer leaving

	ctx.handlerContextsLock.Unlock()
	ctx.peersListLock.Unlock()
	ctx.initiationsLock.Unlock()
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
				ID:          peer.id,
				Addr:        peer.conn.RemoteAddr().String(),
				HasNickname: peer.hasAccount,
				Nickname:    peer.NicknameOrEmpty(),
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

func (hdlCtx *HandlerContext) handleChatStartA(reqFrame *common.RFrame) (res common.Response, err error) {
	startChatAReq, err := common.RequestFromFrame[common.StartChatARequest](*reqFrame)

	if err != nil {
		return nil, err
	}

	receiverPeerCtx, err := hdlCtx.getCtxByNick(startChatAReq.Nickname)

	if err != nil {
		logger.Debug("receiver peer not found")
		return nil, nil
	}

	// initation started
	hdlCtx.initiationsLock.Lock()
	hdlCtx.initiations = append(hdlCtx.initiations, NewInitiation(hdlCtx.peer.account.nickname, startChatAReq.Nickname))
	hdlCtx.initiationsLock.Unlock()

	chatStartB := common.StartChatBRequest{
		Nickname: hdlCtx.peer.account.nickname,
	}

	chatStartBReqF, err := common.RequestFrameFrom(chatStartB)

	if err != nil {
		logger.Debug("chat start B req frame creation failed")
		return nil, err
	}

	receiverPeerCtx.rToClient <- chatStartBReqF

	hdlCtx.initiationsLock.Lock()
	idx := slices.IndexFunc(hdlCtx.initiations, func(i *common.Initiation) bool {
		return i.AbANick == hdlCtx.peer.account.nickname && i.AbBNick == startChatAReq.Nickname
	})
	hdlCtx.initiations[idx].Stage = common.InitiationStageB
	hdlCtx.initiationsLock.Unlock()

	return nil, nil
}

func (hdlCtx *HandlerContext) handleChatStartC(reqFrame *common.RFrame) (res common.Response, err error) {
	hdlCtx.initiationsLock.Lock()
	startChatCReq, err := common.RequestFromFrame[common.StartChatCRequest](*reqFrame)

	if err != nil {
		return nil, err
	}

	logger.Debugf("got chat start c for %s", startChatCReq.Nickname)

	receiverPeerCtx, err := hdlCtx.getCtxByNick(startChatCReq.Nickname)

	if err != nil {
		logger.Debug("receiver peer not found")
		return nil, nil
	}

	idx := slices.IndexFunc(hdlCtx.initiations, func(i *common.Initiation) bool {
		return i.AbBNick == hdlCtx.peer.account.nickname && i.AbANick == startChatCReq.Nickname
	})

	if idx == -1 {
		logger.Debug("initation not found, won't handle")
		return nil, nil
	}

	if hdlCtx.initiations[idx].Stage != common.InitiationStageB {
		logger.Debug("initation found, but is not in stage B, won't handle")
		return nil, nil
	}

	hdlCtx.initiations[idx].Stage = common.InitiationStageC

	aCode, err := generatePunchCode()
	if err != nil {
		logger.Error("failed generating punch code for a")
		return nil, nil
	}

	bCode, err := generatePunchCode()
	if err != nil {
		logger.Error("failed generating punch code for b")
		return nil, nil
	}

	hdlCtx.initiations[idx].AbAPunchCode = aCode
	hdlCtx.initiations[idx].AbBPunchCode = bCode

	dReqToA := common.StartChatDRequest{Nickname: hdlCtx.peer.account.nickname, PunchCode: aCode}
	dReqToB := common.StartChatDRequest{Nickname: startChatCReq.Nickname, PunchCode: bCode}

	err = hdlCtx.sendRequest(dReqToB)

	if err != nil {
		logger.Errorf("could not send chatstartd to B=%s", hdlCtx.peer.account.nickname)
		return nil, nil
	}

	err = receiverPeerCtx.sendRequest(dReqToA)

	if err != nil {
		logger.Errorf("could not send chatstartd to A=%s", startChatCReq.Nickname)
		return nil, nil
	}

	hdlCtx.initiations[idx].Stage = common.InitiationStageD

	hdlCtx.initiationsLock.Unlock()
	return nil, nil
}

func generatePunchCode() (string, error) {
	codeBytes := make([]byte, 8)
	_, err := rand.Read(codeBytes)

	if err != nil {
		return "", err
	}

	for idx, cb := range codeBytes {
		codeBytes[idx] = 65 + (cb % 26)
	}

	return string(codeBytes), nil
}

func (ctx *Context) printDebugInfo() {
	ctx.peersListLock.RLock()
	logger.Debug("================================ server state")
	logger.Debug("displaying all connections:")

	for _, p := range ctx.peersList {
		nick := "-"

		if p.hasAccount {
			nick = p.account.nickname
		}

		logger.Debugf("ID#%d, Addr:%s, Auth:%t, Nick:%s", p.id, p.conn.RemoteAddr(), p.hasAccount, nick)
	}

	logger.Debug("displaying all initiations:")

	for _, i := range ctx.initiations {
		logger.Debugf("from %s to %s, stage: %d", i.AbANick, i.AbBNick, i.Stage)
	}

	ctx.peersListLock.RUnlock()
}

func (ctx *Context) addPeer(peer *Peer) {
	ctx.idCounterLock.Lock()
	ctx.idCounter++
	peer.id = ctx.idCounter
	ctx.idCounterLock.Unlock()
	ctx.peersListLock.Lock()
	ctx.peersList = append(ctx.peersList, peer)
	ctx.peersListLock.Unlock()
}

func testEcho(hdlCtx *HandlerContext) {
	logger.Debug("sending echo request...")
	_ = hdlCtx.sendRequest(common.EchoRequest{EchoByte: 123})
	logger.Debug("sent")
	echoResF := hdlCtx.getResponseFrame()
	logger.Debug("got response")
	echoRes, err := common.ResponseFromFrame[common.EchoResponse](echoResF)

	if err != nil {
		logger.Error(err)
		return
	}

	logger.Debug("test echo done", "byteSent", 123, "byteReceived", echoRes.EchoByte)
}

func (ctx *Context) wsapiHandler(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		logger.Errorf("upgrade failed")
		return
	}

	peer := NewPeer(conn)
	ctx.addPeer(peer)
	handlerCtx := NewHandlerContext(peer, ctx)
	ctx.handlerContextsLock.Lock()
	ctx.handlerContexts = append(ctx.handlerContexts, handlerCtx)
	ctx.handlerContextsLock.Unlock()
	defer handleDisconnection(handlerCtx)

	defer func(conn *websocket.Conn) {
		err := conn.Close()
		if err != nil {
			logger.Error(err)
		}
	}(conn)

	logger.Infof("%s connected", conn.RemoteAddr())

	errGroup, syncCtx := errgroup.WithContext(context.Background())

	errGroup.Go(func() error {
		return handlerCtx.clientHandler(syncCtx)
	})

	errGroup.Go(func() error {
		return handlerCtx.clientWriter(syncCtx)
	})

	errGroup.Go(func() error {
		return handlerCtx.clientReader(syncCtx)
	})

	errGroup.Go(func() error {
		<-syncCtx.Done()
		time.Sleep(time.Second * 3)
		close(handlerCtx.rToClient)
		close(handlerCtx.resFromClient)
		close(handlerCtx.reqFromClient)
		return conn.Close()
	})

	testEcho(handlerCtx)
	err = errGroup.Wait()

	if err != nil {
		logger.Error(err)
		return
	}
}

func (srvCtx *Context) handleUDP(data []byte, addr net.Addr) {
	var punchReq common.PunchRequest
	err := json.Unmarshal(data, &punchReq)

	if err != nil {
		logger.Error("error unmarshalling punch request", "err", err)
		return
	}

	logger.Debugf("got punch request %+v", punchReq)

	srvCtx.initiationsLock.Lock()
	defer srvCtx.initiationsLock.Unlock()

	idx := slices.IndexFunc(srvCtx.initiations, func(i *common.Initiation) bool {
		return i.AbAPunchCode == punchReq.PunchCode ||
			i.AbBPunchCode == punchReq.PunchCode
	})

	if idx == -1 {
		logger.Debugf("haven't found initiation for the request")
		return
	}

	matchedInitation := srvCtx.initiations[idx]
	logger.Debugf("matched initiation %+v", matchedInitation)

	if matchedInitation.AbAPunchCode == punchReq.PunchCode {
		matchedInitation.AbAAddress = addr.String()
	} else {
		matchedInitation.AbBAddress = addr.String()
	}

	if matchedInitation.AbAAddress == "" || matchedInitation.AbBAddress == "" {
		// does not have two addresses can't do anything yet
		return
	}

	logger.Debugf("finished completing initiation %+v", matchedInitation)
	logger.Debug("now sending peers their addresses")

	srvCtx.peersListLock.Lock()
	defer srvCtx.peersListLock.Unlock()

	abA, err := srvCtx.getCtxByNick(matchedInitation.AbANick)

	if err != nil {
		logger.Debug("could not finish punching, abA not found",
			"err", err)
		return
	}

	abB, err := srvCtx.getCtxByNick(matchedInitation.AbBNick)

	if err != nil {
		logger.Debug("could not finish punching, abB not found",
			"err", err)
		return
	}

	err = abA.sendRequest(common.StartChatFinishRequest{
		OtherSideNickname: matchedInitation.AbBNick,
		OtherSideAddress:  matchedInitation.AbBAddress,
	})

	if err != nil {
		logger.Debug("could not send start chat finish request to abA, aborting",
			"err", err)
		return
	}

	err = abB.sendRequest(common.StartChatFinishRequest{
		OtherSideNickname: matchedInitation.AbANick,
		OtherSideAddress:  matchedInitation.AbAAddress,
	})

	if err != nil {
		logger.Debug("could not send start chat finish request to abB, aborting",
			"err", err)
		return
	}
}

func RunServer(settings common.ServerSettings) {
	srvCtx := NewContext()

	go func() {
		for {
			srvCtx.printDebugInfo()
			time.Sleep(time.Second * 5)
		}
	}()

	http.HandleFunc("/wsapi", srvCtx.wsapiHandler)
	logger.Infof("Starting websocket server on %s...", settings.WsapiAddr)
	go func() {
		err := http.ListenAndServe(settings.WsapiAddr, nil)
		if err != nil {
			logger.Error(err)
		}
	}()

	logger.Infof("Starting punching server on %s...", settings.UdpAddr)
	listener, err := net.ListenPacket("udp", settings.UdpAddr)
	if err != nil {
		logger.Error("could not create listener for punching server", err)
	}

	for {
		data := make([]byte, 65536)
		n, punchAddr, err := listener.ReadFrom(data)
		if err != nil {
			logger.Error("error reading from punching server", err)
			continue
		}

		data = data[:n]
		logger.Debugf("got message: %+v", data)
		srvCtx.handleUDP(data, punchAddr)
	}
}
