package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"museum-label-governance/internal/api"
	"museum-label-governance/internal/ledger"
	"museum-label-governance/internal/workflow"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:19081", "监听地址")
	self := flag.Bool("selfcheck", false, "运行回环自检")
	data := flag.String("data", ".benzhi/ledger.json", "账本路径")
	flag.Parse()
	resolved, err := resolveAddr(*addr)
	if err != nil {
		log.Fatal(err)
	}
	st, err := ledger.New(*data)
	if err != nil {
		log.Fatal(err)
	}
	if err = st.Migrate(); err != nil {
		if errors.Is(err, ledger.ErrIntegrity) {
			log.Fatalf("启动中止：账本完整性校验失败，已拒绝监听：%v", err)
		}
		log.Fatal(err)
	}
	srv := &http.Server{Addr: resolved, Handler: api.New(workflow.New(st)).Handler(), ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second}
	if *self {
		if err := runSelfcheck(srv, resolved); err != nil {
			log.Fatal(err)
		}
		return
	}
	ln, err := net.Listen("tcp", resolved)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("服务监听 %s", resolved)
	errs := make(chan error, 1)
	go func() { errs <- srv.Serve(ln) }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errs:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	case <-signals:
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("关闭服务失败: %v", err)
		}
	}
}
func resolveAddr(addr string) (string, error) {
	if p := os.Getenv("PORT"); p != "" {
		addr = "127.0.0.1:" + p
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		return "", fmt.Errorf("仅允许回环地址")
	}
	host, portText, e := net.SplitHostPort(addr)
	if e != nil {
		return "", fmt.Errorf("非法监听地址: %w", e)
	}
	if host != "127.0.0.1" {
		return "", fmt.Errorf("仅允许回环地址")
	}
	port, e := strconv.Atoi(portText)
	if e != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("非法监听端口")
	}
	return addr, nil
}
func runSelfcheck(srv *http.Server, addr string) error {
	ln, e := net.Listen("tcp", addr)
	if e != nil {
		return e
	}
	go srv.Serve(ln)
	base := "http://" + addr
	defer srv.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	var out map[string]any
	post := func(path string, body any) (map[string]any, error) {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", base+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Operator", "selfcheck")
		resp, e := client.Do(req)
		if e != nil {
			return nil, e
		}
		defer resp.Body.Close()
		rb, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("%s: %s", resp.Status, rb)
		}
		var m map[string]any
		_ = json.Unmarshal(rb, &m)
		return m, nil
	}
	for i := 0; i < 20; i++ {
		resp, e := client.Get(base + "/healthz")
		if e == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	out, e = post("/api/v1/dossiers", map[string]any{"exhibitionName": "青铜文明", "objectRef": "OBJ-001", "title": "青铜器", "content": "商代青铜器。", "owner": "editor"})
	if e != nil {
		return e
	}
	d := out["dossier"].(map[string]any)
	did := d["id"].(string)
	ver := int(d["version"].(float64))
	out, e = post("/api/v1/dossiers/"+did+"/evidence", map[string]any{"expectedVersion": ver, "sourceType": "catalog", "citation": "馆藏图录", "locator": "第12页", "excerpt": "商代青铜器", "reliabilityNote": "馆藏记录"})
	if e != nil {
		return e
	}
	ev := out["evidence"].(map[string]any)
	ver++
	out, e = post("/api/v1/dossiers/"+did+"/claims", map[string]any{"expectedVersion": ver, "statement": "该器物为商代青铜器", "category": "年代", "evidenceIds": []string{ev["id"].(string)}})
	if e != nil {
		return e
	}
	cl := out["claim"].(map[string]any)
	ver++
	out, e = post("/api/v1/dossiers/"+did+"/precheck", map[string]any{"expectedVersion": ver})
	if e != nil {
		return e
	}
	ver++
	_, e = post("/api/v1/dossiers/"+did+"/expert-review", map[string]any{"expectedVersion": ver, "claimId": cl["id"].(string), "decision": "pass", "reason": "来源充分"})
	if e != nil {
		return e
	}
	ver++
	_, e = post("/api/v1/dossiers/"+did+"/copy-review", map[string]any{"expectedVersion": ver, "decision": "pass"})
	if e != nil {
		return e
	}
	ver++
	out, e = post("/api/v1/dossiers/"+did+"/freeze", map[string]any{"expectedVersion": ver})
	if e != nil {
		return e
	}
	snap := out["snapshot"].(map[string]any)
	ver++
	issue := func() (map[string]any, int, error) {
		b, _ := json.Marshal(map[string]any{"expectedVersion": ver, "snapshotId": snap["id"].(string)})
		req, _ := http.NewRequest("POST", base+"/api/v1/dossiers/"+did+"/issue", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Operator", "selfcheck")
		req.Header.Set("Idempotency-Key", "selfcheck-issue")
		resp, requestErr := client.Do(req)
		if requestErr != nil {
			return nil, 0, requestErr
		}
		rb, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return nil, resp.StatusCode, fmt.Errorf("%s: %s", resp.Status, rb)
		}
		result := map[string]any{}
		if err := json.Unmarshal(rb, &result); err != nil {
			return nil, resp.StatusCode, err
		}
		return result, resp.StatusCode, nil
	}
	issued, status, e := issue()
	if e != nil || status != http.StatusCreated {
		return fmt.Errorf("首次签发失败: status=%d err=%v", status, e)
	}
	cred := issued["credential"].(map[string]any)
	if cred["signature"] == "" {
		return fmt.Errorf("签名为空")
	}
	replayed, status, e := issue()
	if e != nil || status != http.StatusOK || replayed["credential"].(map[string]any)["credentialNo"] != cred["credentialNo"] {
		return fmt.Errorf("签发重放失败: status=%d err=%v", status, e)
	}
	resp, e := client.Get(base + "/api/v1/credentials/" + cred["credentialNo"].(string))
	if e != nil {
		return e
	}
	rb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("凭据查询失败: %s: %s", resp.Status, rb)
	}
	view := map[string]any{}
	if e = json.Unmarshal(rb, &view); e != nil {
		return e
	}
	if view["snapshot"].(map[string]any)["id"] != snap["id"] || !containsAudit(view["audits"].([]any), "credential.issued") {
		return fmt.Errorf("凭据查询缺少快照或签发审计")
	}
	return nil
}

func containsAudit(items []any, action string) bool {
	for _, item := range items {
		if event, ok := item.(map[string]any); ok && event["action"] == action {
			return true
		}
	}
	return false
}
