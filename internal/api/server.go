package api

import (
	"encoding/json"
	"errors"
	"io"
	"museum-label-governance/internal/label"
	"museum-label-governance/internal/ledger"
	"museum-label-governance/internal/workflow"
	"net/http"
	"strconv"
	"strings"
)

type Server struct {
	svc              *workflow.Service
	copyReviewBuffer copyReviewReq
}

func New(s *workflow.Service) *Server { return &Server{svc: s} }
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.Health)
	mux.HandleFunc("/api/v1/dossiers", s.Dossiers)
	mux.HandleFunc("/api/v1/dossiers/", s.DossierSubroute)
	mux.HandleFunc("/api/v1/credentials/", s.Credential)
	return security(withRequestID(withMaxBody(mux)))
}
func security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeErr(w, 405, "method_not_allowed", "只支持GET")
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ok"})
}
func decode(r *http.Request, v any) error {
	if r.ContentLength > 1<<20 {
		return errors.New("request too large")
	}
	if !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("content type must be application/json")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return errors.New("请求体只能包含一个JSON对象")
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
func expected(r *http.Request, body int) int {
	if v, err := strconv.Atoi(r.Header.Get("X-Expected-Version")); err == nil && v > 0 {
		return v
	}
	if body > 0 {
		return body
	}
	return 0
}
func actor(r *http.Request) string {
	v := r.Header.Get("X-Operator")
	if v == "" {
		v = "anonymous"
	}
	return v
}

type createReq struct {
	ExhibitionName string `json:"exhibitionName"`
	ObjectRef      string `json:"objectRef"`
	Title          string `json:"title"`
	Content        string `json:"content"`
	Owner          string `json:"owner"`
}

func (s *Server) Dossiers(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		s.listDossiers(w, r)
		return
	}
	if r.Method != "POST" {
		methodAllowed(w, "GET, POST")
		return
	}
	var q createReq
	if e := decode(r, &q); e != nil {
		writeErr(w, 400, "invalid_request", e.Error())
		return
	}
	d, e := s.svc.CreateDossier(q.ExhibitionName, q.ObjectRef, q.Title, q.Content, q.Owner)
	if e != nil {
		writeErr(w, 400, "validation_error", "案卷字段不完整")
		return
	}
	writeJSON(w, 201, map[string]any{"dossier": d, "revision": 1})
}

func (s *Server) listDossiers(w http.ResponseWriter, r *http.Request) {
	allowed := map[string]bool{"status": true, "exhibitionName": true, "owner": true, "limit": true, "cursor": true}
	for key, values := range r.URL.Query() {
		if !allowed[key] || len(values) != 1 {
			writeErr(w, 400, "invalid_query", "包含不支持或重复的查询参数")
			return
		}
	}
	limit, err := parseLimit(r)
	if err != nil {
		writeErr(w, 400, "validation_error", err.Error())
		return
	}
	page, err := s.svc.ListDossiers(workflow.DossierQuery{Status: label.Status(r.URL.Query().Get("status")), ExhibitionName: r.URL.Query().Get("exhibitionName"), Owner: r.URL.Query().Get("owner"), Limit: limit, Cursor: r.URL.Query().Get("cursor"), Actor: actor(r)})
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, 200, page)
}
func (s *Server) DossierSubroute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/dossiers/"), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeErr(w, 404, "not_found", "资源不存在")
		return
	}
	did := parts[0]
	if len(parts) == 1 {
		if r.Method == "GET" {
			a, e := s.svc.GetAudit(did)
			if e != nil {
				writeErr(w, 404, "not_found", "案卷不存在")
				return
			}
			writeJSON(w, 200, a)
			return
		}
		if r.Method == "PATCH" {
			s.patchDossier(w, r, did)
			return
		}
		methodAllowed(w, "GET, PATCH")
		return
	}
	if len(parts) < 2 {
		writeErr(w, 404, "not_found", "资源不存在")
		return
	}
	switch parts[1] {
	case "claims":
		if len(parts) == 4 && parts[3] == "evidence" {
			s.claimEvidence(w, r, did, parts[2])
			return
		}
		if len(parts) != 2 {
			writeErr(w, 404, "not_found", "资源不存在")
			return
		}
		s.claims(w, r, did)
	case "evidence":
		if len(parts) == 2 {
			s.evidence(w, r, did)
			return
		}
		if len(parts) == 4 && parts[3] == "supersede" {
			s.supersedeEvidence(w, r, did, parts[2])
			return
		}
		if len(parts) == 4 && parts[3] == "usage" {
			s.evidenceUsage(w, r, did, parts[2])
			return
		}
		notFound(w)
	case "precheck":
		if len(parts) != 2 {
			notFound(w)
			return
		}
		s.precheck(w, r, did)
	case "expert-review":
		if len(parts) == 2 {
			s.expert(w, r, did)
			return
		}
		if len(parts) == 4 && parts[2] == "drafts" {
			s.expertDraft(w, r, did, parts[3])
			return
		}
		if len(parts) == 3 && parts[2] == "finalize" {
			s.expertFinalize(w, r, did)
			return
		}
		notFound(w)
	case "prechecks":
		if len(parts) == 2 {
			s.precheckHistory(w, r, did)
			return
		}
		if len(parts) == 3 && parts[2] == "diff" {
			s.precheckDiff(w, r, did)
			return
		}
		notFound(w)
	case "copy-suggestions":
		if len(parts) == 2 {
			s.copySuggestions(w, r, did)
			return
		}
		if len(parts) == 3 {
			s.disposeCopySuggestion(w, r, did, parts[2])
			return
		}
		notFound(w)
	case "copy-review":
		if len(parts) != 2 {
			notFound(w)
			return
		}
		s.copy(w, r, did)
	case "freeze":
		if len(parts) != 2 {
			notFound(w)
			return
		}
		s.freeze(w, r, did)
	case "issue":
		if len(parts) != 2 {
			notFound(w)
			return
		}
		s.issue(w, r, did)
	case "revise":
		if len(parts) != 2 {
			notFound(w)
			return
		}
		s.revise(w, r, did)
	case "revisions":
		if len(parts) == 3 && parts[2] == "diff" {
			s.revisionDiff(w, r, did)
			return
		}
		writeErr(w, 404, "not_found", "资源不存在")
	default:
		writeErr(w, 404, "not_found", "资源不存在")
	}
}

type dossierPatchReq struct {
	ExpectedVersion int     `json:"expectedVersion"`
	ExhibitionName  *string `json:"exhibitionName"`
	ObjectRef       *string `json:"objectRef"`
	Title           *string `json:"title"`
	Owner           *string `json:"owner"`
}

func (s *Server) patchDossier(w http.ResponseWriter, r *http.Request, did string) {
	var q dossierPatchReq
	if err := decode(r, &q); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	dossier, err := s.svc.UpdateDossier(did, expected(r, q.ExpectedVersion), workflow.DossierPatch{ExhibitionName: q.ExhibitionName, ObjectRef: q.ObjectRef, Title: q.Title, Owner: q.Owner}, actor(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"dossier": dossier})
}

func notFound(w http.ResponseWriter) {
	writeErr(w, 404, "not_found", "资源不存在")
}

type claimReq struct {
	ExpectedVersion int      `json:"expectedVersion"`
	Statement       string   `json:"statement"`
	Category        string   `json:"category"`
	EvidenceIDs     []string `json:"evidenceIds"`
}

func (s *Server) claims(w http.ResponseWriter, r *http.Request, did string) {
	if r.Method != "POST" {
		writeErr(w, 405, "method_not_allowed", "只支持POST")
		return
	}
	var q claimReq
	if e := decode(r, &q); e != nil {
		writeErr(w, 400, "invalid_request", e.Error())
		return
	}
	c, d, e := s.svc.CreateClaimWithEvidence(did, expected(r, q.ExpectedVersion), q.Statement, q.Category, q.EvidenceIDs, actor(r))
	if e != nil {
		mapErr(w, e)
		return
	}
	writeJSON(w, 201, map[string]any{"claim": c, "dossier": d})
}

type linkEvidenceReq struct {
	ExpectedVersion int      `json:"expectedVersion"`
	EvidenceIDs     []string `json:"evidenceIds"`
}

func (s *Server) claimEvidence(w http.ResponseWriter, r *http.Request, did, claimID string) {
	if r.Method != "PUT" && r.Method != "PATCH" {
		methodAllowed(w, "PUT, PATCH")
		return
	}
	var q linkEvidenceReq
	if err := decode(r, &q); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	claim, dossier, err := s.svc.ReplaceEvidenceLinks(did, claimID, expected(r, q.ExpectedVersion), q.EvidenceIDs, actor(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"claim": claim, "dossier": dossier})
}

type evidenceReq struct {
	ExpectedVersion int    `json:"expectedVersion"`
	SourceType      string `json:"sourceType"`
	Citation        string `json:"citation"`
	Locator         string `json:"locator"`
	Excerpt         string `json:"excerpt"`
	ReliabilityNote string `json:"reliabilityNote"`
}

func (s *Server) evidence(w http.ResponseWriter, r *http.Request, did string) {
	if r.Method != "POST" {
		writeErr(w, 405, "method_not_allowed", "只支持POST")
		return
	}
	var q evidenceReq
	if e := decode(r, &q); e != nil {
		writeErr(w, 400, "invalid_request", e.Error())
		return
	}
	evi, e := s.svc.AddEvidence(did, expected(r, q.ExpectedVersion), q.SourceType, q.Citation, q.Locator, q.Excerpt, q.ReliabilityNote, actor(r))
	if e != nil {
		mapErr(w, e)
		return
	}
	writeJSON(w, 201, map[string]any{"evidence": evi})
}

type supersedeEvidenceReq struct {
	ExpectedVersion       int    `json:"expectedVersion"`
	ReplacementEvidenceID string `json:"replacementEvidenceId"`
	Reason                string `json:"reason"`
}

func (s *Server) supersedeEvidence(w http.ResponseWriter, r *http.Request, did, evidenceID string) {
	if r.Method != "POST" {
		methodAllowed(w, "POST")
		return
	}
	var q supersedeEvidenceReq
	if err := decode(r, &q); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	dossier, evidence, err := s.svc.SupersedeEvidence(did, evidenceID, q.ReplacementEvidenceID, q.Reason, expected(r, q.ExpectedVersion), actor(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"dossier": dossier, "evidence": evidence})
}

func (s *Server) evidenceUsage(w http.ResponseWriter, r *http.Request, did, evidenceID string) {
	if r.Method != "GET" {
		methodAllowed(w, "GET")
		return
	}
	if len(r.URL.Query()) != 0 {
		writeErr(w, 400, "invalid_query", "该接口不支持查询参数")
		return
	}
	usage, err := s.svc.EvidenceUsage(did, evidenceID)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, 200, usage)
}

type decisionReq struct {
	ClaimID  string `json:"claimId"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type versionReq struct {
	ExpectedVersion int `json:"expectedVersion"`
}

type expertReviewReq struct {
	ExpectedVersion int           `json:"expectedVersion"`
	ClaimID         string        `json:"claimId"`
	Decision        string        `json:"decision"`
	Reason          string        `json:"reason"`
	Decisions       []decisionReq `json:"decisions"`
}

type copyReviewReq struct {
	ExpectedVersion int                    `json:"expectedVersion"`
	Decision        string                 `json:"decision"`
	Reason          string                 `json:"reason"`
	Suggestions     []label.CopySuggestion `json:"suggestions"`
}

type issueReq struct {
	ExpectedVersion int    `json:"expectedVersion"`
	SnapshotID      string `json:"snapshotId"`
}

func (s *Server) precheck(w http.ResponseWriter, r *http.Request, did string) {
	if r.Method != "POST" {
		writeErr(w, 405, "method_not_allowed", "只支持POST")
		return
	}
	var q versionReq
	if e := decode(r, &q); e != nil {
		writeErr(w, 400, "invalid_request", e.Error())
		return
	}
	result, e := s.svc.RunPrecheckWithKey(did, expected(r, q.ExpectedVersion), actor(r), idempotencyKey(r))
	if e != nil {
		mapErr(w, e)
		return
	}
	code := 200
	if len(result.Problems) > 0 {
		code = 422
	}
	writeJSON(w, code, result)
}
func (s *Server) expert(w http.ResponseWriter, r *http.Request, did string) {
	if r.Method == "GET" {
		progress, err := s.svc.ExpertReviewProgress(did)
		if err != nil {
			mapErr(w, err)
			return
		}
		writeJSON(w, 200, progress)
		return
	}
	if r.Method != "POST" {
		methodAllowed(w, "GET, POST")
		return
	}
	if strings.TrimSpace(r.Header.Get("X-Operator")) == "" {
		writeErr(w, 400, "validation_error", "X-Operator不能为空")
		return
	}
	var q expertReviewReq
	if e := decode(r, &q); e != nil {
		writeErr(w, 400, "invalid_request", e.Error())
		return
	}
	reviews := make([]workflow.ReviewInput, 0, len(q.Decisions))
	for _, item := range q.Decisions {
		reviews = append(reviews, workflow.ReviewInput{ClaimID: item.ClaimID, Decision: item.Decision, Reason: item.Reason, Actor: actor(r)})
	}
	if len(reviews) == 0 && q.ClaimID != "" {
		reviews = append(reviews, workflow.ReviewInput{ClaimID: q.ClaimID, Decision: q.Decision, Reason: q.Reason, Actor: actor(r)})
	}
	result, e := s.svc.ReviewClaimsBatch(did, expected(r, q.ExpectedVersion), reviews, actor(r), idempotencyKey(r))
	if e != nil {
		mapErr(w, e)
		return
	}
	writeJSON(w, 200, result)
}

type expertDraftReq struct {
	ExpectedVersion int    `json:"expectedVersion"`
	Decision        string `json:"decision"`
	Reason          string `json:"reason"`
}

func requireOperator(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("X-Operator")) == "" {
		writeErr(w, 400, "validation_error", "X-Operator不能为空")
		return false
	}
	return true
}

func (s *Server) expertDraft(w http.ResponseWriter, r *http.Request, did, claimID string) {
	if r.Method != "PUT" {
		methodAllowed(w, "PUT")
		return
	}
	if !requireOperator(w, r) {
		return
	}
	var q expertDraftReq
	if err := decode(r, &q); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	result, err := s.svc.SaveExpertReviewDraft(did, claimID, expected(r, q.ExpectedVersion), q.Decision, q.Reason, actor(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (s *Server) expertFinalize(w http.ResponseWriter, r *http.Request, did string) {
	if r.Method != "POST" {
		methodAllowed(w, "POST")
		return
	}
	if !requireOperator(w, r) {
		return
	}
	var q versionReq
	if err := decode(r, &q); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	result, err := s.svc.FinalizeExpertReview(did, expected(r, q.ExpectedVersion), actor(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (s *Server) precheckHistory(w http.ResponseWriter, r *http.Request, did string) {
	if r.Method != "GET" {
		methodAllowed(w, "GET")
		return
	}
	for key, values := range r.URL.Query() {
		if (key != "revisionNo" && key != "limit") || len(values) != 1 {
			writeErr(w, 400, "invalid_query", "包含不支持或重复的查询参数")
			return
		}
	}
	revisionNo := 0
	if raw := r.URL.Query().Get("revisionNo"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			writeErr(w, 400, "validation_error", "revisionNo必须为正整数")
			return
		}
		revisionNo = value
	}
	limit, err := parseLimit(r)
	if err != nil {
		writeErr(w, 400, "validation_error", err.Error())
		return
	}
	history, err := s.svc.PrecheckHistory(did, revisionNo, limit, actor(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, 200, history)
}

func (s *Server) precheckDiff(w http.ResponseWriter, r *http.Request, did string) {
	if r.Method != "GET" {
		methodAllowed(w, "GET")
		return
	}
	for key, values := range r.URL.Query() {
		if (key != "fromVersion" && key != "toVersion") || len(values) != 1 {
			writeErr(w, 400, "invalid_query", "包含不支持或重复的查询参数")
			return
		}
	}
	from, err1 := strconv.Atoi(r.URL.Query().Get("fromVersion"))
	to, err2 := strconv.Atoi(r.URL.Query().Get("toVersion"))
	if err1 != nil || err2 != nil || from < 1 || to < 1 {
		writeErr(w, 400, "validation_error", "fromVersion和toVersion必须为正整数")
		return
	}
	diff, err := s.svc.DiffPrechecks(did, from, to, actor(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, 200, diff)
}
func (s *Server) copy(w http.ResponseWriter, r *http.Request, did string) {
	if r.Method != "POST" {
		writeErr(w, 405, "method_not_allowed", "只支持POST")
		return
	}
	q := &s.copyReviewBuffer
	if e := decode(r, q); e != nil {
		writeErr(w, 400, "invalid_request", e.Error())
		return
	}
	result, e := s.svc.CopyReviewDetailed(did, expected(r, q.ExpectedVersion), workflow.CopyReviewInput{Decision: q.Decision, Reason: q.Reason, Actor: actor(r), Suggestions: q.Suggestions})
	if e != nil {
		mapErr(w, e)
		return
	}
	writeJSON(w, 200, result)
}

type disposeSuggestionReq struct {
	ExpectedVersion int    `json:"expectedVersion"`
	Disposition     string `json:"disposition"`
	Note            string `json:"note"`
}

func (s *Server) copySuggestions(w http.ResponseWriter, r *http.Request, did string) {
	if r.Method != "GET" {
		methodAllowed(w, "GET")
		return
	}
	for key, values := range r.URL.Query() {
		if key != "status" || len(values) != 1 {
			writeErr(w, 400, "invalid_query", "包含不支持或重复的查询参数")
			return
		}
	}
	result, err := s.svc.CopySuggestions(did, r.URL.Query().Get("status"))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (s *Server) disposeCopySuggestion(w http.ResponseWriter, r *http.Request, did, suggestionID string) {
	if r.Method != "PATCH" {
		methodAllowed(w, "PATCH")
		return
	}
	var q disposeSuggestionReq
	if err := decode(r, &q); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	dossier, suggestion, err := s.svc.DisposeCopySuggestion(did, suggestionID, expected(r, q.ExpectedVersion), q.Disposition, q.Note, actor(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"dossier": dossier, "suggestion": suggestion})
}
func (s *Server) freeze(w http.ResponseWriter, r *http.Request, did string) {
	if r.Method != "POST" {
		writeErr(w, 405, "method_not_allowed", "只支持POST")
		return
	}
	if strings.TrimSpace(r.Header.Get("X-Operator")) == "" {
		writeErr(w, 400, "validation_error", "X-Operator不能为空")
		return
	}
	var q versionReq
	if e := decode(r, &q); e != nil {
		writeErr(w, 400, "invalid_request", e.Error())
		return
	}
	snap, replay, e := s.svc.FreezeDetailed(did, expected(r, q.ExpectedVersion), actor(r))
	if e != nil {
		mapErr(w, e)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"snapshot": snap})
}
func (s *Server) issue(w http.ResponseWriter, r *http.Request, did string) {
	if r.Method != "POST" {
		writeErr(w, 405, "method_not_allowed", "只支持POST")
		return
	}
	if strings.TrimSpace(r.Header.Get("X-Operator")) == "" {
		writeErr(w, 400, "validation_error", "X-Operator不能为空")
		return
	}
	var q issueReq
	if e := decode(r, &q); e != nil {
		writeErr(w, 400, "invalid_request", e.Error())
		return
	}
	c, replay, e := s.svc.IssueWithKey(did, expected(r, q.ExpectedVersion), q.SnapshotID, actor(r), idempotencyKey(r))
	if e != nil {
		mapErr(w, e)
		return
	}
	status := 201
	if replay {
		status = 200
	}
	writeJSON(w, status, map[string]any{"credential": c, "digestVerified": true})
}

type reviseReq struct {
	ExpectedVersion  int      `json:"expectedVersion"`
	Content          string   `json:"content"`
	AffectedClaimIDs []string `json:"affectedClaimIds"`
}

func (s *Server) revise(w http.ResponseWriter, r *http.Request, did string) {
	if r.Method != "POST" {
		writeErr(w, 405, "method_not_allowed", "只支持POST")
		return
	}
	var q reviseReq
	if e := decode(r, &q); e != nil {
		writeErr(w, 400, "invalid_request", e.Error())
		return
	}
	d, diff, e := s.svc.ReviseDetailed(did, expected(r, q.ExpectedVersion), q.Content, q.AffectedClaimIDs, actor(r))
	if e != nil {
		mapErr(w, e)
		return
	}
	writeJSON(w, 201, map[string]any{"dossier": d, "diff": diff})
}

func (s *Server) revisionDiff(w http.ResponseWriter, r *http.Request, did string) {
	if r.Method != "GET" {
		methodAllowed(w, "GET")
		return
	}
	for key, values := range r.URL.Query() {
		if (key != "from" && key != "to") || len(values) != 1 {
			writeErr(w, 400, "invalid_query", "包含不支持或重复的查询参数")
			return
		}
	}
	from, err1 := strconv.Atoi(r.URL.Query().Get("from"))
	to, err2 := strconv.Atoi(r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil {
		writeErr(w, 400, "validation_error", "from和to必须为修订号")
		return
	}
	diff, err := s.svc.RevisionDiff(did, from, to)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"diff": diff})
}
func (s *Server) Credential(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeErr(w, 405, "method_not_allowed", "只支持GET")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/credentials/"), "/"), "/")
	if len(parts) == 2 && parts[1] == "verify" {
		if !requireOperator(w, r) {
			return
		}
		report, err := s.svc.VerifyCredential(parts[0], actor(r))
		if err != nil {
			mapErr(w, err)
			return
		}
		writeJSON(w, 200, report)
		return
	}
	if len(parts) != 1 || parts[0] == "" {
		notFound(w)
		return
	}
	no := parts[0]
	view, e := s.svc.GetCredentialView(no)
	if e != nil {
		writeErr(w, 404, "not_found", "凭据不存在")
		return
	}
	writeJSON(w, 200, view)
}
func mapErr(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, ledger.ErrNotFound):
		writeErr(w, 404, "not_found", "资源不存在")
	case errors.Is(e, ledger.ErrConflict):
		writeErr(w, 409, "version_conflict", "版本冲突")
	case errors.Is(e, workflow.ErrState):
		writeErr(w, 409, "invalid_state", "当前状态不允许该操作")
	case errors.Is(e, workflow.ErrValidation):
		writeErr(w, 400, "validation_error", "请求校验失败")
	case errors.Is(e, workflow.ErrIdempotency):
		writeErr(w, 409, "idempotency_conflict", "幂等键已用于不同请求")
	case errors.Is(e, workflow.ErrIntegrity):
		writeErr(w, 409, "snapshot_integrity_error", "冻结快照完整性校验失败")
	case errors.Is(e, workflow.ErrImmutable), errors.Is(e, ledger.ErrImmutable):
		writeErr(w, 409, "immutable_resource", "资源冻结后不可修改")
	default:
		writeErr(w, 500, "internal_error", e.Error())
	}
}
