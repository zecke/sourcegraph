package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type Location struct {
	DumpID int    `json:"dumpId"`
	Path   string `json:"path"`
	Range  Range  `json:"range"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Database struct {
	bundleManagerURL string
	dumpID           int
}

func (d *Database) Exists(path string) (exists bool, err error) {
	err = d.request("exists", url.Values{"path": []string{path}}, &exists)
	return
}

func (d *Database) Definitions(path string, line, character int) ([]Location, error) {
	var locations []Location
	err := d.request("definitions", url.Values{"path": []string{path}, "line": []string{fmt.Sprintf("%d", line)}, "character": []string{fmt.Sprintf("%d", character)}}, &locations)
	if err != nil {
		return nil, err
	}
	for i := range locations {
		locations[i].DumpID = d.dumpID
	}
	return locations, nil
}

func (d *Database) References(path string, line, character int) ([]Location, error) {
	var locations []Location
	err := d.request("references", url.Values{"path": []string{path}, "line": []string{fmt.Sprintf("%d", line)}, "character": []string{fmt.Sprintf("%d", character)}}, &locations)
	if err != nil {
		return nil, err
	}
	for i := range locations {
		locations[i].DumpID = d.dumpID
	}
	return locations, nil
}

func (d *Database) Hover(path string, line, character int) (text string, r Range, exists bool, err error) {
	var target json.RawMessage
	err = d.request("hover", url.Values{"path": []string{path}, "line": []string{fmt.Sprintf("%d", line)}, "character": []string{fmt.Sprintf("%d", character)}}, &target)

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

type MonikerData struct {
	Kind                 string `json:"kind"`
	Scheme               string `json:"scheme"`
	Identifier           string `json:"identifier"`
	PackageInformationID string `json:"packageInformationID"`
}

func (d *Database) MonikersByPosition(path string, line, character int) (target [][]MonikerData, err error) {
	err = d.request("monikersByPosition", url.Values{"path": []string{path}, "line": []string{fmt.Sprintf("%d", line)}, "character": []string{fmt.Sprintf("%d", character)}}, &target)
	return
}

func (d *Database) MonikerResults(modelType, scheme, identifier string, skip, take *int) (locations []Location, count int, err error) {
	target := struct {
		Locations []Location `json:"locations"`
		Count     int        `json:"count"`
	}{}

	vars := url.Values{
		"modelType":  []string{modelType},
		"scheme":     []string{scheme},
		"identifier": []string{identifier},
	}
	if skip != nil {
		vars["skip"] = []string{fmt.Sprintf("%d", *skip)}
	}
	if take != nil {
		vars["take"] = []string{fmt.Sprintf("%d", *take)}
	}

	if err = d.request("monikerResults", vars, &target); err != nil {
		return
	}

	locations = target.Locations
	count = target.Count
	for i := range locations {
		locations[i].DumpID = d.dumpID
	}
	return
}

type PackageInformationData struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (d *Database) PackageInformation(path, packageInformationId string) (target PackageInformationData, err error) {
	err = d.request("packageInformation", url.Values{"path": []string{path}, "packageInformationId": []string{packageInformationId}}, &target)
	return
}

func (d *Database) request(path string, qs url.Values, target interface{}) error {
	url, err := url.Parse(fmt.Sprintf("%s/dbs/%d/%s", d.bundleManagerURL, d.dumpID, path))
	if err != nil {
		return err
	}
	url.RawQuery = qs.Encode()

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

func sendUpload(addr string, id int, r io.Reader) error {
	url, err := url.Parse(fmt.Sprintf("%s/uploads/%d", addr, id))
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url.String(), r)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status %d", resp.StatusCode)
	}

	return nil
}
