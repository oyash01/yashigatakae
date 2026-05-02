package mempalace

import "math"

// cosine returns the cosine similarity between two equal-length vectors.
// Returns 0 if either vector is zero-length or all-zeros.
// Range: -1.0 (opposite) → 0.0 (orthogonal) → 1.0 (identical).
func cosine(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
