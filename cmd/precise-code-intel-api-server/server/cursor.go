package server

import "fmt"

type Cursor struct {
	Phase                  string        // common
	DumpID                 int           // common
	Path                   string        // same-dump/definition-monikers
	Line                   int           // same-dump
	Character              int           // same-dump
	Monikers               []MonikerData // same-dump/definition-monikers
	SkipResults            int           // same-dump/definition-monikers
	Identifier             string        // same-repo/remote-repo
	Scheme                 string        // same-repo/remote-repo
	Name                   string        // same-repo/remote-repo
	Version                string        // same-repo/remote-repo
	DumpIDs                []int         // same-repo/remote-repo
	TotalDumpsWhenBatching int           // same-repo/remote-repo
	SkipDumpsWhenBatching  int           // same-repo/remote-repo
	SkipDumpsInBatch       int           // same-repo/remote-repo
	SkipResultsInDump      int           // same-repo/remote-repo
}

func decodeCursor(raw string) (Cursor, error) {
	return Cursor{}, fmt.Errorf("Unimplemented") // TODO - implement
}

func encodeCursor(cursor Cursor) string {
	return "" // TODO - implement
}
