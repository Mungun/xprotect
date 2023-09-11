package oauth

import (
	"context"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	conf "github.com/mungun/xprotect/internal/config"
	util "github.com/mungun/xprotect/internal/util"
	"golang.org/x/oauth2"
	"k8s.io/klog/v2"
)

type AuthHander struct {
	config          *conf.Config
	token           *oauth2.Token
	mu              sync.RWMutex
	conf            *oauth2.Config
	corpToken       *conf.TokenResult
	chanBearerToken chan *oauth2.Token
	chanCorpToken   chan *conf.TokenResult
	instanceId      string
}

func NewAuthHandler(config *conf.Config, chanBearerToken chan *oauth2.Token, chanCorpToken chan *conf.TokenResult) *AuthHander {

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
		config:          config,
		conf:            conf,
		chanBearerToken: chanBearerToken,
		chanCorpToken:   chanCorpToken,
		instanceId:      uuid.New().String(),
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

	h.setBearerToken(_token)
	return _token, nil
}

func (h *AuthHander) getBearerToken() *oauth2.Token {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.token
}

func (h *AuthHander) setBearerToken(token *oauth2.Token) {
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

func (h *AuthHander) currentBearerToken(stopCtx context.Context) *oauth2.Token {
	var _token *oauth2.Token
	_token = h.getBearerToken()
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

func (h *AuthHander) getCorpToken() *conf.TokenResult {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.corpToken
}

func (h *AuthHander) setCorpToken(corpToken *conf.TokenResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.corpToken = corpToken
}

func (h *AuthHander) fetchCorpToken(bearerToken *oauth2.Token) (*conf.TokenResult, error) {

	var _corpToken string
	if s := h.getCorpToken(); s != nil {
		_corpToken = s.Token
	}

	result, err := util.FetchCorpToken(h.config.BaseUrl, bearerToken.AccessToken, _corpToken, h.instanceId)
	if err != nil {
		klog.Errorf("FetchCorpToken error %v\n", err)
		return nil, err
	}

	// h.setCorpToken(result.Token)
	// h.chanCorpToken <- result
	return result, nil
}

func (h *AuthHander) Start(stopCtx context.Context) {

	bearerToken := h.currentBearerToken(stopCtx)
	if bearerToken == nil {
		return // only called context cancel
	}
	h.setBearerToken(bearerToken)
	h.chanBearerToken <- bearerToken //initial token

	corpToken := h.currentCorpToken(stopCtx)
	if corpToken == nil {
		return // only called context cancel
	}
	h.setCorpToken(corpToken)
	h.chanCorpToken <- corpToken //initial corp token

	go h.registerIntegration(stopCtx)

	go h.syncBearerToken(stopCtx)

	go h.syncCorpToken(stopCtx)

}

func (h *AuthHander) syncBearerToken(stopCtx context.Context) {

	for {

		bearerToken := h.currentBearerToken(stopCtx)
		if bearerToken == nil {
			return // only called context cancel
		}

		select {
		case <-stopCtx.Done():
			klog.Infof("syncBearerToken syncer received stop signal\n")
			return
		case <-time.After(time.Until(bearerToken.Expiry)):
			src := h.conf.TokenSource(GetCtxWithClient(context.Background()), bearerToken)
			newToken, err := src.Token() // this actually goes and renews the tokens
			if err != nil {
				klog.Errorf("Refresh token err: %v\n", err)
			} else if newToken.AccessToken != bearerToken.AccessToken {
				h.setBearerToken(newToken)
				h.chanBearerToken <- newToken
			}

		}

	}
}

func isCorpExpired(corpToken *conf.TokenResult) bool {
	if corpToken == nil {
		return true
	}

	if time.Now().Before(corpToken.Expiry) {
		return true
	}

	return false
}

func (h *AuthHander) currentCorpToken(stopCtx context.Context) *conf.TokenResult {
	var _corpToken *conf.TokenResult
	_corpToken = h.getCorpToken()
	after := time.Second
	if isCorpExpired(_corpToken) {
		for {
			_corpToken, err := h.fetchCorpToken(h.getBearerToken())

			if err != nil { //connection err
				klog.Errorf("currentCorpToken fetch error :%v\n", err)
				jitter := int64(1 + after.Seconds())
				after = time.Duration(jitter) * time.Second
				klog.Infof("retry after %v sec\n", after)
			} else {
				klog.Infof("currentCorpToken is %v\n", _corpToken)
				return _corpToken
			}

			select {
			case <-stopCtx.Done():
				klog.Infof("currentCorpToken fetch received stop signal\n")
				return nil
			case <-time.After(after): // wait 1 second
			}

		}
	}

	return _corpToken
}

func (h *AuthHander) syncCorpToken(stopCtx context.Context) {

	for {

		corpToken := h.currentCorpToken(stopCtx)
		if corpToken == nil {
			return // only called context cancel
		}
		select {
		case <-stopCtx.Done():
			klog.Infof("SyncToken syncer received stop signal\n")
			return
		case <-time.After(time.Until(corpToken.Expiry)):
			corpToken, err := h.fetchCorpToken(h.getBearerToken())
			if err != nil {
				klog.Errorf("fetchCorpToken error %v\n", err)
			} else {
				h.setCorpToken(corpToken)
				h.chanCorpToken <- corpToken
			}

		}

	}

}

func (h *AuthHander) registerIntegration(stopCtx context.Context) {
	bearerToken := h.getBearerToken()
	corpToken := h.getCorpToken()

	err := util.RegisterIntegration(h.config.BaseUrl, bearerToken.AccessToken, corpToken.Token, h.instanceId)
	if err != nil {
		klog.Errorf("RegisterIntegration error %v\n", err)
	}
}
