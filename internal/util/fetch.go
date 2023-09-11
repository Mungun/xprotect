package util
import (
	"encoding/json"
	"fmt"
	"net/url"
	"net/http"
	"io"
	"k8s.io/klog/v2"
	"crypto/tls"
	"bytes"
	"encoding/xml"
	"time"
	"github.com/google/uuid"

	conf "github.com/mungun/xprotect/internal/config"
)

type Recorder struct {
	Enabled      bool   `json:"enabled,omitempty"`
	Name         string `json:"displayName,omitempty"`
	Id           string `json:"id,omitempty"`
	WebServerUrl string `json:"webServerUri,omitempty"`
}

type Camera struct {
	Enabled bool   `json:"enabled,omitempty"`
	Name    string `json:"displayName,omitempty"`
	Id      string `json:"id,omitempty"`
	WebServerUrl string `json:"url,omitempty"`
}

type Result[T any] struct {
	List []T `json:"array,omitempty"`
	Data T `json:"data,omitempty"`
}

func (r *Result[T]) decodeArray(payload []byte) ([]T, error) {
	err := json.Unmarshal(payload, r)
	if err != nil {
		return []T{}, fmt.Errorf("Unmarshal error: %v", err)
	}

	return r.List, nil
}

func (r *Result[T]) decodeData(payload []byte) (T, error) {
	err := json.Unmarshal(payload, r)
	if err != nil {
		var empty T
		return empty, fmt.Errorf("Unmarshal error: %v", err)
	}

	return r.Data, nil
}

func GetHttClient() *http.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	
	client := &http.Client{Transport: tr}
	return client
}

func ListCamera(baseUrl string, token string) ([]Camera, error) {
	URL, err := url.JoinPath(baseUrl, "/api/rest/v1/cameras")
	if err != nil {
		klog.Errorf("listCamera JoinPath error :%v\n", err)
		return []Camera{}, fmt.Errorf("invalid base url %s", baseUrl)
	}

	req, err := http.NewRequest(http.MethodGet, URL, nil)
	if err != nil {
		klog.Errorf("listCamera NewRequest error :%v\n", err)
		return []Camera{}, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	client := GetHttClient()
	resp, err := client.Do(req)

	if err != nil {
		klog.Errorf("listCamera send request error :%v\n", err)
		return []Camera{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		klog.Errorf("listCamera ReadAll error :%v\n", err)
		return []Camera{}, err
	}

	if resp.StatusCode != 200 {
		klog.Errorf("listCamera resp code :%v\n", resp.StatusCode)
		return []Camera{}, fmt.Errorf("listCamera error response :%v", string(body))
	}

	var decoder Result[Camera]
	cameras, err := decoder.decodeArray(body)
	if err != nil {
		klog.Errorf("listCamera decodeArray error :%v\n", err)
		return []Camera{}, err
	}

	return cameras, nil

}

func ListRecorder(baseUrl string, token string) ([]Recorder, error) {
	URL, err := url.JoinPath(baseUrl, "/api/rest/v1/recordingServers")
	if err != nil {
		klog.Errorf("listRecorder JoinPath error :%v\n", err)
		return []Recorder{}, fmt.Errorf("invalid base url %s", baseUrl)
	}

	req, err := http.NewRequest(http.MethodGet, URL, nil)
	if err != nil {
		klog.Errorf("listRecorder NewRequest error :%v\n", err)
		return []Recorder{}, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	client := GetHttClient()

	resp, err := client.Do(req)
	if err != nil {
		klog.Errorf("listRecorder send request error :%v\n", err)
		return []Recorder{}, err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		klog.Errorf("listRecorder ReadAll error :%v\n", err)
		return []Recorder{}, err
	}

	if resp.StatusCode != 200 {
		klog.Errorf("listRecorder resp code :%v\n", resp.StatusCode)
		return []Recorder{}, fmt.Errorf("listRecorder error response :%v", string(body))
	}

	var decoder Result[Recorder]
	list, err := decoder.decodeArray(body)
	if err != nil {
		klog.Errorf("listRecorder decodeArray error :%v\n", err)
		return []Recorder{}, err
	}

	return list, nil

}

func FormatLoginXml(instanceId string, currentToken string) []byte {
	message := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"utf-8\"?>"+
	"<s:Envelope xmlns:s=\"http://schemas.xmlsoap.org/soap/envelope/\">"+
	"<s:Body>"+
	"<Login xmlns=\"http://videoos.net/2/XProtectCSServerCommand\">"+
	"<instanceId>%s</instanceId>"+
	"<currentToken>%s</currentToken>"+
	"</Login>"+
	"</s:Body>"+
	"</s:Envelope>", instanceId, currentToken)

	return []byte(message)
} 

func FetchCorpToken(baseUrl string, bearerToken string, currentToken string, instanceId string) (*conf.TokenResult, error) {
	URL, err := url.JoinPath(baseUrl, "/managementServer/ServerCommandService.svc")
	if err != nil {
		klog.Errorf("FetchCorpToken JoinPath error :%v\n", err)
		return nil, fmt.Errorf("invalid base url %s", baseUrl)
	}

	data := FormatLoginXml(instanceId, currentToken)
	req, err := http.NewRequest(http.MethodPost, URL, bytes.NewReader(data))
	if err != nil {
		klog.Errorf("FetchCorpToken NewRequest error :%v\n", err)
		return nil, err
	}

	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", bearerToken))
	req.Header.Set("SOAPAction", "http://videoos.net/2/XProtectCSServerCommand/IServerCommandService/Login");
	client := GetHttClient()

	resp, err := client.Do(req)
	if err != nil {
		klog.Errorf("FetchCorpToken send request error :%v\n", err)
		return nil, err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		klog.Errorf("FetchCorpToken ReadAll error :%v\n", err)
		return nil, err
	}

	if resp.StatusCode != 200 {
		klog.Errorf("FetchCorpToken resp code :%v\n", resp.StatusCode)
		return nil, fmt.Errorf("fetchCorpToken error response :%v", string(body))
	}

	klog.Infof("FetchCorpToken result %s\n", string(body))
	var result conf.TokenResult
	if err := xml.Unmarshal(body, &result); err != nil {
		klog.Errorf("FetchCorpToken result Unmarshal error :%v\n", err)
		return nil, err
	}

	// set Expire Time
	result.Expiry = time.Now().Add(time.Duration(result.TimeToLive)*time.Millisecond)

	return &result, nil
}

func FormatRegisterXml(instanceId string, currentToken string) []byte {
	integrationId := uuid.New().String()
	message := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"utf-8\"?>"+
	"<s:Envelope xmlns:s=\"http://schemas.xmlsoap.org/soap/envelope/\">"+
	"<s:Body>"+
	"<RegisterIntegration xmlns=\"http://videoos.net/2/XProtectCSServerCommand\">"+
	"<token>%s</token>"+
	"<instanceId>%s</instanceId>"+
	"<integrationId>%s</integrationId>"+
	"<integrationVersion>1.0</integrationVersion>"+
	"<integrationName>ot-video-analyzer</integrationName>"+
	"<manufacturerName>andorean</manufacturerName>"+
	"</RegisterIntegration>"+
	"</s:Body>"+
	"</s:Envelope>", currentToken, instanceId, integrationId)

	return []byte(message)
}

func RegisterIntegration(baseUrl string, bearerToken string, token string, instanceId string) (error) {
	URL, err := url.JoinPath(baseUrl, "/managementServer/ServerCommandService.svc")
	if err != nil {
		klog.Errorf("RegisterIntegration JoinPath error :%v\n", err)
		return fmt.Errorf("invalid base url %s", baseUrl)
	}

	data := FormatRegisterXml(instanceId, token)
	req, err := http.NewRequest(http.MethodPost, URL, bytes.NewReader(data))
	if err != nil {
		klog.Errorf("registerIntegration NewRequest error :%v\n", err)
		return err
	}

	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", bearerToken))
	req.Header.Set("SOAPAction", "http://videoos.net/2/XProtectCSServerCommand/IServerCommandService/RegisterIntegration");
	client := GetHttClient()

	resp, err := client.Do(req)
	if err != nil {
		klog.Errorf("registerIntegration send request error :%v\n", err)
		return err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		klog.Errorf("RegisterIntegration ReadAll error :%v\n", err)
		return err
	}

	if resp.StatusCode != 200 {
		klog.Errorf("RegisterIntegration resp code :%v\n", resp.StatusCode)
		return fmt.Errorf("registerIntegration error response :%v", string(body))
	}

	klog.Infof("RegisterIntegration result %s\n", string(body))
	return nil
}