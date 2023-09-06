package web

import(
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"k8s.io/klog/v2"
	conf "github.com/mungun/xprotect/internal/config"
	reader "github.com/mungun/xprotect/internal/reader"
	util "github.com/mungun/xprotect/internal/util"
	streamer "github.com/mungun/xprotect/internal/streamer"
)

type StopFunc func()

type webserver struct {
	config *conf.Config
	mu   sync.RWMutex
	clients []*SSEClient
	// stopChan  chan struct{}
	chanToken chan *oauth2.Token
	token     *oauth2.Token // used for command

	recorders []util.Recorder
	cameras   []util.Camera

	chanReaderCommand chan *reader.StreamCommand

	ChanImageData chan []byte

}

func NewWebserver(chanToken chan *oauth2.Token, chanReaderCommand chan *reader.StreamCommand, config *conf.Config ) *webserver {
	return &webserver{
		config: config,
		clients:   make([]*SSEClient, 0),
		chanToken: chanToken,
		chanReaderCommand: chanReaderCommand,
		ChanImageData: make(chan []byte, 1024),
	}
}


type SSEClient struct {
	name   string
	events chan *sseEvent
}

func (p *webserver) sse(w http.ResponseWriter, req *http.Request) {
	client := &SSEClient{name: req.RemoteAddr, events: make(chan *sseEvent, 10)}

	defer p.removeClient(client)
	go p.addClient(client)

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")


	notify := req.Context().Done()

	for {
		timeout := time.After(1 * time.Second)

		select {
		// case <-p.stopChan:
		// 	klog.Infof("sse receive server close signal\n")
		// 	return
		case <-notify:
			klog.Infof("sse receive client close signal\n")
			return
		case ev := <-client.events:
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.Encode(ev)
			fmt.Fprintf(w, "data: %v\n\n", buf.String())

			

		case <-timeout:
			fmt.Fprintf(w, ": nothing to sent\n\n")
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		
	}

}

func (p *webserver) cameraList(w http.ResponseWriter, req *http.Request) {

	cameras := p.getCameras()

	if cameras == nil {
		cameras = []util.Camera{}
	}
	w.Header().Set("Content-Type", "application/json")
	
	result, err := jsonResp(cameras, func(payload []util.Camera) map[string]interface{} {
		newMap := map[string]interface{}{
			"list": payload,
		}
		return newMap
	})
	if err != nil {
		// http.Error(w, err.Error(), http.StatusInternalServerError)
		sendErrorAsJson(w, err)
		return
	}

	sendJson(w, result)
}

func (p *webserver) recorderList(w http.ResponseWriter, req *http.Request) {

	recorders := p.getRecorders()

	if recorders == nil {
		recorders = []util.Recorder{}
	}
	w.Header().Set("Content-Type", "application/json")
	
	result, err := jsonResp(recorders, func(payload []util.Recorder) map[string]interface{} {
		newMap := map[string]interface{}{
			"list": payload,
		}
		return newMap
	})
	if err != nil {
		// http.Error(w, err.Error(), http.StatusInternalServerError)
		sendErrorAsJson(w, err)
		return
	}

	sendJson(w, result)
}

func sendAsJson(w http.ResponseWriter, payload interface{}) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)	
	// m := make(map[string]interface{})
	// m["list"] = cameras
	// enc.Encode(m)
	enc.Encode(payload)
	fmt.Fprintf(w, buf.String())
}

func sendErrorAsJson(w http.ResponseWriter, err error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)	
	// m := make(map[string]interface{})
	// m["list"] = cameras
	// enc.Encode(m)
	enc.Encode(err.Error())
	fmt.Fprintf(w, buf.String())
}

type FuncResponse[T, V any] func(V) T

func jsonResp[T, V any](in V, fs FuncResponse[T, V]) (string, error) {
	newMap := fs(in)
	result, err := json.Marshal(newMap)
	if err != nil {
		klog.Errorf("jsonResp - Marshal content error %v\n", err)
		return "", err
	}

	klog.V(2).Infof("jsonResp - Marshaled content %v\n", result)
	return string(result), nil
}

func sendJson(w http.ResponseWriter, result string) {
	
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, result)
}

func parseBody[T any](req *http.Request, t *T) error {
	decoder := json.NewDecoder(req.Body)	
	err := decoder.Decode(t)
	if err != nil {
		return err
	}
	return nil
}

type LiveBody struct {
	CameraId string   `json:"cameraId,omitempty"`
	ServerUrl string `json:"serverUrl,omitempty"`
}

func isEmpty(val *string) bool {
	return val == nil || *val == ""
}

func (p *webserver) testSse(w http.ResponseWriter, req *http.Request) {

	w.Header().Set("Content-Type", "application/json")
	
	t := time.Now()
	p.SendSse(reader.RESULT_ERROR, t.UnixMilli(), t.Format("20060102150405000"))
	result, err := jsonResp(t, func(t time.Time) map[string]interface{} {
		newMap := map[string]interface{}{
			"tick": t,
		}
		return newMap
	})
	if err != nil {
		// http.Error(w, err.Error(), http.StatusInternalServerError)
		sendErrorAsJson(w, err)
		return
	}

	sendJson(w, result)
}

func (p *webserver) cameraLive(w http.ResponseWriter, req *http.Request) {
	body := &LiveBody{}
	err := parseBody(req, body)	
	if err != nil {
		klog.Errorf("parseBody err %v\n", err)
		sendErrorAsJson(w, err)		
		return
	}
	
	if isEmpty(&body.CameraId) {
		klog.Errorf("cameraId required\n")
		sendErrorAsJson(w, fmt.Errorf("cameraId required"))
		return
	}

	if isEmpty(&body.ServerUrl) {
		klog.Errorf("serverUrl required\n")
		sendErrorAsJson(w, fmt.Errorf("serverUrl required"))
		return
	}

	token := p.getToken()
	if token == nil {
		klog.Errorf("token is null\n")
		sendErrorAsJson(w, fmt.Errorf("Not connected management server!"))
		return
	}

	p.chanReaderCommand <- &reader.StreamCommand{
		Type:     reader.COMMAND_START_LIVE,
		CameraId: body.CameraId,
		Token:    token,
		ServerUrl: body.ServerUrl,
	}

	sendAsJson(w, map[string]interface{}{
		"cameraId": body.CameraId,
	})
}

func (p *webserver) home(w http.ResponseWriter, req *http.Request) {
	http.ServeFile(w, req, "./public/capture.html")
}

func (p *webserver) addClient(client *SSEClient) {
	klog.Infof("addClient %v", client)
	p.mu.Lock()
	defer p.mu.Unlock()
	if client != nil {
		p.clients = append(p.clients, client)
	}

}

func (p *webserver) removeClient(client *SSEClient) {
	klog.Infof("removeClient %v", client)
	p.mu.Lock()
	defer p.mu.Unlock()
	if client != nil {
		for i, ch := range p.clients {
			if ch == client {
				p.clients[i] = p.clients[len(p.clients)-1]
				p.clients = p.clients[:len(p.clients)-1]
				close(ch.events)
				break
			}
		}
	}
}

func (p *webserver) getClients() []*SSEClient {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.clients
}

func (p *webserver) SendSse(event string, timestamp int64, message string) {
	sse := &sseEvent{
		Type:      event,
		Timestamp: timestamp,
		Message:   message,
	}
	clients := p.getClients()
	for _, ch := range clients {
		ch.events <- sse
	}
}

type sseEvent struct {
	Type      string `json:"type,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
	Message   string `json:"message,omitempty"`
}



func (p *webserver) setRecorders(recorders []util.Recorder) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.recorders = recorders
}

func (p *webserver) getRecorders() []util.Recorder {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.recorders
}

func (p *webserver) listRecorder(token *oauth2.Token) {
	recorders := p.getRecorders()
	if recorders != nil && len(recorders) > 0 {		
		return
	}

	recorders, err := util.ListRecorder(p.config.BaseUrl, token.AccessToken)
	if err != nil {
		klog.Errorf("ListRecorder error %v\n", err)
		p.SendSse(reader.RESULT_ERROR, time.Now().UnixMilli(), fmt.Sprintf("ListRecorder error %v", err)) 
		return
	} else if recorders == nil || len(recorders) == 0 {
		klog.Errorf("ListRecorder empty response\n")
		p.SendSse(reader.RESULT_ERROR, time.Now().UnixMilli(), "ListRecorder empty response") 
		return
	}

	p.setRecorders(recorders)
}

func (p *webserver) setCameras(cameras []util.Camera) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cameras = cameras
}

func (p *webserver) getCameras() []util.Camera {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cameras
}

func (p *webserver) listCamera(token *oauth2.Token) {
	cameras := p.getCameras()
	
	if cameras != nil && len(cameras) > 0 {		
		return
	}

	cameras, err := util.ListCamera(p.config.BaseUrl, token.AccessToken)
	if err != nil {
		klog.Errorf("ListCamera error %v\n", err)
		p.SendSse(reader.RESULT_ERROR, time.Now().UnixMilli(), fmt.Sprintf("ListCamera error %v", err)) 
		return
	} else if cameras == nil || len(cameras) == 0 {
		klog.Errorf("ListCamera empty response\n")
		p.SendSse(reader.RESULT_ERROR, time.Now().UnixMilli(), "ListCamera empty response") 
		return
	}

	p.setCameras(cameras)
}

func (p *webserver) getToken() *oauth2.Token {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.token
}

func (p *webserver) setToken(token *oauth2.Token) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.token = token
}

func (p *webserver) updateToken(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			klog.Infof("updateToken receive stop signal\n")
			return
		case _t := <-p.chanToken:
			klog.Infof("updateToken receive token: %v\n", _t)
			p.setToken(_t)
			go p.listRecorder(_t)
			go p.listCamera(_t)
		}
	}
}

func (p *webserver) Start(ctx context.Context) (StopFunc, error) {
	
	stream := streamer.NewStream()
	go stream.Process(ctx, p.ChanImageData)

	http.HandleFunc("/", p.home)
	http.HandleFunc("/sse", p.sse)
	http.HandleFunc("/api/recorder", p.recorderList)
	http.HandleFunc("/api/camera", p.cameraList)
	http.HandleFunc("/api/live", p.cameraLive)
	http.HandleFunc("/api/test", p.testSse)
	http.Handle("/video", stream)

	server := &http.Server{
		Addr:    p.config.Port,
		Handler: nil,
	}

	stop := func() {
		err := server.Shutdown(ctx)
		if err != nil {
			klog.Errorf("failed to shutdown server %v\n", err)
		}

	}

	go p.updateToken(ctx) // update


	go func() {
		err := server.ListenAndServe()
		if err != nil {
			klog.Errorf("failed to listen on http port %v\n", err)
		}
	}()

	return stop, nil
}