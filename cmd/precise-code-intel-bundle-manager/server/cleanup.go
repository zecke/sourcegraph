package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sourcegraph/sourcegraph/internal/endpoint"
	"github.com/sourcegraph/sourcegraph/internal/env"
	"github.com/sourcegraph/sourcegraph/internal/trace/ot"
)

const FailedUploadMaxAge = time.Minute * 20      // TODO - configure
const MaximumSizeBytes = 10 * 1024 * 1024 * 1024 // TODO - configure

func (s *Server) Janitor() error {
	for _, task := range []func() error{s.removeDeadDumps, s.cleanOldDumps, s.cleanFailedUploads} {
		if err := task(); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) cleanOldDumps() error {
	dirSizeBytes, err := dirSize(filepath.Join(s.storageDir, "dbs"))
	if err != nil {
		return err
	}

	for dirSizeBytes > MaximumSizeBytes {
		id, prunable, err := DefaultClient.Prune()
		if err != nil {
			return err
		}

		if !prunable {
			break
		}

		filename := s.dbFilename(id)
		fileInfo, err := os.Stat(filename)
		if err != nil {
			return err
		}

		if err := os.Remove(filename); err != nil {
			return err
		}

		dirSizeBytes -= fileInfo.Size()
	}

	return nil
}

func (s *Server) removeDeadDumps() error {
	fileInfos, err := ioutil.ReadDir(filepath.Join(s.storageDir, "dbs"))
	if err != nil {
		return err
	}

	pathsByID := map[int]string{}
	for _, fileInfo := range fileInfos {
		id, ok := idFromFilename(fileInfo.Name())
		if !ok {
			continue
		}

		pathsByID[id] = filepath.Join(s.storageDir, "dbs", fileInfo.Name())
	}

	// TODO - request in max-sized chunks

	var ids []int
	for id := range pathsByID {
		ids = append(ids, id)
	}

	states, err := DefaultClient.States(ids)
	if err != nil {
		return err
	}

	for id, path := range pathsByID {
		state, exists := states[id]
		if !exists || state == "errored" {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Server) cleanFailedUploads() error {
	fileInfos, err := ioutil.ReadDir(filepath.Join(s.storageDir, "uploads"))
	if err != nil {
		return err
	}

	for _, fileInfo := range fileInfos {
		if time.Since(fileInfo.ModTime()) < FailedUploadMaxAge {
			continue
		}

		if err := os.Remove(filepath.Join(s.storageDir, "uploads", fileInfo.Name())); err != nil {
			return err
		}
	}

	return nil
}

func idFromFilename(filename string) (int, bool) {
	id, err := strconv.Atoi(strings.Split(filename, ".")[0])
	if err != nil {
		return 0, false
	}

	return int(id), true
}

func dirSize(path string) (int64, error) {
	fileInfos, err := ioutil.ReadDir(path)
	if err != nil {
		return 0, err
	}

	size := int64(0)
	for _, fileInfo := range fileInfos {
		if !fileInfo.IsDir() {
			size += fileInfo.Size()
		}
	}

	return size, nil
}

//
// TODO - make one unified client
//

var (
	preciseCodeIntelAPIServerURL = env.Get("PRECISE_CODE_INTEL_API_SERVER_URL", "k8s+http://precise-code-intel:3186", "precise-code-intel-api-server URL (or space separated list of precise-code-intel-api-server URLs)")

	preciseCodeIntelAPIServerURLsOnce sync.Once
	preciseCodeIntelAPIServerURLs     *endpoint.Map

	DefaultClient = &Client{
		endpoint: LSIFURLs(),
		HTTPClient: &http.Client{
			// ot.Transport will propagate opentracing spans
			Transport: &ot.Transport{},
		},
	}
)

type Client struct {
	endpoint   *endpoint.Map
	HTTPClient *http.Client
}

func LSIFURLs() *endpoint.Map {
	preciseCodeIntelAPIServerURLsOnce.Do(func() {
		if len(strings.Fields(preciseCodeIntelAPIServerURL)) == 0 {
			preciseCodeIntelAPIServerURLs = endpoint.Empty(errors.New("an precise-code-intel-api-server has not been configured"))
		} else {
			preciseCodeIntelAPIServerURLs = endpoint.New(preciseCodeIntelAPIServerURL)
		}
	})
	return preciseCodeIntelAPIServerURLs
}

func (c *Client) Prune() (int64, bool, error) {
	serverURL, err := c.endpoint.Get("", nil)
	if err != nil {
		return 0, false, err
	}

	req, err := http.NewRequest("POST", serverURL+"/prune", nil)
	if err != nil {
		return 0, false, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, false, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var id *int64
	if err := json.NewDecoder(resp.Body).Decode(&id); err != nil {
		return 0, false, err
	}

	if id == nil {
		return 0, false, nil
	}

	return *id, true, nil
}

func (c *Client) States(ids []int) (map[int]string, error) {
	serverURL, err := c.endpoint.Get("", nil)
	if err != nil {
		return nil, err
	}

	reqPayload, err := json.Marshal(map[string]interface{}{"ids": ids})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", serverURL+"/uploads", bytes.NewReader(reqPayload))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var respPayload wrappedMapValue
	if err := json.NewDecoder(resp.Body).Decode(&respPayload); err != nil {
		return nil, err
	}

	states := map[int]string{}
	for _, pair := range respPayload.Value {
		var key int
		var value string
		payload := []interface{}{&key, &value}
		if err := json.Unmarshal([]byte(pair), &payload); err != nil {
			return nil, err
		}

		states[key] = value
	}

	return states, nil
}
