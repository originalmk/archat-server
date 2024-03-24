package common

import (
	"encoding/json"
)

// Constants

const (
	EchoRID      = 1
	ListPeersRID = 2
	AuthRID      = 3
)

// Requests & responses subtypes

type PeerInfo struct {
	ID           int    `json:"id"`
	Addr         string `json:"addr"`
	HasNickaname bool   `json:"hasNickname"`
	Nickname     string `json:"nickname"`
}

// Requests & responses:

type RequestFrame struct {
	ID   int             `json:"id"`
	Rest json.RawMessage `json:"request"`
}

func RequestFrameFrom(req Request) (RequestFrame, error) {
	jsonBytes, err := json.Marshal(req)

	if err != nil {
		return *new(RequestFrame), err
	}

	return RequestFrame{req.GetRID(), jsonBytes}, nil
}

func RequestFromFrame[T Request](reqFrame RequestFrame) (T, error) {
	var req T
	err := json.Unmarshal(reqFrame.Rest, &req)

	if err != nil {
		return *new(T), err
	}

	return req, nil
}

type ResponseFrame struct {
	ID   int             `json:"id"`
	Rest json.RawMessage `json:"response"`
}

func ResponseFrameFrom(res Response) (ResponseFrame, error) {
	jsonBytes, err := json.Marshal(res)

	if err != nil {
		return *new(ResponseFrame), err
	}

	return ResponseFrame{res.GetRID(), jsonBytes}, nil
}

func ResponseFromFrame[T Response](resFrame ResponseFrame) (T, error) {
	var res T
	err := json.Unmarshal(resFrame.Rest, &res)

	if err != nil {
		return *new(T), err
	}

	return res, nil
}

type Request interface {
	GetRID() int
}

type Response Request

type EchoRequest struct {
	EchoByte byte `json:"echoByte"`
}

func (EchoRequest) GetRID() int {
	return EchoRID
}

type EchoResponse struct {
	EchoByte byte `json:"echoByte"`
}

func (EchoResponse) GetRID() int {
	return EchoRID
}

type ListPeersRequest struct {
}

func (ListPeersRequest) GetRID() int {
	return ListPeersRID
}

type ListPeersResponse struct {
	PeersInfo []PeerInfo `json:"peers"`
}

func (ListPeersResponse) GetRID() int {
	return ListPeersRID
}

type AuthRequest struct {
	Nickname string `json:"nickname"`
	Password string `json:"password"`
}

func (req AuthRequest) MarshalJSON() ([]byte, error) {
	type Alias AuthRequest
	return json.Marshal(&struct {
		ID int `json:"id"`
		Alias
	}{
		AuthRID,
		Alias(req),
	})
}

func (AuthRequest) GetRID() int {
	return AuthRID
}

type AuthResponse struct {
	IsSuccess bool
}

func (AuthResponse) GetRID() int {
	return AuthRID
}
