package server

import "github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server/bundles"

func (s *Server) getHover(file string, line, character, uploadID int) (string, bundles.Range, bool, error) {
	dump, bundleClient, exists, err := s.getDumpAndBundleClient(uploadID)
	if err != nil {
		return "", bundles.Range{}, false, err
	}
	if !exists {
		return "", bundles.Range{}, false, ErrMissingDump
	}

	pathx := pathRelativeToRoot(dump.Root, file)
	text, rn, exists, err := bundleClient.Hover(pathx, line, character)
	if err != nil {
		return "", bundles.Range{}, false, err
	}
	if exists {
		return text, rn, true, nil
	}

	resolved, err := s.getDefsRaw(dump, bundleClient, pathx, line, character)
	if err != nil {
		return "", bundles.Range{}, false, err
	}
	if len(resolved) == 0 {
		return "", bundles.Range{}, false, nil
	}
	definition := resolved[0]

	return s.bundleManagerClient.BundleClient(definition.Dump.ID).Hover(
		pathRelativeToRoot(definition.Dump.Root, definition.Path),
		definition.Range.Start.Line,
		definition.Range.Start.Character,
	)
}
