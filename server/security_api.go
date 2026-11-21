package server

import "net/http"

// securityAuditVerify 校验 host_ledger 哈希链完整性（P2-13 审计完整性）。
// 返回 ok/total/hashed/broken_at；broken_at 精确定位第一条被篡改或断链的行。
func (s *Server) securityAuditVerify(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	res, err := pg.VerifyHostLedger()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
