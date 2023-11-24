package service

import (
	"fmt"
	"math/rand"

	"github.com/google/uuid"
)

var letters = []rune("abcdef0123456789")

func randSeqForUUID(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func newTraceID() string {
	traceId, err := uuid.NewRandom()
	if err != nil {
		return fmt.Sprintf("%s-%s-%s-%s-%s", randSeqForUUID(8), randSeqForUUID(4), randSeqForUUID(4), randSeqForUUID(4), randSeqForUUID(12))
	}
	return traceId.String()
}
