package oauth

import (
	"context"
	"log"
	"net/url"
	"sync"
	"time"

	conf "github.com/mungun/xprotect/internal/config"
	util "github.com/mungun/xprotect/internal/util"
	"golang.org/x/oauth2"
	"k8s.io/klog/v2"
)

type AuthHander struct {
	config *conf.Config
	token  *oauth2.Token
	mu     sync.RWMutex
	conf   *oauth2.Config
}

func NewAuthHandler(config *conf.Config) *AuthHander {

	tokenUrl, err := url.JoinPath(config.BaseUrl, "/idp/connect/token")

	if err != nil {
		log.Fatalf("invalid base url: %v\n", config.BaseUrl)
	}

	conf := &oauth2.Config{
		ClientID: config.ClientId,
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenUrl,
		},
	}
	return &AuthHander{
		config: config,
		conf:   conf,
	}
}

func GetCtxWithClient(ctx context.Context) context.Context {
	httpClient := util.GetHttClient()
	return context.WithValue(ctx, oauth2.HTTPClient, httpClient)
}

func (h *AuthHander) Connect() (*oauth2.Token, error) {

	_token, err := h.conf.PasswordCredentialsToken(GetCtxWithClient(context.Background()), h.config.Username, h.config.Password)
	if err != nil {
		klog.Errorf("PasswordCredentialsToken err: %v\n", err)
		return nil, err
	}

	klog.Infof("Token :%+v\n", _token)
	klog.Infof("Token will expire at :%v\n", _token.Expiry)

	h.setToken(_token)
	return _token, nil
}

func (h *AuthHander) getToken() *oauth2.Token {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.token
}

func (h *AuthHander) setToken(token *oauth2.Token) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.token = token
}

func isExpired(token *oauth2.Token) bool {
	if token == nil {
		return true
	}

	if time.Now().Before(token.Expiry) {
		return true
	}

	return false
}

func (h *AuthHander) currentToken(stopCtx context.Context) *oauth2.Token {
	var _token *oauth2.Token
	_token = h.getToken()
	after := time.Second
	if isExpired(_token) {
		for {
			_token, err := h.Connect()

			if err != nil { //connection err
				klog.Errorf("currentToken connect error :%v\n", err)
				jitter := int64(1 + after.Seconds())
				after = time.Duration(jitter) * time.Second
				klog.Infof("retry after %v sec\n", after)
			} else {
				klog.Infof("token is %v\n", _token)
				return _token
			}

			select {
			case <-stopCtx.Done():
				klog.Infof("currentToken connect received stop signal\n")
				return nil
			case <-time.After(after): // wait 1 second
			}

		}
	}

	return _token
}

func (h *AuthHander) SyncToken(stopCtx context.Context, chanToken chan *oauth2.Token, init bool) {

	isInit := init
	for {
		token := h.currentToken(stopCtx)
		if token == nil { //ctx cancelled from sub routine
			return
		}

		if isInit {
			chanToken <- token //initial token
			isInit = false
		}

		select {
		case <-stopCtx.Done():
			klog.Infof("SyncToken syncer received stop signal\n")
			return
		case <-time.After(time.Until(token.Expiry)):
			src := h.conf.TokenSource(GetCtxWithClient(context.Background()), token)
			newToken, err := src.Token() // this actually goes and renews the tokens
			if err != nil {
				klog.Errorf("Refresh token err: %v\n", err)
			} else if newToken.AccessToken != token.AccessToken {
				h.setToken(newToken)
				chanToken <- newToken
			}
		}

	}

}
