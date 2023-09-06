package util
import (
	"encoding/json"
	"fmt"
	"net/url"
	"net/http"
	"io"
	"k8s.io/klog/v2"
	"crypto/tls"
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