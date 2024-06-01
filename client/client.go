package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"
	"krzyzanowski.dev/archat/common"
	cm "krzyzanowski.dev/archat/common"
)

type InitiationInfo struct {
	otherSideNick  string
	punchLaddrUsed string
}

type Context struct {
	conn *websocket.Conn
	// Assumption: size of 1 is enough, because first response read will be response for the last request
	// no need to buffer
	resFromServer   chan cm.RFrame
	reqFromServer   chan cm.RFrame
	rToServer       chan cm.RFrame
	initiations     []InitiationInfo
	initiationsLock sync.RWMutex
	settings        common.ClientSettings
}

func NewClientContext(conn *websocket.Conn, settings common.ClientSettings) *Context {
	return &Context{
		conn:          conn,
		resFromServer: make(chan cm.RFrame),
		reqFromServer: make(chan cm.RFrame),
		rToServer:     make(chan cm.RFrame),
		settings:      settings,
	}
}

func (cliCtx *Context) serverHandler(syncCtx context.Context) error {
	defer logger.Debug("server handler last line...")

handleNext:
	for {
		select {
		case <-syncCtx.Done():
			return nil
		case reqFrame := <-cliCtx.reqFromServer:
			logger.Debug("got request from server", "id", reqFrame.ID)

			var res cm.Response
			var err error

			if reqFrame.ID == cm.EchoReqID {
				res, err = cliCtx.handleEcho(reqFrame)
			} else if reqFrame.ID == cm.StartChatBReqID {
				res, err = cliCtx.handleStartChatB(reqFrame)
			} else if reqFrame.ID == cm.StartChatDReqID {
				res, err = cliCtx.handleStartChatD(reqFrame)
			} else if reqFrame.ID == cm.StartChatFinishReqID {
				res, err = cliCtx.handleChatStartFinish(reqFrame)
			} else {
				logger.Warn("can't handle it!")
			}

			if err != nil {
				logger.Errorf("could not handle request ID=%d", reqFrame.ID)
				return err
			}

			if res == nil {
				logger.Debugf("request without response ID=%d", reqFrame.ID)
				continue handleNext
			}

			resFrame, err := cm.ResponseFrameFrom(res)

			if err != nil {
				logger.Errorf("could not create frame from response")
				return err
			}

			cliCtx.rToServer <- resFrame
		}
	}
}

func (cliCtx *Context) handleEcho(reqFrame cm.RFrame) (res cm.Response, err error) {
	echoReq, err := cm.RequestFromFrame[cm.EchoRequest](reqFrame)
	if err != nil {
		return nil, err
	}

	return cm.EchoResponse(echoReq), nil
}

func (cliCtx *Context) handleStartChatB(reqFrame cm.RFrame) (res cm.Response, err error) {
	startChatBReq, err := cm.RequestFromFrame[cm.StartChatBRequest](reqFrame)
	if err != nil {
		return nil, err
	}

	logger.Infof("got start chat, %s wants to contact. use startchatc command to "+
		"decide if you want to accept the chat", startChatBReq.Nickname)

	cliCtx.initiationsLock.Lock()
	cliCtx.initiations = append(cliCtx.initiations, InitiationInfo{
		otherSideNick: startChatBReq.Nickname,
	})
	cliCtx.initiationsLock.Unlock()

	return nil, nil
}

func (cliCtx *Context) handleStartChatD(reqFrame cm.RFrame) (res cm.Response, err error) {
	cliCtx.initiationsLock.Lock()
	defer cliCtx.initiationsLock.Unlock()

	startChatDReq, err := cm.RequestFromFrame[cm.StartChatDRequest](reqFrame)
	if err != nil {
		return nil, err
	}

	logger.Infof("servers wants to be punched, got start chat d request for %s with code %s",
		startChatDReq.Nickname, startChatDReq.PunchCode)
	idx := slices.IndexFunc(cliCtx.initiations, func(i InitiationInfo) bool {
		return i.otherSideNick == startChatDReq.Nickname
	})

	if idx == -1 {
		logger.Error("there is no initation related to chatstartd's nickname, ignoring",
			"nickname", startChatDReq.Nickname)
		return nil, nil
	}

	conn, err := net.Dial("udp", cliCtx.settings.UdpAddr)
	if err != nil {
		logger.Error("error udp dialing for punch", err)
		return nil, nil
	}

	cliCtx.initiations[idx].punchLaddrUsed = conn.LocalAddr().String()
	enc := json.NewEncoder(conn)
	err = enc.Encode(cm.PunchRequest{PunchCode: startChatDReq.PunchCode})

	if err != nil {
		logger.Error("error sending punch request data", "err", err)
		return nil, nil
	}

	logger.Debug("punch request sent!")
	conn.Close()
	logger.Debug("UDP 'connection' closed", "laddr", conn.LocalAddr())

	return nil, nil
}

func (ctx *Context) handleChatStartFinish(reqFrame common.RFrame) (res common.Response, err error) {
	startChatFinishReq, err := common.RequestFromFrame[common.StartChatFinishRequest](reqFrame)

	if err != nil {
		return nil, err
	}

	logger.Info("got chat finish info!",
		"nick", startChatFinishReq.OtherSideNickname,
		"addr", startChatFinishReq.OtherSideAddress)

	ctx.initiationsLock.RLock()
	defer ctx.initiationsLock.RUnlock()

	idx := slices.IndexFunc(ctx.initiations, func(i InitiationInfo) bool {
		return i.otherSideNick == startChatFinishReq.OtherSideNickname
	})
	relatedInitiation := ctx.initiations[idx]

	logger.Debug("punch laddr used", "laddr", relatedInitiation.punchLaddrUsed)

	logger.Debug("resolving udp addrs")
	laddr, err := net.ResolveUDPAddr("udp", relatedInitiation.punchLaddrUsed)
	if err != nil {
		logger.Error(err)
		return nil, nil
	}
	logger.Debug("resolved laddr", "laddr", laddr.String())

	raddr, err := net.ResolveUDPAddr("udp", startChatFinishReq.OtherSideAddress)
	if err != nil {
		logger.Error(err)
		return nil, nil
	}
	logger.Debug("resolved raddr", "raddr", raddr.String())
	logger.Debug("dialing udp")
	conn, err := net.DialUDP("udp", laddr, raddr)
	if err != nil {
		logger.Error(err)
		return nil, nil
	}
	log.Debugf("dialed udp L%s<->R%s", laddr.String(), raddr.String())

	for i := 0; i < 3; i++ {
		log.Debugf("writing Hello to other peer")
		_, err = conn.Write([]byte("Hello"))
		if err != nil {
			logger.Error(err)
			return nil, nil
		}

		log.Debug("waiting for message from other peer")
		bb := make([]byte, 1024)
		//conn.SetDeadline(time.Now().Add(time.Second * 5))
		n, _, err := conn.ReadFrom(bb)
		if err != nil {
			logger.Error(err)
			return nil, nil
		}
		bb = bb[:n]
		logger.Debug("got info from other peer", "info", string(bb))
		time.Sleep(time.Second)
	}

	return nil, nil
}

func (cliCtx *Context) serverWriter(syncCtx context.Context) error {
	defer logger.Debug("server writer last line...")

	for {
		logger.Debug("waiting for a frame to write")
		select {
		case <-syncCtx.Done():
			return nil
		case frameToWrite := <-cliCtx.rToServer:
			err := cliCtx.conn.WriteJSON(frameToWrite)
			if err != nil {
				return err
			}
			logger.Debug("frame written", "id", frameToWrite.ID)
		}
	}
}

func (cliCtx *Context) serverReader(syncCtx context.Context) error {
	defer logger.Debug("server reader last line...")

	for {
		logger.Debug("waiting for a frame to read")
		var rFrame cm.RFrame
		err := cliCtx.conn.ReadJSON(&rFrame)
		if err != nil {
			return err
		}

		logger.Debug("frame read", "id", rFrame.ID)

		if rFrame.IsResponse() {
			cliCtx.resFromServer <- rFrame
		} else {
			cliCtx.reqFromServer <- rFrame
		}

		logger.Debug("frame pushed", "id", rFrame.ID)
	}
}

func (cliCtx *Context) sendRequest(req cm.Request) error {
	rf, err := cm.RequestFrameFrom(req)
	if err != nil {
		return err
	}

	cliCtx.rToServer <- rf
	return nil
}

func (cliCtx *Context) getResponseFrame() cm.RFrame {
	return <-cliCtx.resFromServer
}

var logger = log.NewWithOptions(os.Stdout, log.Options{
	ReportTimestamp: true,
	TimeFormat:      time.TimeOnly,
	Prefix:          "👤 Client",
})

func init() {
	if cm.IsProd {
		logger.SetLevel(log.InfoLevel)
	} else {
		logger.SetLevel(log.DebugLevel)
	}
}

func sendAuth(ctx *Context, nick, pass string) {
	logger.Info("trying to authenticate...", "nick", nick)
	err := ctx.sendRequest(cm.AuthRequest{Nickname: nick, Password: pass})

	if err != nil {
		logger.Error(err)
		return
	}

	logger.Debug("request sent, waiting for response...")
	arf := ctx.getResponseFrame()
	ar, err := cm.ResponseFromFrame[cm.AuthResponse](arf)

	if err != nil {
		logger.Error(err)
		return
	}

	logger.Infof("Authenticated?: %t", ar.IsSuccess)
}

func sendEcho(ctx *Context, echoByte byte) {
	logger.Info("testing echo...", "echoByte", echoByte)
	err := ctx.sendRequest(cm.EchoRequest{EchoByte: echoByte})

	if err != nil {
		logger.Error(err)
		return
	}

	logger.Debug("request sent, waiting for response...")
	ereqf := ctx.getResponseFrame()
	ereq, err := cm.ResponseFromFrame[cm.EchoResponse](ereqf)

	if err != nil {
		logger.Error(err)
		return
	}

	logger.Info("got response", "echoByte", ereq.EchoByte)
}

func sendListPeers(ctx *Context) {
	logger.Info("trying to get list of peers...")
	err := ctx.sendRequest(cm.ListPeersRequest{})

	if err != nil {
		logger.Error(err)
		return
	}

	logger.Debug("request sent, waiting for response...")
	lpreqf := ctx.getResponseFrame()
	lpreq, err := cm.ResponseFromFrame[cm.ListPeersResponse](lpreqf)

	if err != nil {
		logger.Error(err)
		return
	}

	logger.Info("Got that list", "peersList", lpreq.PeersInfo)
}

func sendStartChatA(ctx *Context, nick string) {
	logger.Info("doing chat start A...")
	err := ctx.sendRequest(cm.StartChatARequest{Nickname: nick})

	if err != nil {
		logger.Error(err)
		return
	}

	logger.Debug("request sent, no wait for response")
}

func sendStartChatC(ctx *Context, nick string) {
	ctx.initiationsLock.Lock()
	defer ctx.initiationsLock.Unlock()

	idx := slices.IndexFunc(ctx.initiations, func(i InitiationInfo) bool {
		return i.otherSideNick == nick
	})

	if idx == -1 {
		logger.Warn("user of that nick did not initiate connection, ignoring")
		return
	}

	err := ctx.sendRequest(cm.StartChatCRequest{Nickname: nick})

	if err != nil {
		logger.Error(err)
		return
	}

	logger.Debug("request sent, no wait for response")
}

func RunClient(settings common.ClientSettings) {
	u := url.URL{Scheme: "ws", Host: settings.WsapiAddr, Path: "/wsapi"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)

	if err != nil {
		logger.Error("could not connect to websocket")
		return
	}

	cliCtx := NewClientContext(c, settings)
	errGroup, syncCtx := errgroup.WithContext(context.Background())

	errGroup.Go(func() error {
		return cliCtx.serverHandler(syncCtx)
	})

	errGroup.Go(func() error {
		return cliCtx.serverReader(syncCtx)
	})

	errGroup.Go(func() error {
		return cliCtx.serverWriter(syncCtx)
	})

	errGroup.Go(func() error {
		<-syncCtx.Done()
		logger.Info("closing client...")
		time.Sleep(time.Second * 3)
		close(cliCtx.rToServer)
		close(cliCtx.resFromServer)
		close(cliCtx.reqFromServer)
		_ = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		return c.Close()
	})

	closer := make(chan int)
	errGroup.Go(func() error {
		select {
		case <-closer:
			return errors.New("close")
		case <-syncCtx.Done():
			return nil
		}
	})

	go func() {
		scanner := bufio.NewScanner(os.Stdin)

		for scanner.Scan() {
			cmd := strings.TrimRight(scanner.Text(), " \n\t")
			cmdElements := strings.Split(cmd, " ")
			cmdName := cmdElements[0]
			cmdArgs := cmdElements[1:]

			if cmdName == "exit" {
				logger.Info("closing...")
				closer <- 1
			} else if cmdName == "echo" {
				if len(cmdArgs) != 1 {
					logger.Errorf("echo command requires 1 argument, but %d was provided", len(cmdArgs))
					continue
				}

				num, err := strconv.Atoi(cmdArgs[0])

				if err != nil {
					logger.Errorf("%s is not a number", cmdArgs[0])
					continue
				}

				sendEcho(cliCtx, byte(num))
			} else if cmdName == "list" {
				sendListPeers(cliCtx)
			} else if cmdName == "auth" {
				if len(cmdArgs) != 2 {
					logger.Errorf("auth command requires 2 argument, but %d was provided", len(cmdArgs))
					continue
				}

				nick := cmdArgs[0]
				pass := cmdArgs[1]

				sendAuth(cliCtx, nick, pass)
			} else if cmdName == "startchata" {
				if len(cmdArgs) != 1 {
					logger.Errorf("startchata command requires 1 argument, but %d was provided", len(cmdArgs))
					continue
				}

				sendStartChatA(cliCtx, cmdArgs[0])

				cliCtx.initiationsLock.Lock()
				cliCtx.initiations = append(cliCtx.initiations, InitiationInfo{
					otherSideNick: cmdArgs[0],
				})
				cliCtx.initiationsLock.Unlock()
			} else if cmdName == "initations" {
				logger.Info("displaying all initations...")

				cliCtx.initiationsLock.RLock()
				for _, i := range cliCtx.initiations {
					logger.Debugf("with %+v", i)
				}
				cliCtx.initiationsLock.RUnlock()
			} else if cmdName == "startchatc" {
				if len(cmdArgs) != 1 {
					logger.Errorf("startchatc command requires 1 argument, but %d was provided", len(cmdArgs))
					continue
				}

				sendStartChatC(cliCtx, cmdArgs[0])
			}
		}
	}()

	err = errGroup.Wait()

	if err != nil {
		logger.Error(err)
	}
}
