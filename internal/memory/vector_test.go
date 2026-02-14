package memory

import (
	"math"
	"strings"
	"testing"
)

func TestEncodeDecodeVectorRoundTrip(t *testing.T) {
	original := []float32{1.5, -2.25, 0, 3.75}

	encoded, err := EncodeVector(original)
	if err != nil {
		t.Fatalf("EncodeVector error: %v", err)
	}

	decoded, err := DecodeVector(encoded)
	if err != nil {
		t.Fatalf("DecodeVector error: %v", err)
	}

	if len(decoded) != len(original) {
		t.Fatalf("decoded length=%d, want %d", len(decoded), len(original))
	}

	for i := range original {
		if decoded[i] != original[i] {
			t.Fatalf("decoded[%d]=%v, want %v", i, decoded[i], original[i])
		}
	}
}

func TestEncodeDecodeVectorMalformedPayload(t *testing.T) {
	t.Run("invalid header length", func(t *testing.T) {
		_, err := DecodeVector([]byte{0x01, 0x02, 0x03})
		if err == nil {
			t.Fatal("expected error for malformed vector header")
		}
		if !strings.Contains(err.Error(), "invalid vector blob length") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("dimension payload mismatch", func(t *testing.T) {
		// Declared dimension=2, but only 1 float32 payload present.
		payload := []byte{
			0x02, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x80, 0x3f,
		}

		_, err := DecodeVector(payload)
		if err == nil {
			t.Fatal("expected error for malformed vector payload")
		}
		if !strings.Contains(err.Error(), "vector blob dimension mismatch") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestCosineSimilarityKnownCases(t *testing.T) {
	t.Run("same vector", func(t *testing.T) {
		score, err := CosineSimilarity([]float32{1, 2, 3}, []float32{1, 2, 3})
		if err != nil {
			t.Fatalf("CosineSimilarity error: %v", err)
		}
		if math.Abs(score-1.0) > 1e-12 {
			t.Fatalf("score=%v, want 1.0", score)
		}
	})

	t.Run("orthogonal vectors", func(t *testing.T) {
		score, err := CosineSimilarity([]float32{1, 0, 0}, []float32{0, 1, 0})
		if err != nil {
			t.Fatalf("CosineSimilarity error: %v", err)
		}
		if math.Abs(score) > 1e-12 {
			t.Fatalf("score=%v, want 0", score)
		}
	})

	t.Run("opposite vectors", func(t *testing.T) {
		score, err := CosineSimilarity([]float32{1, -2, 3}, []float32{-1, 2, -3})
		if err != nil {
			t.Fatalf("CosineSimilarity error: %v", err)
		}
		if math.Abs(score+1.0) > 1e-12 {
			t.Fatalf("score=%v, want -1.0", score)
		}
	})
}

func TestCosineSimilarityDimensionMismatch(t *testing.T) {
	_, err := CosineSimilarity([]float32{1, 2}, []float32{1, 2, 3})
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
	if !strings.Contains(err.Error(), "vector dimension mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCosineSimilarityZeroNorm(t *testing.T) {
	t.Run("left vector zero norm", func(t *testing.T) {
		_, err := CosineSimilarity([]float32{0, 0, 0}, []float32{1, 2, 3})
		if err == nil {
			t.Fatal("expected zero norm error")
		}
		if !strings.Contains(err.Error(), "zero vector norm") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("right vector zero norm", func(t *testing.T) {
		_, err := CosineSimilarity([]float32{1, 2, 3}, []float32{0, 0, 0})
		if err == nil {
			t.Fatal("expected zero norm error")
		}
		if !strings.Contains(err.Error(), "zero vector norm") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

var benchmarkVectorSimilaritySink float64

func BenchmarkVectorBruteForce10k384(b *testing.B) {
	const (
		candidateCount = 10000
		dim            = 384
	)

	query := make([]float32, dim)
	for i := range query {
		query[i] = 1 + float32(i%13)/17
	}

	candidates := make([][]float32, candidateCount)
	for i := 0; i < candidateCount; i++ {
		vec := make([]float32, dim)
		for j := 0; j < dim; j++ {
			vec[j] = float32(((i+1)*(j+3))%97)/97 + 0.01
		}
		candidates[i] = vec
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		best := -2.0
		for _, candidate := range candidates {
			score, err := CosineSimilarity(query, candidate)
			if err != nil {
				b.Fatalf("CosineSimilarity error: %v", err)
			}
			if score > best {
				best = score
			}
		}
		benchmarkVectorSimilaritySink = best
	}
}
