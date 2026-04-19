package secrets

import (
	"sort"

	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
)

type SecretDetector struct {
	patterns     []patternSpec
	enabled      map[EntityType]bool
	maxScanChars int
}

type DetectResult struct {
	Detected  bool
	Matches   []EntityMatch
	Locations []masking.SecretLocation
}

type EntityMatch struct {
	Type  EntityType
	Count int
}

func NewDetector(enabledTypes []string, maxScanChars int) *SecretDetector {
	enabled := make(map[EntityType]bool)
	for _, t := range enabledTypes {
		enabled[EntityType(t)] = true
	}
	return &SecretDetector{
		patterns:     allPatterns,
		enabled:      enabled,
		maxScanChars: maxScanChars,
	}
}

func DefaultDetector() *SecretDetector {
	return NewDetector([]string{
		string(EntityOpenSSHKey),
		string(EntityPEMKey),
		string(EntityAPIKeySK),
		string(EntityAPIKeyAWS),
		string(EntityAPIKeyGitHub),
		string(EntityAPIKeyGitLab),
		string(EntityJWTToken),
		string(EntityBearerToken),
		string(EntityThaiID),
	}, 200000)
}

func (d *SecretDetector) Detect(text string) DetectResult {
	scanText := text
	if d.maxScanChars > 0 && len(scanText) > d.maxScanChars {
		scanText = text[:d.maxScanChars]
	}

	var matches []EntityMatch
	matchIndex := make(map[EntityType]int)
	var locations []masking.SecretLocation
	matchedPositions := make(map[int]struct{})

	for _, p := range d.patterns {
		if !d.enabled[p.entityType] {
			continue
		}

		found := p.regex.FindAllStringIndex(scanText, -1)
		if len(found) == 0 {
			continue
		}

		count := 0
		for _, loc := range found {
			if _, dup := matchedPositions[loc[0]]; dup {
				continue
			}
			matchedPositions[loc[0]] = struct{}{}
			locations = append(locations, masking.SecretLocation{
				Start: loc[0],
				End:   loc[1],
				Type:  string(p.entityType),
			})
			count++
		}

		if count > 0 {
			matchIndex[p.entityType] += count
		}
	}

	if len(matchIndex) == 0 {
		return DetectResult{}
	}

	for et, count := range matchIndex {
		matches = append(matches, EntityMatch{Type: et, Count: count})
	}

	// Sort locations by start DESC for safe backward replacement.
	sort.Slice(locations, func(i, j int) bool {
		return locations[i].Start > locations[j].Start
	})

	return DetectResult{
		Detected:  true,
		Matches:   matches,
		Locations: locations,
	}
}
