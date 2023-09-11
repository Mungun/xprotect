package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"k8s.io/klog/v2"
)
var (
	bufioReaderPool   sync.Pool
	bufioWriter2kPool sync.Pool
	bufioWriter4kPool sync.Pool
)

func newBufioReader(r io.Reader) *bufio.Reader {
	if v := bufioReaderPool.Get(); v != nil {
		br := v.(*bufio.Reader)
		br.Reset(r)
		return br
	}
	// Note: if this reader size is ever changed, update
	// TestHandlerBodyClose's assumptions.
	return bufio.NewReader(r)
}

type Config struct {
	Port string
}

func InitConfig() *Config {
	config := Config{}
	flag.StringVar(&config.Port, "port", ":9999", "server port")
	flag.Parse()

	return &config
}

func Server struct {
	listener net.Listener
	conns map[net.Conn]struct{}
	mtx sync.Mutex
	cancelled bool
}

func (srv *Server) Serve() {
	var stop bool
	for {
		srv.mtx.Lock()
		stop = srv.cancelled
		srv.mtx.Unlock()

		if stop {
			break
		}

		conn, err := srv.listener.Accept()
		if err != nil {
			klog.Infof("start - accept error %v\n", err)
			continue
		}
		srv.mtx.Lock()
		srv.conns[conn] = struct{}{}
		srv.mtx.Unlock()
		go srv.handleConnection(conn)
	}
}

func (srv *Server) handleConnection(conn net.Conn) error {
	klog.Infof("Accepted connection from %s\n", conn.RemoteAddr())

	defer func() {
		klog.Infof("Closing connection from %s\n", conn.RemoteAddr())
		conn.Close()
		srv.mtx.Lock()
		delete(srv.conns, conn)
		srv.mtx.Unlock()
	}()

	bfr := bufio.NewReader(conn)
	bfw := 
	buf := make([]byte, 1024)
	_, err := conn.Read(buf)
	if err != nil {
		fmt.Println("Read error", err.Error())
		return err
	}
	return nil
}



func handle(ctx context.Context, wg *sync.WaitGroup, conn net.Conn) {
	defer wg.Done()
	defer conn.Close()

	for {
		select {
		case <-ctx.Done():
			klog.Infof("handle - receive stop signal\n")
			return
		default:
			netData, err := bufio.NewReader(conn).ReadString('\n')
			if err != nil {
				klog.Errorf("handle - read error!%v\n", err)
				return
			}

			if strings.TrimSpace(string(netData)) == "STOP" {
				klog.Errorf("handle - read STOP, will close connection\n")
				return
			}

			klog.Infof("-> %s\n", string(netData))
			t := time.Now()
			conn.Write([]byte(fmt.Sprintf("%s\n", t.Format(time.RFC3339))))
		}
	}

}


func NewServer(config *Config) *Server {
	port := fmt.Sprintf(":%s", config.Port)
	addr, err := net.ResolveTCPAddr("tcp4", port)
	if err != nil {
		klog.Fatalf("Failed to resolve address %v\n", err.Error())		
	}

	listener, err := net.Listen("tcp", addr.String())
	if err != nil {
		klog.Fatalf("Failed to create listener %v\n", err)
	}

	listener, err := net.Listen("tcp", addr.String())
	if err != nil {
		fmt.Println("Failed to create listener", err.Error())
		os.Exit(1)
	}

	srv := &Server{
		listener,
		make([]net.Conn, 0, 50),
		sync.Mutex{},
		false,
	}

	go srv.Serve()
	return srv
}

func main() {
	klog.InitFlags(nil)

	config := InitConfig()
	termChan := make(chan os.Signal)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go start(ctx, wg, config)

	sig := <-termChan // Blocks here until interrupted
	klog.Infoln("********************************* Shutdown signal received *********************************")
	klog.Infof("Signal [%v]\n", sig)
	cancel()
	wg.Wait()

	klog.Flush()
	klog.Infoln("********************************* All worker done *********************************")
}
