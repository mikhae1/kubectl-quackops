package lib

import (
	"fmt"
	"math"
)

// CosineSimilarity computes cosine similarity between two embedding vectors.
func CosineSimilarity(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// CosineSimilarity computes row‑wise cosine similarity between two equal‑shaped matrices X and Y.
// Each of X and Y must be a slice of the same number of float32 slices, each of equal length.
// Returns a slice of length len(X) where each element is
//
//	dot(X[i],Y[i]) / (‖X[i]‖ · ‖Y[i]‖).
func CosineSimilarityMatrix(X, Y [][]float32) ([]float32, error) {
	n := len(X)
	if n != len(Y) {
		return nil, fmt.Errorf("CosineSimilarity: row count mismatch X has %d rows, Y has %d", n, len(Y))
	}

	result := make([]float32, n)
	for i := 0; i < n; i++ {
		xi, yi := X[i], Y[i]
		if len(xi) != len(yi) {
			return nil, fmt.Errorf("CosineSimilarity: column count mismatch at row %d: len(X[%d])=%d, len(Y[%d])=%d",
				i, i, len(xi), i, len(yi))
		}

		var dot, normX, normY float64
		for j := range xi {
			a := float64(xi[j])
			b := float64(yi[j])
			dot += a * b
			normX += a * a
			normY += b * b
		}

		if normX == 0 || normY == 0 {
			// if either vector is zero‐length, define similarity as zero
			result[i] = 0
		} else {
			result[i] = float32(dot / (math.Sqrt(normX) * math.Sqrt(normY)))
		}
	}

	return result, nil
}
