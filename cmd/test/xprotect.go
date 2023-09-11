package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/oauth2"
	"k8s.io/klog/v2"

	conf "github.com/mungun/xprotect/internal/config"
	auth "github.com/mungun/xprotect/internal/oauth"
	reader "github.com/mungun/xprotect/internal/reader"
	web "github.com/mungun/xprotect/internal/web"
)

func main() {
	klog.InitFlags(nil)

	config := conf.InitConfig()
	termChan := make(chan os.Signal)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	// defer cancel()
	chanBearerTokenResult := make(chan *oauth2.Token, 1)
	chanCorpTokenResult := make(chan *conf.TokenResult, 1)

	chanReaderCommand := make(chan *reader.StreamCommand, 1)
	chanReaderResult := make(chan *reader.StreamResult, 1)

	chanWebserverBearerToken := make(chan *oauth2.Token, 1)
	chanWebserverCorpToken := make(chan *conf.TokenResult, 1)
	// Start oauth service
	authHandler := auth.NewAuthHandler(config, chanBearerTokenResult, chanCorpTokenResult)
	go authHandler.Start(ctx)

	// Start web server
	server := web.NewWebserver(chanWebserverBearerToken, chanWebserverCorpToken, chanReaderCommand, config)
	stop, err := server.Start(ctx)
	if err != nil {
		klog.Errorf("server start err %v\n", err)
		termChan <- syscall.SIGTERM
	}

	// Start reader
	readerHandler := reader.NewReaderHandler()
	go readerHandler.Start(ctx, chanReaderCommand, chanReaderResult)

	// process
	go func(_ctx context.Context, _chanToken chan *oauth2.Token, _chanCorpToken chan *conf.TokenResult, _chanReaderResult chan *reader.StreamResult, imageData chan []byte) {
		for {
			select {
			case <-_ctx.Done():
				return
			case _t := <-_chanToken: // token updated
				chanWebserverBearerToken <- _t
			case _t := <-_chanCorpToken:
				chanWebserverCorpToken <- _t
				chanReaderCommand <- &reader.StreamCommand{
					Type:      reader.COMMAND_TOKEN_UPDATE,
					CorpToken: _t,
				}
			case _res := <-_chanReaderResult:
				if _res.Type == reader.RESULT_ERROR {
					t := time.Now().UnixMilli()
					server.SendSse(_res.Type, t, _res.Message)
				}
				if _res.Type == reader.RESULT_IMAGE {
					imageData <- _res.Image.Data
				}
			}
		}
	}(ctx, chanBearerTokenResult, chanCorpTokenResult, chanReaderResult, server.ChanImageData)

	sig := <-termChan // Blocks here until interrupted
	klog.Infoln("********************************* Shutdown signal received *********************************")
	klog.Infof("Signal [%v]\n", sig)
	cancel()
	stop()
	klog.Flush()
	klog.Infoln("********************************* All worker done *********************************")
}
