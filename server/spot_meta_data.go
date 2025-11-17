package server

import (
	"encoding/json"
	"fmt"
	"sync"

	_ "embed"
)

//go:embed testdata/spot_meta.json
var recordedSpotMetaJSON []byte

//go:embed testdata/spot_meta_and_asset_ctxs.json
var recordedSpotMetaAndAssetCtxsJSON []byte

var (
	spotFixturesOnce sync.Once

	recordedSpotMeta           SpotMeta
	recordedSpotMetaFound      bool
	recordedSpotMetaLoadErr    error
	recordedSpotMetaAndCtxs    SpotMetaAndAssetCtxs
	recordedSpotMetaAndFound   bool
	recordedSpotMetaAndLoadErr error
)

func ensureSpotFixtures() {
	spotFixturesOnce.Do(func() {
		if err := json.Unmarshal(recordedSpotMetaJSON, &recordedSpotMeta); err != nil {
			recordedSpotMetaLoadErr = fmt.Errorf("failed to decode embedded spotMeta fixture: %w", err)
		} else {
			recordedSpotMetaFound = true
		}

		if err := json.Unmarshal(recordedSpotMetaAndAssetCtxsJSON, &recordedSpotMetaAndCtxs); err != nil {
			recordedSpotMetaAndLoadErr = fmt.Errorf("failed to decode embedded spotMetaAndAssetCtxs fixture: %w", err)
		} else {
			recordedSpotMetaAndFound = true
		}
	})
}

func getRecordedSpotMeta() (SpotMeta, error) {
	ensureSpotFixtures()
	if !recordedSpotMetaFound {
		return SpotMeta{}, recordedSpotMetaLoadErr
	}
	return recordedSpotMeta, nil
}

func getRecordedSpotMetaAndAssetCtxs() (SpotMetaAndAssetCtxs, error) {
	ensureSpotFixtures()
	if !recordedSpotMetaAndFound {
		return SpotMetaAndAssetCtxs{}, recordedSpotMetaAndLoadErr
	}
	return recordedSpotMetaAndCtxs, nil
}
