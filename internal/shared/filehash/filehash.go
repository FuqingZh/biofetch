package filehash

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sync"
)

const bufferSize = 1 << 20

var bufferPool = sync.Pool{
	New: func() any {
		buffer := make([]byte, bufferSize)
		return &buffer
	},
}

// SHA256 reads all content from reader and returns its lowercase SHA256 digest.
// Buffers are pooled so locking snapshots with many files does not allocate one
// large copy buffer per file.
func SHA256(reader io.Reader) (string, error) {
	hash := sha256.New()
	buffer := bufferPool.Get().(*[]byte)
	defer bufferPool.Put(buffer)
	if _, err := io.CopyBuffer(hash, reader, *buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
