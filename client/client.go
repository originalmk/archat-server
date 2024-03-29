package client

import (
	"math/rand"
	"net/url"
	"os"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"
	cm "krzyzanowski.dev/p2pchat/common"
)

type Context struct {
	conn *websocket.Conn
	// Assumption: size of 1 is enough, because first response read will be response for the last request
	// no need to buffer
	resFromServer chan cm.RFrame
	reqFromServer chan cm.RFrame
	rToServer     chan cm.RFrame
}

func NewClientContext(conn *websocket.Conn) *Context {
	return &Context{
		conn:          conn,
		resFromServer: make(chan cm.RFrame),
		reqFromServer: make(chan cm.RFrame),
		rToServer:     make(chan cm.RFrame),
	}
}

func (cliCtx *Context) serverHandler() error {
	for {
		reqFrame := <-cliCtx.reqFromServer

		logger.Debug("got request from server", "id", reqFrame.ID)

		if reqFrame.ID == cm.EchoReqID {
			echoReq, err := cm.RequestFromFrame[cm.EchoRequest](reqFrame)
			if err != nil {
				return err
			}

			resFrame, err := cm.ResponseFrameFrom(cm.EchoResponse(echoReq))
			if err != nil {
				return err
			}

			cliCtx.rToServer <- resFrame
		} else {
			logger.Fatal("can't handle it!")
		}
	}
}

func (cliCtx *Context) serverWriter() error {
	for {
		logger.Debug("waiting for a frame to write")
		frameToWrite := <-cliCtx.rToServer
		err := cliCtx.conn.WriteJSON(frameToWrite)
		if err != nil {
			return err
		}
		logger.Debug("frame written", "id", frameToWrite.ID)
	}
}

func (cliCtx *Context) serverReader() error {
	for {
		logger.Debug("waiting for a frame to read")
		var rFrame cm.RFrame
		err := cliCtx.conn.ReadJSON(&rFrame)
		if err != nil {
			return err
		}

		logger.Debug("frame read", "id", rFrame.ID)

		if rFrame.ID > 128 {
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

func testAuth(ctx *Context) {
	logger.Info("Trying to authenticate as krzmaciek...")
	ctx.sendRequest(cm.AuthRequest{Nickname: "krzmaciek", Password: "9maciek1"})
	logger.Debug("Request sent, waiting for response...")
	arf := ctx.getResponseFrame()
	ar, err := cm.ResponseFromFrame[cm.AuthResponse](arf)
	if err != nil {
		logger.Error(err)
	}
	logger.Infof("Authenticated?: %t", ar.IsSuccess)
}

func testEcho(ctx *Context) {
	echoByte := rand.Intn(32)
	logger.Info("Testing echo...", "echoByte", echoByte)
	ctx.sendRequest(cm.EchoRequest{EchoByte: byte(echoByte)})
	logger.Debug("Request sent, waiting for response...")
	ereqf := ctx.getResponseFrame()
	ereq, err := cm.ResponseFromFrame[cm.EchoResponse](ereqf)
	if err != nil {
		logger.Error(err)
	}
	logger.Info("Got response", "echoByte", ereq.EchoByte)
}

func testListPeers(ctx *Context) {
	logger.Info("Trying to get list of peers...")
	ctx.sendRequest(cm.ListPeersRequest{})
	logger.Debug("Request sent, waiting for response...")
	lpreqf := ctx.getResponseFrame()
	lpreq, err := cm.ResponseFromFrame[cm.ListPeersResponse](lpreqf)
	if err != nil {
		logger.Error(err)
	}
	logger.Info("Got that list", "peersList", lpreq.PeersInfo)
}

func RunClient() {
	u := url.URL{Scheme: "ws", Host: ":8080", Path: "/wsapi"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)

	if err != nil {
		logger.Error("could not connect to websocket")
		return
	}

	defer c.Close()

	ctx := NewClientContext(c)
	go ctx.serverHandler()
	go ctx.serverReader()
	go ctx.serverWriter()

	testAuth(ctx)
	testEcho(ctx)
	testListPeers(ctx)

	time.Sleep(time.Second * 5)
	logger.Info("closing connection...")
	c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}
