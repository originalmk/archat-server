package client

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"time"

	cm "krzyzanowski.dev/p2pchat/common"
)

type ClientContext struct {
	reader *bufio.Reader
	writer *bufio.Writer
}

func perform[T cm.Request, U cm.Response](cliCtx *ClientContext, request T) (U, error) {
	reqJsonBytes, err := json.Marshal(request)

	if err != nil {
		return *new(U), err
	}

	reqBytes := make([]byte, 0)
	reqBytes = append(reqBytes, request.GetRID())
	reqBytes = append(reqBytes, reqJsonBytes...)
	reqBytes = append(reqBytes, '\n')

	_, err = cliCtx.writer.Write(reqBytes)

	if err != nil {
		return *new(U), err
	}

	err = cliCtx.writer.Flush()

	if err != nil {
		return *new(U), err
	}

	resBytes, err := cliCtx.reader.ReadBytes('\n')

	if err != nil {
		return *new(U), err
	}

	var res U
	json.Unmarshal(resBytes, &res)
	return res, nil
}

func RunClient() {
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
	cliCtx := &ClientContext{br, bw}

	log.Println("[Client] connected to server")
	time.Sleep(time.Second * 1)

	echoRes, err := perform[cm.EchoRequest, cm.EchoResponse](cliCtx, cm.EchoRequest{EchoByte: 5})

	if err != nil {
		log.Fatalln("[Client] error performing echo")
	}

	log.Printf("[Client] echo sent (5), got %d\n", echoRes)

	authRes, _ := perform[cm.AuthRequest, cm.AuthResponse](cliCtx, cm.AuthRequest{Nickname: "maciek", Password: "9maciek1"})
	log.Printf("[Client] authenticated: %t\n", authRes.IsSuccess)

	listRes, _ := perform[cm.ListPeersRequest, cm.ListPeersResponse](cliCtx, cm.ListPeersRequest{})
	log.Println("[Client] printing all peers:")

	for _, peer := range listRes.PeersInfo {
		log.Printf("[Client] Peer#%d from %s, hasNick: %t, nick: %s", peer.ID, peer.Addr, peer.HasNickaname, peer.Nickname)
	}

	time.Sleep(time.Second * 10)
}
