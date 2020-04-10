package bundles

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type BundleClient struct {
	bundleManagerURL string
	bundleID         int
}

func (c *BundleClient) Exists(path string) (exists bool, err error) {
	err = c.request("exists", map[string]interface{}{"path": path}, &exists)
	return
}

func (c *BundleClient) Definitions(path string, line, character int) (locations []Location, err error) {
	args := map[string]interface{}{
		"path":      path,
		"line":      line,
		"character": character,
	}

	err = c.request("definitions", args, &locations)
	c.addBundleIDToLocations(locations)
	return
}

func (c *BundleClient) References(path string, line, character int) (locations []Location, err error) {
	args := map[string]interface{}{
		"path":      path,
		"line":      line,
		"character": character,
	}

	err = c.request("references", args, &locations)
	c.addBundleIDToLocations(locations)
	return
}

func (c *BundleClient) Hover(path string, line, character int) (text string, r Range, exists bool, err error) {
	args := map[string]interface{}{
		"path":      path,
		"line":      line,
		"character": character,
	}

	var target json.RawMessage
	err = c.request("hover", args, &target)

	// TODO - gross
	if string(target) == "null" {
		exists = false
		return
	}
	exists = true

	payload := struct {
		Text  string `json:"text"`
		Range Range  `json:"range"`
	}{}
	err = json.Unmarshal(target, &payload)
	text = payload.Text
	r = payload.Range
	return
}

func (c *BundleClient) MonikersByPosition(path string, line, character int) (target [][]MonikerData, err error) {
	args := map[string]interface{}{
		"path":      path,
		"line":      line,
		"character": character,
	}

	err = c.request("monikersByPosition", args, &target)
	return
}

func (c *BundleClient) MonikerResults(modelType, scheme, identifier string, skip, take *int) (locations []Location, count int, err error) {
	args := map[string]interface{}{
		"modelType":  modelType,
		"scheme":     scheme,
		"identifier": identifier,
	}
	if skip != nil {
		args["skip"] = *skip
	}
	if take != nil {
		args["take"] = *take
	}

	target := struct {
		Locations []Location `json:"locations"`
		Count     int        `json:"count"`
	}{}

	// TODO
	err = c.request("monikerResults", args, &target)
	locations = target.Locations
	count = target.Count
	c.addBundleIDToLocations(locations)
	return
}

func (c *BundleClient) PackageInformation(path, packageInformationId string) (target PackageInformationData, err error) {
	args := map[string]interface{}{
		"path":                 path,
		"packageInformationId": packageInformationId,
	}

	err = c.request("packageInformation", args, &target)
	return
}

func (c *BundleClient) request(path string, qs map[string]interface{}, target interface{}) error {
	values := url.Values{}
	for k, v := range qs {
		values[k] = []string{fmt.Sprintf("%v", v)} // TODO - check serialization here
	}

	url, err := url.Parse(fmt.Sprintf("%s/dbs/%d/%s", c.bundleManagerURL, c.bundleID, path))
	if err != nil {
		return err
	}
	url.RawQuery = values.Encode()

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(&target)
}

func (c *BundleClient) addBundleIDToLocations(locations []Location) {
	for i := range locations {
		locations[i].DumpID = c.bundleID
	}
}
