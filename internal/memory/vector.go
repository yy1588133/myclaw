package memory

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	vectorBlobHeaderSize = 4
	vectorValueByteSize  = 4
)

// EncodeVector encodes a float32 vector into a binary blob.
// Format: [4-byte little-endian dimension][N x 4-byte little-endian float32 values].
func EncodeVector(vector []float32) ([]byte, error) {
	if len(vector) == 0 {
		return nil, fmt.Errorf("encode vector: empty vector")
	}

	maxDim := (math.MaxInt - vectorBlobHeaderSize) / vectorValueByteSize
	if len(vector) > maxDim {
		return nil, fmt.Errorf("encode vector: dimension too large: %d", len(vector))
	}

	blob := make([]byte, vectorBlobHeaderSize+len(vector)*vectorValueByteSize)
	binary.LittleEndian.PutUint32(blob[:vectorBlobHeaderSize], uint32(len(vector)))

	offset := vectorBlobHeaderSize
	for i, value := range vector {
		if !isFiniteFloat64(float64(value)) {
			return nil, fmt.Errorf("encode vector: invalid value at index %d", i)
		}
		bits := math.Float32bits(value)
		binary.LittleEndian.PutUint32(blob[offset:offset+vectorValueByteSize], bits)
		offset += vectorValueByteSize
	}

	return blob, nil
}

// DecodeVector decodes a vector blob created by EncodeVector.
func DecodeVector(blob []byte) ([]float32, error) {
	if len(blob) < vectorBlobHeaderSize {
		return nil, fmt.Errorf("decode vector: invalid vector blob length: %d", len(blob))
	}

	dim := int(binary.LittleEndian.Uint32(blob[:vectorBlobHeaderSize]))
	if dim <= 0 {
		return nil, fmt.Errorf("decode vector: invalid vector dimension: %d", dim)
	}

	maxDim := (math.MaxInt - vectorBlobHeaderSize) / vectorValueByteSize
	if dim > maxDim {
		return nil, fmt.Errorf("decode vector: invalid vector dimension: %d", dim)
	}

	expectedLength := vectorBlobHeaderSize + dim*vectorValueByteSize
	if len(blob) != expectedLength {
		return nil, fmt.Errorf("decode vector: vector blob dimension mismatch: dim=%d payload=%d", dim, len(blob)-vectorBlobHeaderSize)
	}

	vector := make([]float32, dim)
	offset := vectorBlobHeaderSize
	for i := range vector {
		value := math.Float32frombits(binary.LittleEndian.Uint32(blob[offset : offset+vectorValueByteSize]))
		if !isFiniteFloat64(float64(value)) {
			return nil, fmt.Errorf("decode vector: invalid value at index %d", i)
		}
		vector[i] = value
		offset += vectorValueByteSize
	}

	return vector, nil
}

// CosineSimilarity computes cosine similarity for two vectors.
func CosineSimilarity(a, b []float32) (float64, error) {
	if len(a) == 0 || len(b) == 0 {
		return 0, fmt.Errorf("cosine similarity: empty vector")
	}
	if len(a) != len(b) {
		return 0, fmt.Errorf("cosine similarity: vector dimension mismatch: %d vs %d", len(a), len(b))
	}

	var dot float64
	var normA float64
	var normB float64

	for i := range a {
		ai := float64(a[i])
		bi := float64(b[i])
		if !isFiniteFloat64(ai) {
			return 0, fmt.Errorf("cosine similarity: invalid value in vector a at index %d", i)
		}
		if !isFiniteFloat64(bi) {
			return 0, fmt.Errorf("cosine similarity: invalid value in vector b at index %d", i)
		}

		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}

	if normA == 0 {
		return 0, fmt.Errorf("cosine similarity: zero vector norm for a")
	}
	if normB == 0 {
		return 0, fmt.Errorf("cosine similarity: zero vector norm for b")
	}

	score := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	if score > 1 {
		score = 1
	} else if score < -1 {
		score = -1
	}

	return score, nil
}

func isFiniteFloat64(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
