package ucloud

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
)

func sign(values map[string]string, privateKey string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var source strings.Builder
	for _, key := range keys {
		source.WriteString(key)
		source.WriteString(values[key])
	}
	source.WriteString(privateKey)
	sum := sha1.Sum([]byte(source.String()))
	return hex.EncodeToString(sum[:])
}
