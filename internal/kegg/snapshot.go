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

func (cfg *pathwayConfig) applyKEGGInfoMetadataStart(metadata keggInfoMetadata) {
	cfg.sourceRelease = metadata.sourceRelease
	cfg.sourceReleaseStart = metadata.sourceRelease
	cfg.sourceLastUpdate = metadata.sourceLastUpdate
	cfg.sourceLastUpdateStart = metadata.sourceLastUpdate
}

func (cfg *pathwayConfig) applyKEGGInfoMetadataEnd(metadata keggInfoMetadata) {
	cfg.sourceReleaseEnd = metadata.sourceRelease
	cfg.sourceLastUpdateEnd = metadata.sourceLastUpdate
	logKEGGInfoMetadataChanges("pathway", cfg.sourceReleaseStart, cfg.sourceReleaseEnd, cfg.sourceLastUpdateStart, cfg.sourceLastUpdateEnd)
}

func (cfg *briteConfig) applyKEGGInfoMetadataStart(metadata keggInfoMetadata) {
	cfg.sourceRelease = metadata.sourceRelease
	cfg.sourceReleaseStart = metadata.sourceRelease
	cfg.sourceLastUpdate = metadata.sourceLastUpdate
	cfg.sourceLastUpdateStart = metadata.sourceLastUpdate
}

func (cfg *briteConfig) applyKEGGInfoMetadataEnd(metadata keggInfoMetadata) {
	cfg.sourceReleaseEnd = metadata.sourceRelease
	cfg.sourceLastUpdateEnd = metadata.sourceLastUpdate
	logKEGGInfoMetadataChanges("brite", cfg.sourceReleaseStart, cfg.sourceReleaseEnd, cfg.sourceLastUpdateStart, cfg.sourceLastUpdateEnd)
}

func (cfg *catalogConfig) applyKEGGInfoMetadataStart(metadata keggInfoMetadata) {
	cfg.sourceRelease = metadata.sourceRelease
	cfg.sourceReleaseStart = metadata.sourceRelease
	cfg.sourceLastUpdate = metadata.sourceLastUpdate
	cfg.sourceLastUpdateStart = metadata.sourceLastUpdate
}

func (cfg *catalogConfig) applyKEGGInfoMetadataEnd(metadata keggInfoMetadata) {
	cfg.sourceReleaseEnd = metadata.sourceRelease
	cfg.sourceLastUpdateEnd = metadata.sourceLastUpdate
	logKEGGInfoMetadataChanges("catalog", cfg.sourceReleaseStart, cfg.sourceReleaseEnd, cfg.sourceLastUpdateStart, cfg.sourceLastUpdateEnd)
}

func logKEGGInfoMetadataChanges(asset string, releaseStart string, releaseEnd string, lastUpdateStart string, lastUpdateEnd string) {
	if releaseStart != "" && releaseEnd != "" && releaseStart != releaseEnd {
		logf("KEGG %s release changed during download: start=%s end=%s", asset, releaseStart, releaseEnd)
	}
	if lastUpdateStart != "" && lastUpdateEnd != "" && lastUpdateStart != lastUpdateEnd {
		logf("KEGG %s last update changed during download: start=%s end=%s", asset, lastUpdateStart, lastUpdateEnd)
	}
}
