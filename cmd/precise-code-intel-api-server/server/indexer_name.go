package server

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
)

type MetaDataVertex struct {
	Label    string   `json:"label"`
	ToolInfo ToolInfo `json:"toolInfo"`
}

type ToolInfo struct {
	Name string `json:"name"`
}

func readIndexerNameFromFile(f *os.File) (string, error) {
	_, err1 := f.Seek(0, 0)
	name, err2 := readIndexerName(f)
	_, err3 := f.Seek(0, 0)

	for _, err := range []error{err1, err2, err3} {
		if err != nil {
			return "", err
		}
	}

	return name, nil
}

func readIndexerName(r io.Reader) (string, error) {
	gzipReader, err := gzip.NewReader(r)
	if err != nil {
		return "", err
	}

	line, isPrefix, err := bufio.NewReader(gzipReader).ReadLine()
	if err != nil {
		return "", err
	}
	if isPrefix {
		// OOF strange condition in these parts
		return "", err
	}

	meta := MetaDataVertex{}
	if err := json.Unmarshal(line, &meta); err != nil {
		return "", err
	}

	if meta.Label != "metaData" || meta.ToolInfo.Name == "" {
		panic("OOPS")
	}

	return meta.ToolInfo.Name, nil
}
