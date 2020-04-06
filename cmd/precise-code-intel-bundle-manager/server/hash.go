package server

func hashKey(id ID, maxIndex int) int {
	hash := 0
	for _, c := range string(id) {
		chr := int(c)
		hash = (hash << 5) - hash + chr
		hash |= 0
	}

	if hash < 0 {
		hash = -hash
	}
	return hash % maxIndex
}
