package server

func hashKey(id ID, maxIndex int) int {
	hash := int32(0)
	for _, c := range string(id) {
		hash = (hash << 5) - hash + int32(c)
	}

	if hash < 0 {
		hash = -hash
	}

	return int(hash % int32(maxIndex))
}
