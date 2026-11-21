package db

import "encoding/json"

// 资产蜜罐评分的读写（M1-04）。列由迁移 0003 添加；评分为 0 表示未命中/未评估。

// SetHoneypotScore 写入（或提升）资产的蜜罐评分与证据。
// 只在评分不低于现有值时覆盖：多次评估取最高分，证据随最高分一起更新。
func (s *AssetStore) SetHoneypotScore(assetID int64, score float64, evidence []string) error {
	if assetID <= 0 || score <= 0 {
		return nil
	}
	if evidence == nil {
		evidence = []string{}
	}
	_, err := s.db.Exec(`UPDATE assets
		SET honeypot_score = GREATEST(honeypot_score, $2),
		    honeypot_evidence = CASE WHEN $2 >= honeypot_score THEN $3 ELSE honeypot_evidence END
		WHERE id = $1`, assetID, score, evidence)
	return err
}

// GetHoneypotScore 读取资产的蜜罐评分与证据，供测试与运维核对。
func (s *AssetStore) GetHoneypotScore(assetID int64) (float64, []string, error) {
	var score float64
	var evidenceJSON string
	// pgx database/sql 驱动把 TEXT[] 作为 string 返回，不能直接 Scan 进 []string；
	// 这里统一转 JSON 文本再解码，避免依赖数组扫描能力。
	err := s.db.QueryRow(`SELECT honeypot_score, COALESCE(array_to_json(honeypot_evidence), '[]')::text FROM assets WHERE id = $1`, assetID).Scan(&score, &evidenceJSON)
	if err != nil {
		return 0, nil, err
	}
	var evidence []string
	if err := json.Unmarshal([]byte(evidenceJSON), &evidence); err != nil {
		return score, nil, err
	}
	return score, evidence, nil
}
