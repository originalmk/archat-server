package common

type Request interface {
	GetRID() byte
}

type Response Request

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
	EchoRID      = 1
	ListPeersRID = 2
	AuthRID      = 3
)

func (EchoRequest) GetRID() byte {
	return EchoRID
}

func (EchoResponse) GetRID() byte {
	return EchoRID
}

func (AuthRequest) GetRID() byte {
	return AuthRID
}

func (AuthResponse) GetRID() byte {
	return AuthRID
}

func (ListPeersRequest) GetRID() byte {
	return ListPeersRID
}

func (ListPeersResponse) GetRID() byte {
	return ListPeersRID
}
