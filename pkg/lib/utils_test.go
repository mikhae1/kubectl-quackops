package lib

import (
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	testCases := []struct {
		name     string
		a        []float32
		b        []float32
		expected float64
	}{
		{
			name:     "identical vectors",
			a:        []float32{1.0, 2.0, 3.0},
			b:        []float32{1.0, 2.0, 3.0},
			expected: 1.0, // identical vectors should have similarity of 1.0
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{0.0, 1.0, 0.0},
			expected: 0.0, // orthogonal vectors should have similarity of 0.0
		},
		{
			name:     "opposite vectors",
			a:        []float32{1.0, 2.0, 3.0},
			b:        []float32{-1.0, -2.0, -3.0},
			expected: -1.0, // opposite vectors should have similarity of -1.0
		},
		{
			name:     "zero vector a",
			a:        []float32{0.0, 0.0, 0.0},
			b:        []float32{1.0, 2.0, 3.0},
			expected: 0.0, // zero vector should have similarity of 0.0
		},
		{
			name:     "zero vector b",
			a:        []float32{1.0, 2.0, 3.0},
			b:        []float32{0.0, 0.0, 0.0},
			expected: 0.0, // zero vector should have similarity of 0.0
		},
		{
			name:     "similar vectors",
			a:        []float32{1.0, 2.0, 3.0},
			b:        []float32{2.0, 3.0, 4.0},
			expected: 0.9926, // actual calculated value ≈0.9926
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := CosineSimilarity(tc.a, tc.b)

			// For all cases, check with a small tolerance
			if math.Abs(result-tc.expected) > 0.0001 {
				t.Errorf("Expected similarity %.4f, got %.4f", tc.expected, result)
			}
		})
	}
}

func TestCosineSimilarityMatrix(t *testing.T) {
	testCases := []struct {
		name          string
		X             [][]float32
		Y             [][]float32
		expected      []float32
		expectedError bool
	}{
		{
			name: "valid matrices",
			X: [][]float32{
				{1.0, 2.0, 3.0},
				{4.0, 5.0, 6.0},
			},
			Y: [][]float32{
				{1.0, 2.0, 3.0},
				{7.0, 8.0, 9.0},
			},
			expected:      []float32{1.0, 0.9982}, // actual calculated value ≈0.9982
			expectedError: false,
		},
		{
			name: "row count mismatch",
			X: [][]float32{
				{1.0, 2.0, 3.0},
				{4.0, 5.0, 6.0},
			},
			Y: [][]float32{
				{1.0, 2.0, 3.0},
			},
			expected:      nil,
			expectedError: true,
		},
		{
			name: "column count mismatch",
			X: [][]float32{
				{1.0, 2.0, 3.0},
				{4.0, 5.0, 6.0},
			},
			Y: [][]float32{
				{1.0, 2.0},
				{4.0, 5.0},
			},
			expected:      nil,
			expectedError: true,
		},
		{
			name: "zero vectors",
			X: [][]float32{
				{0.0, 0.0, 0.0},
				{1.0, 2.0, 3.0},
			},
			Y: [][]float32{
				{1.0, 2.0, 3.0},
				{0.0, 0.0, 0.0},
			},
			expected:      []float32{0.0, 0.0},
			expectedError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := CosineSimilarityMatrix(tc.X, tc.Y)

			// Check error expectation
			if tc.expectedError && err == nil {
				t.Errorf("Expected error, got nil")
			}
			if !tc.expectedError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			// If no error is expected, check the results
			if !tc.expectedError {
				if len(result) != len(tc.expected) {
					t.Errorf("Expected %d results, got %d", len(tc.expected), len(result))
					return
				}

				for i := range result {
					if math.Abs(float64(result[i]-tc.expected[i])) > 0.0001 {
						t.Errorf("Result at index %d: expected %.4f, got %.4f",
							i, tc.expected[i], result[i])
					}
				}
			}
		})
	}
}
