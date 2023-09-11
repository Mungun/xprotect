package reader

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"k8s.io/klog/v2"

	conf "github.com/mungun/xprotect/internal/config"
)

const (
	COMMAND_TOKEN_UPDATE = "TOKEN_UPDATE"
	COMMAND_START_LIVE   = "START_LIVE"
	COMMAND_STOP_LIVE    = "STOP_LIVE"
	RESULT_ERROR         = "ERROR"
	RESULT_IMAGE         = "IMAGE"
)

var END = []byte{13, 10, 13, 10}

type StreamCommand struct {
	Type      string
	CorpToken *conf.TokenResult
	ServerUrl string
	CameraId  string
}

type StreamResult struct {
	Type    string
	Message string
	Image   *ImageInfo
	Live    bool
}

type ImageInfo struct {
	Length    int
	Type      string
	Current   string
	Next      string
	Prev      string
	Data      []byte
	KeyFrame  bool
	Timestamp int64
}

type ReaderHandler struct {
	CorpToken  *conf.TokenResult
	Live       bool
	cancelFunc context.CancelFunc
}

func NewReaderHandler() *ReaderHandler {
	return &ReaderHandler{
		Live: false,
	}
}

func (s *ReaderHandler) Start(ctx context.Context, chanCommand chan *StreamCommand, chanResult chan *StreamResult) {
	klog.Infof("ReaderHandler started\n")

	wg := &sync.WaitGroup{}

	chanCorpToken := make(chan *conf.TokenResult, 1)
	procOutChannel := make(chan *StreamResult, 1)
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("Context cancelled\n")
			wg.Wait()
			if s.cancelFunc != nil {
				s.cancelFunc()
			}
			return
		case cmd := <-chanCommand:
			klog.Infof("cmd from chanCommand %v\n", cmd)
			if cmd.Type == COMMAND_TOKEN_UPDATE {
				s.CorpToken = cmd.CorpToken
				if s.Live {
					chanCorpToken <- cmd.CorpToken
				}

			} else if cmd.Type == COMMAND_START_LIVE {
				if s.Live {
					chanResult <- &StreamResult{
						Type:    RESULT_ERROR,
						Message: "Has active stream!",
					}
				} else {
					s.CorpToken = cmd.CorpToken // set token if null
					ctx, cancelFunc := context.WithCancel(context.Background())
					s.cancelFunc = cancelFunc
					wg.Add(1)
					go processStream(ctx, wg, cmd, chanCorpToken, procOutChannel)
				}
			} else if cmd.Type == COMMAND_STOP_LIVE {
				if s.cancelFunc != nil {
					s.cancelFunc()
					s.cancelFunc = nil
				} else {
					chanResult <- &StreamResult{
						Type:    RESULT_ERROR,
						Message: "No active stream!",
					}
				}
			}
		case result := <-procOutChannel:
			s.Live = result.Live
			chanResult <- result
		}
	}
}

func FormatConnect(requestId int, cameraId string, token string) []byte {
	message := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"utf-8\"?>"+
		"<methodcall>"+
		"<requestid>%d</requestid>"+
		"<methodname>connect</methodname>"+
		"<username>a</username>"+
		"<password>a</password>"+
		"<cameraid>%s</cameraid>"+
		"<alwaysstdjpeg>yes</alwaysstdjpeg>"+
		"<connectparam>id=%s&amp;connectiontoken=%s</connectparam>"+
		"</methodcall>", requestId, cameraId, cameraId, token)

	return append([]byte(message), END...)
}

func FormatLive(requestId int, quality int) []byte {
	message := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"utf-8\"?>"+
		"<methodcall><requestid>%d</requestid>"+
		"<methodname>live</methodname>"+
		"<compressionrate>%d</compressionrate>"+
		"</methodcall>", requestId, quality)
	return append([]byte(message), END...)
}

func FormatConnectUpdate(requestId int, cameraId string, token string) []byte {
	message := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"utf-8\"?>"+
		"<methodcall><requestid>%d</requestid>"+
		"<methodname>connectupdate</methodname>"+
		"<connectparam>id=%d&amp;connectiontoken=%s</connectparam>"+
		"</methodcall>", requestId, cameraId, token)
	return append([]byte(message), END...)
}

func FormatStop(requestId int) []byte {
	message := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"utf-8\"?>"+
		"<methodcall><requestid>%d</requestid>"+
		"<methodname>stop</methodname>"+
		"</methodcall>", requestId)
	return append([]byte(message), END...)
}

func RecvUntil(conn net.Conn, buf []byte, offset int, size int) (int, error) {
	var miss = size
	var got = 0

	var ended = 4
	var i = 0

	for got < size && ended > 0 {
		i = offset + got
		b := make([]byte, 1)
		bytes, err := conn.Read(b)
		if err == nil {
			if b[0] == '\r' || b[0] == '\n' {
				ended--
			} else {
				ended = 4
			}
			buf[i] = b[0]

			got += bytes
			miss -= bytes
		} else {
			return got, err
		}

	}

	if got > size {
		return 0, fmt.Errorf("Buffer overflow")
	}

	if ended == 0 {
		return got, nil
	}

	return -got, nil
}

func RecvFixed(conn net.Conn, buf []byte, offset int, size int) (int, error) {
	var miss = size
	var got = 0
	var bytes = 0
	var get = 1
	var maxb = 1024 * 16
	var err error

	for ok := true; ok; {
		if miss > maxb {
			get = maxb
		} else {
			get = miss
		}

		b := make([]byte, get)
		bytes, err = conn.Read(b)
		if err == nil {
			i := 0
			for i < bytes {
				buf[offset+got+i] = b[i]
				i++
			}
			got += bytes
			miss -= bytes
		} else {
			return got, err
		}

		ok = got < size
	}

	if got > size {
		return 0, fmt.Errorf("Buffer overflow")
	}

	if size < 4 {
		return -got, nil
	}

	i := offset + got - 4
	if buf[i] == '\r' && buf[i+1] == '\n' && buf[i+2] == '\r' && buf[i+3] == '\n' {
		return got, nil
	}

	return -got, nil

}

var regConnected = regexp.MustCompile(`<connected>(?P<auth>.*)</connected>`)

func isConnected(page string) bool {
	matched := regConnected.FindStringSubmatch(page) // left most
	if matched == nil {
		return false
	}
	auth := matched[regConnected.SubexpIndex("auth")]

	return strings.ToLower(auth) == "yes"
}

func ParseHeader(buf []byte, offset int, bytes int) ImageInfo {
	h := ImageInfo{
		Length: 0,
		Type:   "",
	}
	response := string(buf[offset:bytes])
	headers := strings.Split(response, "\n")

	for _, header := range headers {
		keyVal := strings.Split(header, ":")

		if strings.ToLower(keyVal[0]) == "content-length" && len(keyVal) > 1 {
			i, err := strconv.ParseInt(keyVal[1], 10, 32)
			if err != nil {
				fmt.Printf("parse length error: %v\n", err)
			} else {
				h.Length = int(i)
			}

		}

		if strings.ToLower(keyVal[0]) == "content-type" && len(keyVal) > 1 {
			h.Type = strings.ToLower(strings.Trim(keyVal[1], "\r"))
		}

		if strings.ToLower(keyVal[0]) == "current" && len(keyVal) > 1 {
			h.Current = strings.Trim(keyVal[1], "\r")
		}

		if strings.ToLower(keyVal[0]) == "next" && len(keyVal) > 1 {
			h.Next = strings.Trim(keyVal[1], "\r")
		}

		if strings.ToLower(keyVal[0]) == "prev" && len(keyVal) > 1 {
			h.Prev = strings.Trim(keyVal[1], "\r")
		}

	}

	return h
}

func RoundUpBufSize(needed int) int {
	roundup := (needed / 1024) * 1024 / 100 * 130
	return roundup
}

func Reverse(slice []byte) []byte {
	for i, j := 0, len(slice)-1; i < j; i, j = i+1, j-1 {
		slice[i], slice[j] = slice[j], slice[i] //reverse the slice
	}

	return slice
}

func Abs(number int) int {
	if number >= 0 {
		return number
	}
	return -number
}

func processImage(conn net.Conn, bytesReceived []byte, maxBuf int) (*ImageInfo, error) {
	curBufSize := maxBuf

	bytes, err := RecvUntil(conn, bytesReceived, 0, maxBuf)
	if err != nil {
		// fmt.Printf("Receive error: %v\n", err)
		return nil, err
	} else if bytes < 0 {
		// fmt.Printf("Receive error A\n")
		return nil, fmt.Errorf("receive error A")
	}

	if bytesReceived[0] == '<' {
		// This is XML status message
		page := string(bytesReceived[0:bytes])
		fmt.Printf("document status: %s\n", page)
		return nil, fmt.Errorf("status message: %s", page)
	}

	if bytesReceived[0] == 'I' {
		// Image
		h := ParseHeader(bytesReceived, 0, bytes)

		// Takes two first bytes
		bytes, err = RecvFixed(conn, bytesReceived, 0, 2)
		if err != nil {
			// fmt.Printf("RecvFixed err: %v\n", err)
			return nil, err
		} else if Abs(bytes) != 2 {
			// fmt.Printf("Receive error 2\n")
			return nil, fmt.Errorf("receive error 2")
		}

		if bytesReceived[0] == 0xFF && bytesReceived[1] == 0xD8 {
			neededBufSize := h.Length
			if neededBufSize > curBufSize {
				newBufSize := RoundUpBufSize(neededBufSize)
				curBufSize = newBufSize
				b0 := bytesReceived[0]
				b1 := bytesReceived[1]
				bytesReceived = make([]byte, curBufSize)
				bytesReceived[0] = b0
				bytesReceived[1] = b1
			}

			bytes, err = RecvFixed(conn, bytesReceived, 2, neededBufSize-2)
			if err != nil {
				// fmt.Printf("RecvFixed err: %v\n", err)
				return nil, err
			}
			bytes = Abs(bytes)
			if bytes != neededBufSize-2 {
				// fmt.Printf("Receive error B\n")
				return nil, fmt.Errorf("receive error B")
			}
		} else {
			bytes, err = RecvFixed(conn, bytesReceived, 2, 30)
			if err != nil {
				// fmt.Printf("RecvFixed err: %v\n", err)
				return nil, err
			} else if Abs(bytes) != 30 {
				// fmt.Printf("Receive error C\n")
				return nil, fmt.Errorf("receive error C")
			}

			// dataType := binary.BigEndian.Uint16(Reverse(bytesReceived[0:2]))
			// length := binary.BigEndian.Uint32(Reverse(bytesReceived[2:6]))
			// codec := binary.BigEndian.Uint16(Reverse(bytesReceived[6:8]))
			// seqNo := binary.BigEndian.Uint16(Reverse(bytesReceived[8:10]))
			// flags := binary.BigEndian.Uint16(Reverse(bytesReceived[10:12]))
			// timeStampSync := binary.BigEndian.Uint64(Reverse(bytesReceived[12:20]))
			// timeStampPicture := binary.BigEndian.Uint64(Reverse(bytesReceived[20:28]))
			// reserved := binary.BigEndian.Uint32(Reverse(bytesReceived[28:32]))

			// dataType := binary.BigEndian.Uint16(bytesReceived[0:2])
			length := binary.BigEndian.Uint32(bytesReceived[2:6])
			// codec := binary.BigEndian.Uint16(bytesReceived[6:8])
			// seqNo := binary.BigEndian.Uint16(bytesReceived[8:10])
			flags := binary.BigEndian.Uint16(bytesReceived[10:12])
			// timeStampSync := binary.BigEndian.Uint64(bytesReceived[12:20])
			timeStampPicture := binary.BigEndian.Uint64(bytesReceived[20:28])
			// reserved := binary.BigEndian.Uint32(bytesReceived[28:32])

			isKeyFrame := (flags & 0x01) == 0x01
			payloadLength := int(length) - 32

			if payloadLength > curBufSize {
				newBufSize := RoundUpBufSize(payloadLength)
				curBufSize = newBufSize
				bytesReceived = make([]byte, curBufSize)
			}

			//this appears to be the correct amount of data
			bytes, err = RecvFixed(conn, bytesReceived, 0, payloadLength)

			if err != nil {
				// fmt.Printf("RecvFixed err: %v\n", err)
				return nil, err
			}

			bytes = Abs(bytes)
			if bytes != payloadLength {
				// fmt.Printf("Receive error D\n")
				return nil, fmt.Errorf("Receiver error D")
			}

			h.KeyFrame = isKeyFrame
			h.Timestamp = int64(timeStampPicture)
		}

		ms := make([]byte, bytes)
		copy(ms, bytesReceived)
		h.Data = ms

		return &h, nil
	}

	return nil, fmt.Errorf("other message")
}

func processStream(ctx context.Context, wg *sync.WaitGroup, cmd *StreamCommand, chanCorpToken chan *conf.TokenResult, chanResult chan *StreamResult) {

	defer wg.Done()

	_currentToken := cmd.CorpToken
	cameraId := cmd.CameraId

	maxBuf := 1024 * 8

	urlParts, err := url.ParseRequestURI(cmd.ServerUrl)
	if err != nil {
		klog.Errorf("Parse image server url %s error: %v\n", cmd.ServerUrl, err)
		chanResult <- &StreamResult{
			Type:    RESULT_ERROR,
			Message: fmt.Sprintf("Parse image server url %s error: %v\n", cmd.ServerUrl, err),
			Live:    false, // goroutine stopped
		}
		return
	}

	var host string
	var port string
	if strings.Index(urlParts.Host, ":") == -1 {
		if urlParts.Scheme == "http" {
			port = "80"
		} else if urlParts.Scheme == "https" {
			port = "443"
		}
		host = net.JoinHostPort(host, port)
	} else {
		host = urlParts.Host
	}

	klog.Infof("host %s\n", host)

	conn, err := net.Dial("tcp", host)
	if err != nil {
		klog.Errorf("Connect to image server %s error: %v\n", cmd.ServerUrl, err)
		chanResult <- &StreamResult{
			Type:    RESULT_ERROR,
			Message: fmt.Sprintf("Connect to image server %s error: %v\n", cmd.ServerUrl, err),
			Live:    false, // goroutine stopped
		}
		return
	}

	defer conn.Close()

	var requestId = 0

	sendBuffer := FormatConnect(requestId, cameraId, _currentToken.Token)
	nBytes, err := conn.Write(sendBuffer)
	if err != nil {
		klog.Errorf("Write connect message to socket error: %v\n", err)
		chanResult <- &StreamResult{
			Type:    RESULT_ERROR,
			Message: fmt.Sprintf("Write connect message to socket error: %v\n", err),
			Live:    false, // goroutine stopped
		}
		return
	}
	klog.Infof("Write %d bytes connect message to socket\n", nBytes)

	bytesReceived := make([]byte, maxBuf)
	bytes, err := RecvUntil(conn, bytesReceived, 0, maxBuf)
	if err != nil {
		klog.Errorf("Receive connection response error: %v\n", err)
		chanResult <- &StreamResult{
			Type:    RESULT_ERROR,
			Message: fmt.Sprintf("Receive connection response error: %v\n", err),
			Live:    false, // goroutine stopped
		}
		return
	}

	page := string(bytesReceived[0:bytes])
	klog.Infof("page %s\n", page)
	if !isConnected(page) {
		klog.Errorf("Unauthenticated\n")
		chanResult <- &StreamResult{
			Type:    RESULT_ERROR,
			Message: fmt.Sprintf("Unauthenticated\n"),
			Live:    false, // goroutine stopped
		}
		return
	}

	// start live
	requestId++
	quality := 75
	sendBuffer = FormatLive(requestId, quality)
	nBytes, err = conn.Write(sendBuffer)
	if err != nil {
		klog.Errorf("Write start live message to socket error: %v\n", err)
		chanResult <- &StreamResult{
			Type:    RESULT_ERROR,
			Message: fmt.Sprintf("Write stare live message to socket error: %v\n", err),
			Live:    false, // goroutine stopped
		}
		return
	}
	klog.Infof("Write %d bytes start live message to socket\n", nBytes)

	for {

		select {
		case <-ctx.Done():
			fmt.Printf("Context cancelled\n")
			requestId++
			sendBuffer = FormatStop(requestId)
			conn.Write(sendBuffer)
			return
		case _t := <-chanCorpToken:
			fmt.Printf("Token updated, will expire :%+v\n", _t.Expiry)
			_currentToken = _t
			requestId++
			sendBuffer = FormatConnectUpdate(requestId, cameraId, _currentToken.Token)
			nBytes, err := conn.Write(sendBuffer)
			if err != nil {
				klog.Errorf("Write token update message to socket error: %v\n", err)
				chanResult <- &StreamResult{
					Type:    RESULT_ERROR,
					Message: fmt.Sprintf("Write token update message to socket error: %v\n", err),
				}
			}

			klog.Infof("Write %d bytes token update message to socket\n", nBytes)
		default:
			image, err := processImage(conn, bytesReceived, maxBuf)
			if err != nil {
				klog.Errorf("processImage error: %v\n", err)
				chanResult <- &StreamResult{
					Type:    RESULT_ERROR,
					Message: fmt.Sprintf("processImage error: %v\n", err),
				}
			} else {
				chanResult <- &StreamResult{
					Type:  RESULT_IMAGE,
					Image: image,
				}
			}
		}
	}

}
