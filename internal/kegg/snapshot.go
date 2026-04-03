package kegg

import (
	"strings"
	"time"
)

const keggSnapshotVersionLayout = "2006-01"

func deriveKEGGSnapshotVersionToken(timeStarted time.Time) string {
	return timeStarted.Format(keggSnapshotVersionLayout)
}

func isValidKEGGSnapshotVersionToken(value string) bool {
	text := strings.TrimSpace(value)
	if text == "" {
		return false
	}
	timeParsed, err := time.Parse(keggSnapshotVersionLayout, text)
	if err != nil {
		return false
	}
	return timeParsed.Format(keggSnapshotVersionLayout) == text
}

func deriveKEGGReleaseFields(
	sourceRelease string,
	sourceReleaseStart string,
	sourceReleaseEnd string,
) (string, string, string) {
	valueStart := firstNonEmpty(sourceReleaseStart, sourceRelease)
	valueEnd := firstNonEmpty(sourceReleaseEnd, valueStart, sourceRelease)
	valueCompat := firstNonEmpty(sourceRelease, valueStart)
	return valueCompat, valueStart, valueEnd
}
