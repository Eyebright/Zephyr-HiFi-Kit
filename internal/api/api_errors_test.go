package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/commands"
	"hifihub/server/internal/library"
	"hifihub/server/internal/license"
	"hifihub/server/internal/node"
	"hifihub/server/internal/rollouts"
	"hifihub/server/internal/task"
	"hifihub/server/internal/update"
	"hifihub/server/internal/zatserver"
)

func errorBody(t *testing.T, w *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	if w.Code != status || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("response headers/status: %d %v", w.Code, w.Header())
	}
	body := mustJSON(t, w.Body.Bytes())
	trace, ok := body["trace_id"].(string)
	if !ok {
		t.Fatalf("missing trace_id: %s", w.Body.String())
	}
	if _, err := uuid.Parse(trace); err != nil {
		t.Fatalf("invalid trace_id %q: %v", trace, err)
	}
	return body
}

func TestWriteErrKeepsLegacyDiagnostics(t *testing.T) {
	w := httptest.NewRecorder()
	message := "原始 diagnostic <input>"
	detail := "stderr: 未翻译的路径 /音乐/a.flac"
	writeErr(w, http.StatusBadRequest, "artifact_rejected", message, detail)
	body := errorBody(t, w, http.StatusBadRequest)
	if body["code"] != "artifact_rejected" || body["message_code"] != "errors.artifact_rejected" || body["message"] != message || body["detail"] != detail {
		t.Fatalf("legacy envelope changed: %s", w.Body.String())
	}
	if _, ok := body["message_params"]; ok {
		t.Fatalf("empty params must be omitted: %s", w.Body.String())
	}
	w2 := httptest.NewRecorder()
	writeErr(w2, http.StatusBadRequest, "bad_json", "raw parser error", "")
	b2 := errorBody(t, w2, http.StatusBadRequest)
	if b2["trace_id"] == body["trace_id"] {
		t.Fatal("trace IDs must be unique per response")
	}
	if _, ok := b2["detail"]; ok {
		t.Fatal("empty detail must remain omitted")
	}
}

func TestWriteErrorAddsStableEnvelopeWithoutTranslating(t *testing.T) {
	for status, code := range map[int]string{
		400: "bad_request", 401: "unauthorized", 403: "forbidden", 404: "not_found",
		405: "method_not_allowed", 408: "request_timeout", 409: "conflict",
		413: "request_too_large", 415: "unsupported_media_type", 422: "unprocessable_entity",
		429: "too_many_requests", 500: "internal_error", 502: "bad_gateway",
		503: "service_unavailable", 504: "gateway_timeout", 507: "internal_error",
	} {
		t.Run(code+strconv.Itoa(status), func(t *testing.T) {
			w := httptest.NewRecorder()
			// Even text identical to a product message must not be used to infer a code.
			raw := "密码错误"
			writeError(w, status, raw)
			body := errorBody(t, w, status)
			if body["error"] != raw || body["message"] != raw || body["code"] != code || body["message_code"] != "errors."+code {
				t.Fatalf("wrong envelope: %s", w.Body.String())
			}
		})
	}
}

func TestWriteErrorPreservesEmptyErrorField(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusInternalServerError, "")
	body := errorBody(t, w, http.StatusInternalServerError)
	if body["error"] != "" || body["message"] != "" || body["message_code"] != "errors.internal_error" {
		t.Fatalf("empty legacy error field was lost: %s", w.Body.String())
	}
}

func TestWriteErrorCodeParamsArePlainData(t *testing.T) {
	w := httptest.NewRecorder()
	input := `<img src=x onerror=alert(1)> & 用户 {field}`
	writeErrorCode(w, http.StatusNotFound, "key_not_found", "密钥不存在："+input, map[string]any{"id": input})
	body := errorBody(t, w, http.StatusNotFound)
	if body["code"] != "key_not_found" || body["message_code"] != "errors.key_not_found" || body["error"] != "密钥不存在："+input {
		t.Fatalf("wrong parameter envelope: %s", w.Body.String())
	}
	if body["message_params"].(map[string]any)["id"] != input || strings.Contains(w.Body.String(), "<img") {
		t.Fatalf("input changed or not JSON-escaped: %s", w.Body.String())
	}
}

func TestServiceErrorsUseIdentityAndPreserveDiagnostics(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{catalog.ErrNotFound, "not_found"}, {license.ErrInactive, "license_required"},
		{task.ErrNodeUnauthorized, "node_unauthorized"}, {node.ErrPlaying, "node_playing"},
		{commands.ErrPlaying, "upgrade_playing"}, {commands.ErrPlayingInstall, "install_playing"},
		{update.ErrBusy, "update_busy"}, {rollouts.ErrRingBusy, "rollout_ring_busy"},
		{zatserver.ErrNotConnected, "zat_not_connected"}, {zatserver.ErrUnknownDevice, "unknown_audio_device"},
		{zatserver.ErrNoSession, "no_playback_session"}, {zatserver.ErrQueueEmpty, "queue_empty"},
		{zatserver.ErrQueueEnd, "queue_end"}, {zatserver.ErrQueueIndex, "queue_index_invalid"},
		{library.ErrNotMounted, "source_not_mounted"}, {library.ErrLibraryBusy, "library_busy"},
		{library.ErrInvalidLibraryBackup, "backup_invalid"}, {library.ErrInvalidLibraryCleanup, "cleanup_invalid"},
		{library.ErrLibraryMaintenance, "maintenance_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			err := fmt.Errorf("原始 diagnostic: %w", tc.err)
			w := httptest.NewRecorder()
			writeServiceError(w, http.StatusConflict, err)
			body := errorBody(t, w, http.StatusConflict)
			if body["error"] != err.Error() || body["message"] != err.Error() || body["code"] != tc.code || body["message_code"] != "errors."+tc.code {
				t.Fatalf("lost identity or diagnostics: %s", w.Body.String())
			}
			if got := serviceErrorCode(errors.New(tc.err.Error()), "fallback"); got != "fallback" {
				t.Fatalf("code inferred from diagnostic text: %s", got)
			}
		})
	}
	w := httptest.NewRecorder()
	err := fmt.Errorf("wrapped: %w", task.ErrNodeUnauthorized)
	fail(w, err)
	body := errorBody(t, w, http.StatusConflict)
	if body["code"] != "license_required" || body["message_code"] != "errors.node_unauthorized" || body["message"] != err.Error() {
		t.Fatalf("fail must preserve its legacy code: %s", w.Body.String())
	}
}

func TestBrowseValidationHasSemanticParams(t *testing.T) {
	for query, code := range map[string]string{
		"q=a&q=b": "query_duplicate", "page=0": "positive_integer_required",
		"year=10000": "year_invalid", "year_from=0": "year_range_invalid", "recursive=nope": "recursive_invalid",
	} {
		t.Run(query, func(t *testing.T) {
			values, _ := url.ParseQuery(query)
			_, err := parseBrowseQuery(values)
			if err == nil {
				t.Fatal("invalid query accepted")
			}
			w := httptest.NewRecorder()
			fail(w, fmt.Errorf("query: %w", err))
			body := errorBody(t, w, http.StatusBadRequest)
			if body["code"] != "bad_request" || body["message_code"] != "errors."+code || body["message"] != "query: "+err.Error() {
				t.Fatalf("wrong validation response: %s", w.Body.String())
			}
			if strings.Contains(code, "integer") || code == "query_duplicate" || code == "year_range_invalid" {
				if _, ok := body["message_params"].(map[string]any)["field"].(string); !ok {
					t.Fatal("missing field interpolation parameter")
				}
			}
		})
	}
}

func TestPublishBomFailureRetainsReport(t *testing.T) {
	base := setup(t, "dev")
	status, b := doJSON(t, "POST", base+"/boms/missing/versions/1/publish", nil)
	body := mustJSON(t, b)
	if status != http.StatusConflict || body["code"] != "not_found" || body["message_code"] != "errors.not_found" || body["error"] != "not found" || body["message"] != body["error"] {
		t.Fatalf("publish error contract changed: %d %s", status, b)
	}
	if _, ok := body["report"]; !ok {
		t.Fatalf("publish validation report was lost: %s", b)
	}
}

func TestLocalSourceErrorSummaryKeepsDiagnostic(t *testing.T) {
	base := setup(t, "dev")
	s, b := doJSON(t, "POST", base+"/mounts", map[string]any{"kind": "local", "path": "relative/music"})
	body := mustJSON(t, b)
	if s != http.StatusBadRequest || body["code"] != "bad_request" || body["message_code"] != "errors.local_source_invalid" || body["message"] != "本地目录必须是服务端绝对路径" {
		t.Fatalf("local source diagnostics lost: %d %s", s, b)
	}
}

func TestMountSuccessEnvelopeKeepsFailureDetail(t *testing.T) {
	for _, status := range []string{library.StatusMounted, library.StatusError} {
		w := httptest.NewRecorder()
		m := library.Mount{ID: "local", Kind: "local", Status: status, Detail: "raw stderr 原始诊断", Password: "synthetic-secret"}
		writeMountResult(w, m, "source_mount_failed")
		body := mustJSON(t, w.Body.Bytes())
		if w.Code != http.StatusOK || body["status"] != status || body["detail"] != m.Detail || strings.Contains(w.Body.String(), m.Password) {
			t.Fatalf("mount response contract changed: %s", w.Body.String())
		}
		if status == library.StatusError {
			if body["detail_code"] != "errors.source_mount_failed" {
				t.Fatalf("missing localized mount summary: %s", w.Body.String())
			}
		} else if _, ok := body["detail_code"]; ok {
			t.Fatal("successful mount must not have a failure summary")
		}
	}
}

// Keep dictionary coverage close to the API contract without requiring a JS
// runtime for Go tests. Each entry uses a JSON array of zh-CN, zh-TW and en.
func TestAPIErrorDictionaryCoverage(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate API tests")
	}
	apiDir := filepath.Dir(thisFile)
	f, err := os.Open(filepath.Join(apiDir, "..", "web", "assets", "i18n-errors.js"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	entries := map[string]bool{}
	entry := regexp.MustCompile(`^\s*(\w+): (\[.*\]),?$`)
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		parts := entry.FindStringSubmatch(scan.Text())
		if parts == nil {
			continue
		}
		var messages []string
		if err := json.Unmarshal([]byte(parts[2]), &messages); err != nil || len(messages) != 3 {
			t.Fatalf("invalid dictionary entry %s: %v", parts[1], err)
		}
		for _, text := range messages {
			if strings.TrimSpace(text) == "" {
				t.Fatalf("empty translation: %s", parts[1])
			}
		}
		if entries[parts[1]] {
			t.Fatalf("duplicate dictionary key: %s", parts[1])
		}
		entries[parts[1]] = true
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	check := func(code string) {
		t.Helper()
		if code != "" && !entries[code] {
			t.Errorf("missing three-language dictionary entry: %s", code)
		}
	}
	files, err := filepath.Glob(filepath.Join(apiDir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range files {
		name := filepath.Base(filename)
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			arg := -1
			switch fn.Name {
			case "writeErr", "writeErrorCode", "fail", "writeMountResult":
				arg = 2
			case "invalidInput", "newAPIError":
				arg = 0
			case "writeServiceError":
				arg = 3
			}
			if arg >= 0 && len(call.Args) > arg {
				if lit, ok := call.Args[arg].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					code, _ := strconv.Unquote(lit.Value)
					check(code)
				}
			}
			return true
		})
		if name == "api.go" {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Name.Name != "statusErrorCode" && fn.Name.Name != "serviceErrorCode" {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if ret, ok := n.(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
						if lit, ok := ret.Results[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							code, _ := strconv.Unquote(lit.Value)
							check(code)
						}
					}
					return true
				})
			}
		}
	}
	check("builtin_seats_full")
	check("license_seats_full")
	check("local_source_invalid")
	check("source_add_failed")
	check("bom_publish_failed")
}
