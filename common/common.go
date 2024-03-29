package common

import (
	"encoding/json"
)

// Constants

const (
	EchoReqID      = 1
	EchoResID      = 128 + EchoReqID
	ListPeersReqID = 2
	ListPeersResID = 128 + ListPeersReqID
	AuthReqID      = 3
	AuthResID      = 128 + AuthReqID
)

// Requests & responses subtypes

type PeerInfo struct {
	ID           int    `json:"id"`
	Addr         string `json:"addr"`
	HasNickaname bool   `json:"hasNickname"`
	Nickname     string `json:"nickname"`
}

// Requests & responses:

type RFrame struct {
	ID   int             `json:"id"`
	Rest json.RawMessage `json:"r"`
}

func RequestFrameFrom(req Request) (RFrame, error) {
	jsonBytes, err := json.Marshal(req)

	if err != nil {
		return *new(RFrame), err
	}

	return RFrame{req.ID(), jsonBytes}, nil
}

func RequestFromFrame[T Request](reqFrame RFrame) (T, error) {
	var req T
	err := json.Unmarshal(reqFrame.Rest, &req)

	if err != nil {
		return *new(T), err
	}

	return req, nil
}

func ResponseFrameFrom(res Response) (RFrame, error) {
	jsonBytes, err := json.Marshal(res)

	if err != nil {
		return *new(RFrame), err
	}

	return RFrame{res.ID(), jsonBytes}, nil
}

func ResponseFromFrame[T Response](resFrame RFrame) (T, error) {
	var res T
	err := json.Unmarshal(resFrame.Rest, &res)

	if err != nil {
		return *new(T), err
	}

	return res, nil
}

type Request interface {
	ID() int
}

type Response Request

type EchoRequest struct {
	EchoByte byte `json:"echoByte"`
}

func (EchoRequest) ID() int {
	return EchoReqID
}

type EchoResponse struct {
	EchoByte byte `json:"echoByte"`
}

func (EchoResponse) ID() int {
	return EchoResID
}

type ListPeersRequest struct {
}

func (ListPeersRequest) ID() int {
	return ListPeersReqID
}

type ListPeersResponse struct {
	PeersInfo []PeerInfo `json:"peers"`
}

func (ListPeersResponse) ID() int {
	return ListPeersResID
}

type AuthRequest struct {
	Nickname string `json:"nickname"`
	Password string `json:"password"`
}

func (AuthRequest) ID() int {
	return AuthReqID
}

type AuthResponse struct {
	IsSuccess bool
}

func (AuthResponse) ID() int {
	return AuthResID
}
