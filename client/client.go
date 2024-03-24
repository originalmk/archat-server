package client

import (
	"net/url"
	"os"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"
	cm "krzyzanowski.dev/p2pchat/common"
)

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

func RunClient() {
	u := url.URL{Scheme: "ws", Host: ":8080", Path: "/wsapi"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)

	if err != nil {
		logger.Error("could not connect to websocket")
		return
	}

	defer c.Close()

	logger.Info("authenticating...")
	rf, _ := cm.RequestFrameFrom(cm.AuthRequest{Nickname: "krzmaciek", Password: "9maciek1"})
	err = c.WriteJSON(rf)
	if err != nil {
		logger.Fatal(err)
	}

	var authResFrame cm.ResponseFrame
	err = c.ReadJSON(&authResFrame)
	if err != nil {
		logger.Fatal(err)
	}

	authRes, err := cm.ResponseFromFrame[cm.AuthResponse](authResFrame)
	if err != nil {
		logger.Fatal(err)
	}

	logger.Infof("authentication result: %t", authRes.IsSuccess)
	time.Sleep(time.Second * 1)

	logger.Info("sending echo...")
	echoByte := 123
	rf, err = cm.RequestFrameFrom(cm.EchoRequest{EchoByte: byte(echoByte)})
	if err != nil {
		logger.Fatal(err)
	}

	err = c.WriteJSON(rf)
	if err != nil {
		logger.Fatal(err)
	}

	var echoResFrame cm.ResponseFrame
	err = c.ReadJSON(&echoResFrame)
	if err != nil {
		logger.Fatal(err)
	}

	echoRes, err := cm.ResponseFromFrame[cm.EchoResponse](echoResFrame)
	if err != nil {
		logger.Fatal(err)
	}

	logger.Infof("sent echo of %d, got %d in return", echoByte, echoRes.EchoByte)
	time.Sleep(time.Second)

	logger.Infof("i want list of peers...")
	rf, err = cm.RequestFrameFrom(cm.ListPeersRequest{})
	if err != nil {
		logger.Fatal(err)
	}

	err = c.WriteJSON(rf)
	if err != nil {
		logger.Fatal(err)
	}

	var listPeersResFrame cm.ResponseFrame
	err = c.ReadJSON(&listPeersResFrame)
	if err != nil {
		logger.Fatal(err)
	}

	listPeersRes, err := cm.ResponseFromFrame[cm.ListPeersResponse](listPeersResFrame)
	if err != nil {
		logger.Fatal(err)
	}

	logger.Info("printing list of peers:")

	for _, p := range listPeersRes.PeersInfo {
		logger.Infof("%+v", p)
	}

	time.Sleep(time.Second * 5)
	logger.Info("closing connection...")
	c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}
