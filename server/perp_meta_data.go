package server

import (
	"encoding/json"
	"fmt"
	"sync"

	_ "embed"
)

//go:embed testdata/meta.json
var recordedMetaJSON []byte

//go:embed testdata/meta_and_asset_ctxs.json
var recordedMetaAndAssetCtxsJSON []byte

var (
	perpFixturesOnce sync.Once

	recordedMeta                Meta
	recordedMetaFound           bool
	recordedMetaLoadErr         error
	recordedMetaAndAssetCtxs    MetaAndAssetCtxs
	recordedMetaAndAssetFound   bool
	recordedMetaAndAssetLoadErr error
)

func ensurePerpFixtures() {
	perpFixturesOnce.Do(func() {
		if err := json.Unmarshal(recordedMetaJSON, &recordedMeta); err != nil {
			recordedMetaLoadErr = fmt.Errorf("failed to decode embedded meta fixture: %w", err)
		} else {
			recordedMetaFound = true
		}

		if err := json.Unmarshal(recordedMetaAndAssetCtxsJSON, &recordedMetaAndAssetCtxs); err != nil {
			recordedMetaAndAssetLoadErr = fmt.Errorf("failed to decode embedded metaAndAssetCtxs fixture: %w", err)
		} else {
			recordedMetaAndAssetFound = true
		}
	})
}

func getRecordedMeta() (Meta, error) {
	ensurePerpFixtures()
	if !recordedMetaFound {
		return Meta{}, recordedMetaLoadErr
	}
	return recordedMeta, nil
}

func getRecordedMetaAndAssetCtxs() (MetaAndAssetCtxs, error) {
	ensurePerpFixtures()
	if !recordedMetaAndAssetFound {
		return MetaAndAssetCtxs{}, recordedMetaAndAssetLoadErr
	}
	return recordedMetaAndAssetCtxs, nil
}
