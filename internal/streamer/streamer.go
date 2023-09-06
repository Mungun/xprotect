package streamer

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"k8s.io/klog/v2"
)

type Stream struct {
	m             map[chan []byte]bool
	frame         []byte
	lock          sync.RWMutex
	FrameInterval time.Duration
}

const boundaryWord = "MJPEGBOUNDARY"
const headerf = "\r\n" +
	"--" + boundaryWord + "\r\n" +
	"Content-Type: image/jpeg\r\n" +
	"Content-Length: %d\r\n" +
	"X-Timestamp: 0.000000\r\n" +
	"\r\n"

func (s *Stream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	klog.Infof("Stream:%v connected", r.RemoteAddr)
	w.Header().Add("Content-Type", "multipart/x-mixed-replace;boundary="+boundaryWord)

	c := make(chan []byte)
	s.lock.Lock()
	s.m[c] = true
	s.lock.Unlock()

	for {
		time.Sleep(s.FrameInterval)
		b := <-c
		_, err := w.Write(b)
		if err != nil {
			break
		}
	}

	s.lock.Lock()
	delete(s.m, c)
	s.lock.Unlock()
	klog.Infof("Stream:%v disconnected", r.RemoteAddr)
}

// JPEG frame onto the clients.
func (s *Stream) Process(ctx context.Context, dataChan chan []byte) {

	for {
		select {
		case <-ctx.Done():
			return
		case jpeg := <-dataChan:
			header := fmt.Sprintf(headerf, len(jpeg))
			if len(s.frame) < len(jpeg)+len(header) {
				s.frame = make([]byte, (len(jpeg)+len(header))*2)
			}

			copy(s.frame, header)
			copy(s.frame[len(header):], jpeg)

			s.lock.RLock()
			for c := range s.m {
				// Select to skip streams which are sleeping to drop frames.
				// This might need more thought.
				select {
				case c <- s.frame:
				default:
				}
			}
			s.lock.RUnlock()
		}
	}
}

// NewStream initializes and returns a new Stream.
func NewStream() *Stream {
	return &Stream{
		m:             make(map[chan []byte]bool),
		frame:         make([]byte, len(headerf)),
		FrameInterval: 50 * time.Millisecond,
	}
}
