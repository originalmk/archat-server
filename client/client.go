package client

import (
	"log"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	cm "krzyzanowski.dev/p2pchat/common"
)

func RunClient() {
	u := url.URL{Scheme: "ws", Host: ":8080", Path: "/wsapi"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)

	if err != nil {
		log.Println("[Client] could not connect to websocket")
		return
	}

	defer c.Close()

	log.Println("[Client] authenticating...")
	rf, _ := cm.RequestFrameFrom(cm.AuthRequest{Nickname: "krzmaciek", Password: "9maciek1"})
	err = c.WriteJSON(rf)
	if err != nil {
		log.Fatalln(err)
	}

	var authResFrame cm.ResponseFrame
	err = c.ReadJSON(&authResFrame)
	if err != nil {
		log.Fatalln(err)
	}

	authRes, err := cm.ResponseFromFrame[cm.AuthResponse](authResFrame)
	if err != nil {
		log.Fatalln(err)
	}

	log.Printf("[Client] authentication result: %t\n", authRes.IsSuccess)
	time.Sleep(time.Second * 1)

	log.Println("[Client] sending echo...")
	echoByte := 123
	rf, err = cm.RequestFrameFrom(cm.EchoRequest{EchoByte: byte(echoByte)})
	if err != nil {
		log.Fatalln(err)
	}

	err = c.WriteJSON(rf)
	if err != nil {
		log.Fatalln(err)
	}

	var echoResFrame cm.ResponseFrame
	err = c.ReadJSON(&echoResFrame)
	if err != nil {
		log.Fatalln(err)
	}

	echoRes, err := cm.ResponseFromFrame[cm.EchoResponse](echoResFrame)
	if err != nil {
		log.Fatalln(err)
	}

	log.Printf("[Client] sent echo of %d, got %d in return\n", echoByte, echoRes.EchoByte)
	time.Sleep(time.Second)

	log.Println("[Client] i want list of peers...")
	rf, err = cm.RequestFrameFrom(cm.ListPeersRequest{})
	if err != nil {
		log.Fatalln(err)
	}

	err = c.WriteJSON(rf)
	if err != nil {
		log.Fatalln(err)
	}

	var listPeersResFrame cm.ResponseFrame
	err = c.ReadJSON(&listPeersResFrame)
	if err != nil {
		log.Fatalln(err)
	}

	listPeersRes, err := cm.ResponseFromFrame[cm.ListPeersResponse](listPeersResFrame)
	if err != nil {
		log.Fatalln(err)
	}

	log.Println("[Client] printing list of peers:")

	for _, p := range listPeersRes.PeersInfo {
		log.Printf("[Client] %+v\n", p)
	}

	time.Sleep(time.Second * 5)
	c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}
