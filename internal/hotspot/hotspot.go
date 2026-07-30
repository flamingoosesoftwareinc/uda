package hotspot

// PackageScore holds the computed hotspot data for a single package.
type PackageScore struct {
	ChangeFreq   float64 `json:"ChangeFreq"`
	HotspotScore float64 `json:"HotspotScore"`
}

// ComputeScores computes hotspot scores for each package.
// hotspot = changeFreq * curve.Evaluate(instability).
func ComputeScores(
	instabilities map[string]float64,
	changeFreqs map[string]float64,
	curve *CatmullRomCurve,
) map[string]PackageScore {
	scores := make(map[string]PackageScore, len(instabilities))
	for pkg, instability := range instabilities {
		freq := changeFreqs[pkg]
		score := freq * curve.Evaluate(instability)
		scores[pkg] = PackageScore{
			ChangeFreq:   freq,
			HotspotScore: score,
		}
	}

	return scores
}

// ComputeChangeFrequencies computes change frequency as touchCount/totalCommits per package.
func ComputeChangeFrequencies(touchCounts map[string]int, totalCommits int) map[string]float64 {
	freqs := make(map[string]float64, len(touchCounts))
	if totalCommits == 0 {
		return freqs
	}

	for pkg, count := range touchCounts {
		freqs[pkg] = float64(count) / float64(totalCommits)
	}

	return freqs
}
