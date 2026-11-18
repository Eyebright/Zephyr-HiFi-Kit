#!/bin/sh
# Zephyr Music Player — end-to-end runtime smoke test.
# Builds a host-native server, generates an ephemeral GPG signing key, starts
# the server in ENFORCE mode, and exercises the full delivery core:
#   health -> console -> catalog -> signed upload (accepted) -> unsigned upload
#   (rejected) -> repo download -> token -> register -> BOM validate/publish ->
#   job advance -> play gate (waiting_idle) -> idle resume -> done.
# Cleans up its temp dir and server process on exit. Requires: go, gpg, curl.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
if [ -x "$HOME/sdk/go/bin/go" ]; then export GOROOT="$HOME/sdk/go"; export PATH="$GOROOT/bin:$PATH"; fi
export CGO_ENABLED=0 GO111MODULE=on GOFLAGS=-mod=mod

for tool in go gpg curl; do command -v "$tool" >/dev/null 2>&1 || { echo "missing $tool" >&2; exit 1; }; done

EXE=""; case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) EXE=".exe";; esac
TMP=$(mktemp -d)
# On Windows/MSYS, native Go needs a Windows-form data dir while MSYS gpg needs
# a POSIX GNUPGHOME; MSYS_NO_PATHCONV stops the double translation that otherwise
# mangles the keyring path. On Linux both are plain POSIX and the flag is inert.
if [ -n "$EXE" ]; then TW=$(cygpath -m "$TMP"); else TW="$TMP"; fi
PORT="${PORT:-18080}"
BASE="http://127.0.0.1:$PORT/api/v1"
SRV=""
PASS=0; FAIL=0

cleanup() {
  [ -n "$SRV" ] && kill "$SRV" 2>/dev/null || true
  GNUPGHOME="$TMP/gnupg" gpgconf --kill gpg-agent 2>/dev/null || true
  rm -rf "$TMP" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

ok()   { PASS=$((PASS+1)); printf '  \033[1;32mPASS\033[0m %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  \033[1;31mFAIL\033[0m %s (got: %s)\n' "$1" "$2"; }
check(){ if [ "$2" = "$3" ]; then ok "$1"; else bad "$1" "$2 != $3"; fi; }
# json helpers extract the FIRST occurrence (top-level job state precedes the
# phases array, which also carries "state" keys). bools are unquoted in JSON.
jsonstr(){ printf '%s' "$1" | grep -o "\"$2\":\"[^\"]*\"" | head -1 | cut -d'"' -f4; }
jsonint(){ printf '%s' "$1" | grep -o "\"$2\":[0-9][0-9]*" | head -1 | cut -d: -f2; }
jsonbool(){ printf '%s' "$1" | grep -oE "\"$2\":(true|false)" | head -1 | cut -d: -f2; }
# req <method> <path> <data> -> sets CODE and BODY
req(){ CODE=$(curl -s -o "$TMP/resp" -w '%{http_code}' -X "$1" "$BASE$2" ${3:+-H 'Content-Type: application/json' -d "$3"}); BODY=$(cat "$TMP/resp"); }

echo "== build host server =="
go build -o "$TMP/hifihub-server$EXE" ./cmd/hifihub-server

echo "== ephemeral GPG trust root =="
export GNUPGHOME="$TMP/gnupg"; mkdir -p "$GNUPGHOME"; chmod 700 "$GNUPGHOME"
gpg --batch --pinentry-mode loopback --passphrase '' \
    --quick-generate-key "Zephyr Smoke <smoke@zephyr.hk>" ed25519 sign never >/dev/null 2>&1
FPR=$(gpg --list-keys --with-colons | sed -n 's/^fpr:::::::::\([^:]*\):.*/\1/p' | head -1)
[ -n "$FPR" ] || { echo "key generation failed" >&2; exit 1; }

echo "== start server (enforce) on :$PORT =="
mkdir -p "$TMP/data" "$TMP/dist"
# Seed the distribution dir so /dl/ whitelist + serving can be exercised.
printf '#!/bin/sh\necho zephyr agent installer\n' > "$TMP/dist/agent-install.sh"
printf 'fake agent deb for smoke\n' > "$TMP/dist/hifihub-agent_0.1.0_amd64.deb"
( cd "$TMP/dist" && sha256sum agent-install.sh hifihub-agent_0.1.0_amd64.deb > SHA256SUMS )
MSYS_NO_PATHCONV=1 HIFIHUB_LISTEN="127.0.0.1:$PORT" HIFIHUB_DATA_DIR="$TW/data" \
HIFIHUB_GPG_HOME="$TMP/gnupg" HIFIHUB_DIST_DIR="$TW/dist" HIFIHUB_SIGNATURE_MODE=enforce HIFIHUB_PLAY_GATE=true \
  "$TMP/hifihub-server$EXE" > "$TMP/server.log" 2>&1 &
SRV=$!

# wait for health
i=0; until curl -s "$BASE/health" >/dev/null 2>&1; do i=$((i+1)); [ "$i" -gt 50 ] && { echo "server did not start"; cat "$TMP/server.log"; exit 1; }; sleep 0.2; done

echo "== assertions =="
req GET /health ""; check "health 200" "$CODE" "200"
check "signature_mode=enforce" "$(jsonstr "$BODY" signature_mode)" "enforce"

# console shell
CCODE=$(curl -s -o "$TMP/idx" -w '%{http_code}' "http://127.0.0.1:$PORT/")
check "console 200" "$CCODE" "200"
grep -q "Zephyr Music Player" "$TMP/idx" && ok "console branded Zephyr" || bad "console branding" "missing"

# catalog
req POST /apps '{"app_id":"testapp","name":"Test App","category":"renderer"}'
check "create app 201" "$CODE" "201"

# artifact + detached signature
printf 'zephyr smoke artifact %s\n' "$(date -u)" > "$TMP/testapp_1.0.0_amd64.deb"
gpg --batch --yes --pinentry-mode loopback --passphrase '' --armor --detach-sign \
    --local-user "$FPR" -o "$TMP/testapp_1.0.0_amd64.deb.asc" "$TMP/testapp_1.0.0_amd64.deb" >/dev/null 2>&1
ORIG_SHA=$(sha256sum "$TMP/testapp_1.0.0_amd64.deb" | awk '{print $1}')

# signed upload -> accepted
CODE=$(curl -s -o "$TMP/resp" -w '%{http_code}' -X POST "$BASE/apps/testapp/versions" \
  -F 'meta={"version":"1.0.0","arch":"amd64","format":"deb","units":["testapp.service"],"lifecycle":"available"}' \
  -F "artifact=@$TMP/testapp_1.0.0_amd64.deb" -F "signature=@$TMP/testapp_1.0.0_amd64.deb.asc")
BODY=$(cat "$TMP/resp"); check "signed upload 201" "$CODE" "201"
check "version signed=true" "$(jsonbool "$BODY" signed)" "true"
check "server sha256 matches" "$(jsonstr "$BODY" sha256)" "$ORIG_SHA"

# unsigned upload -> rejected (enforce gate, NFR-S2)
CODE=$(curl -s -o "$TMP/resp" -w '%{http_code}' -X POST "$BASE/apps/testapp/versions" \
  -F 'meta={"version":"2.0.0","arch":"amd64","format":"deb"}' \
  -F "artifact=@$TMP/testapp_1.0.0_amd64.deb")
BODY=$(cat "$TMP/resp"); check "unsigned upload rejected 400" "$CODE" "400"
check "rejection code" "$(jsonstr "$BODY" code)" "artifact_rejected"

# repo download integrity
curl -s -o "$TMP/dl.deb" "$BASE/repo/testapp/1.0.0/amd64"
check "download sha256 matches" "$(sha256sum "$TMP/dl.deb" | awk '{print $1}')" "$ORIG_SHA"
SCODE=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/repo/testapp/1.0.0/amd64/sig")
check "signature download 200" "$SCODE" "200"

# token + register
req POST /nodes/token '{"ttl_seconds":1800}'; check "issue token 201" "$CODE" "201"
TOKEN=$(jsonstr "$BODY" token)
req POST /nodes/register "{\"token\":\"$TOKEN\",\"alias\":\"smoke-node\",\"arch\":\"amd64\",\"distro\":\"Debian\",\"kernel\":\"6.x\",\"machine_id\":\"smoke-mid\"}"
check "register 201" "$CODE" "201"; NODE=$(jsonstr "$BODY" node_id)
SECRET=$(jsonstr "$BODY" secret)
check "register returns hex-32 secret" "$(printf '%s' "$SECRET" | grep -cE '^[0-9a-f]{32}$')" "1"
# one-time token must now be consumed
req POST /nodes/register "{\"token\":\"$TOKEN\",\"alias\":\"dup\",\"arch\":\"amd64\",\"machine_id\":\"dup\"}"
check "token reuse rejected 401" "$CODE" "401"

# BOM validate + publish
req POST /boms '{"bom_id":"smoke","role":"renderer","apps":[{"app_id":"testapp","version":"1.0.0"}]}'
check "create BOM 201" "$CODE" "201"; BVER=$(jsonint "$BODY" version)
req POST "/boms/smoke/versions/$BVER/validate?arch=amd64" ""
check "BOM validate no errors" "$(printf '%s' "$BODY" | sed -n 's/.*"errors":\[\([^]]*\)\].*/\1/p')" ""
req POST "/boms/smoke/versions/$BVER/publish?arch=amd64" ""
check "BOM published" "$(jsonbool "$BODY" published)" "true"

# --- built-in grant: up to 3 terminals authorized without any license ---
req GET /license ""; check "builtin seats=3" "$(jsonint "$BODY" builtin_seats)" "3"
check "effective seats=3 before activation" "$(jsonint "$BODY" effective_seats)" "3"
req POST "/nodes/$NODE/authorize" ""; check "authorize without license 200" "$CODE" "200"
req POST "/nodes/$NODE/revoke" ""; check "revoke releases builtin seat" "$CODE" "200"

# --- license: sign -> activate -> authorize -> job gate (FR-6) ---
EXP=$(( $(date +%s) + 2592000 ))
printf '{"key":"ZEPHYR-SMOKE-0001","account":"smoke@zephyr.hk","seats":5,"expires_at":%s}' "$EXP" > "$TMP/license.json"
gpg --batch --yes --pinentry-mode loopback --passphrase '' --armor --detach-sign \
    --local-user "$FPR" -o "$TMP/license.json.asc" "$TMP/license.json" >/dev/null 2>&1
PAYLOAD_B64=$(base64 "$TMP/license.json" | tr -d '\n')
SIG_B64=$(base64 "$TMP/license.json.asc" | tr -d '\n')
# bad signature rejected (enforce mode verifies against the server keyring)
req POST /license/activate "{\"payload_b64\":\"$PAYLOAD_B64\",\"sig_b64\":\"bm90LWEtc2lnbmF0dXJl\"}"
check "license bad signature rejected 400" "$CODE" "400"
# valid signed activation
req POST /license/activate "{\"payload_b64\":\"$PAYLOAD_B64\",\"sig_b64\":\"$SIG_B64\"}"
check "license activate 200" "$CODE" "200"
check "license activated" "$(jsonstr "$BODY" status)" "activated"
check "license seats=5" "$(jsonint "$BODY" seats)" "5"
req GET /license ""; check "license GET 200" "$CODE" "200"
check "license GET seats=5" "$(jsonint "$BODY" seats)" "5"
check "license GET effective=5" "$(jsonint "$BODY" effective_seats)" "5"
# a job against an unauthorized node is gated even with an active license
req POST /jobs "{\"node_id\":\"$NODE\",\"bom_id\":\"smoke\",\"bom_version\":$BVER,\"kind\":\"provision\"}"
check "job before authorize rejected 409" "$CODE" "409"
# authorize consumes a license seat
req POST "/nodes/$NODE/authorize" ""; check "authorize node 200" "$CODE" "200"
check "node authorized" "$(jsonbool "$BODY" authorized)" "true"

# job: idle node advances to downloading
req POST /jobs "{\"node_id\":\"$NODE\",\"bom_id\":\"smoke\",\"bom_version\":$BVER,\"kind\":\"provision\"}"
check "create job 201" "$CODE" "201"; JOB=$(jsonstr "$BODY" id)
req POST "/jobs/$JOB/advance" ""; check "idle job -> downloading" "$(jsonstr "$BODY" state)" "downloading"

# play gate: playing node parks in waiting_idle (FR-3.10)
req POST "/nodes/$NODE/heartbeat" '{"playing":true,"active_proto":"airplay"}'
req POST /jobs "{\"node_id\":\"$NODE\",\"bom_id\":\"smoke\",\"bom_version\":$BVER,\"kind\":\"upgrade\"}"
JOB2=$(jsonstr "$BODY" id)
req POST "/jobs/$JOB2/advance" ""; check "playing job -> waiting_idle" "$(jsonstr "$BODY" state)" "waiting_idle"
# resume when idle
req POST "/nodes/$NODE/heartbeat" '{"playing":false}'
req POST "/jobs/$JOB2/advance" ""; check "idle resume -> downloading" "$(jsonstr "$BODY" state)" "downloading"

# agent reports final phase -> done
req POST "/jobs/$JOB2/phases" '{"phase":"validating","state":"ok"}'
check "validating ok -> done" "$(jsonstr "$BODY" state)" "done"

# --- heartbeat policy fields (FR-4) ---
req POST "/nodes/$NODE/heartbeat" '{"playing":false}'
check "heartbeat ok" "$(jsonbool "$BODY" ok)" "true"
check "heartbeat play_protect default true" "$(jsonbool "$BODY" play_protect)" "true"
check "heartbeat auto_os_update default false" "$(jsonbool "$BODY" auto_os_update)" "false"
check "heartbeat offline_after default 90" "$(jsonint "$BODY" offline_after)" "90"

# --- node PATCH validation + offline_after clamp (FR-4 management) ---
req PATCH "/nodes/$NODE" '{"alias":"   "}'
check "patch empty alias rejected 400" "$CODE" "400"
LONG_ALIAS=$(printf '%041d' 0 | tr '0' 'a')
req PATCH "/nodes/$NODE" "{\"alias\":\"$LONG_ALIAS\"}"
check "patch 41-char alias rejected 400" "$CODE" "400"
LONG_GROUP=$(printf '%025d' 0 | tr '0' 'g')
req PATCH "/nodes/$NODE" "{\"group\":\"$LONG_GROUP\"}"
check "patch 25-char group rejected 400" "$CODE" "400"
LONG_NOTE=$(printf '%0201d' 0 | tr '0' 'n')
req PATCH "/nodes/$NODE" "{\"note\":\"$LONG_NOTE\"}"
check "patch 201-char note rejected 400" "$CODE" "400"
req PATCH "/nodes/$NODE" '{"bom_id":"no-such-bom"}'
check "patch missing bom rejected 400" "$CODE" "400"
req PATCH "/nodes/$NODE" '{"alias":"smoke-renamed","group":"lab","note":"bench-node","offline_after":120}'
check "patch valid 200" "$CODE" "200"
check "patch alias applied" "$(jsonstr "$BODY" alias)" "smoke-renamed"
check "patch group applied" "$(jsonstr "$BODY" group)" "lab"
check "patch offline_after applied" "$(jsonint "$BODY" offline_after)" "120"
req PATCH "/nodes/$NODE" '{"offline_after":5}'
check "patch offline_after clamp low -> 30" "$(jsonint "$BODY" offline_after)" "30"
req PATCH "/nodes/$NODE" '{"offline_after":99999}'
check "patch offline_after clamp high -> 3600" "$(jsonint "$BODY" offline_after)" "3600"

# --- token status (registration wizard polling) ---
req GET "/nodes/token/$TOKEN" ""; check "token status 200" "$CODE" "200"
check "token used=true" "$(jsonbool "$BODY" used)" "true"
check "token bound node" "$(jsonstr "$BODY" node_id)" "$NODE"

# --- command roundtrip: os-check -> result -> osupdates (FR-6) ---
req POST "/nodes/$NODE/commands" '{"type":"os-check","params":{"packages":["foo"]}}'
check "create command 201" "$CODE" "201"; CMD=$(jsonstr "$BODY" id)
check "command pending" "$(jsonstr "$BODY" status)" "pending"
req GET "/nodes/$NODE/commands?status=pending" ""
check "pending commands listed 200" "$CODE" "200"
printf '%s' "$BODY" | grep -q "$CMD" && ok "pending list contains command" || bad "pending list contains command" "missing"
req POST "/commands/$CMD/result" '{"status":"ok","log":"checked","data":{"reboot_required":true,"packages":[{"name":"foo","current":"1.0","candidate":"2.0"}]}}'
check "command result 200" "$CODE" "200"
check "command done" "$(jsonstr "$BODY" status)" "done"
req POST "/commands/$CMD/result" '{"status":"ok","log":"again"}'
check "duplicate command result rejected 409" "$CODE" "409"
req GET "/nodes/$NODE/osupdates" ""; check "osupdates 200" "$CODE" "200"
check "osupdates reboot_required" "$(jsonbool "$BODY" reboot_required)" "true"
req GET "/nodes/$NODE/netdiag" ""; check "netdiag empty default 200" "$CODE" "200"

# --- play protection blocks os-upgrade while playing (FR-3.10) ---
req POST "/nodes/$NODE/heartbeat" '{"playing":true,"active_proto":"raat"}'
req POST "/nodes/$NODE/commands" '{"type":"os-upgrade","params":{"packages":["foo"]}}'
check "os-upgrade while playing rejected 409" "$CODE" "409"
req POST "/nodes/$NODE/commands" '{"type":"os-check","params":{}}'
check "os-check while playing still allowed 201" "$CODE" "201"
req POST "/nodes/$NODE/heartbeat" '{"playing":false}'
req POST "/nodes/$NODE/commands" '{"type":"os-upgrade","params":{"packages":["foo"]}}'
check "os-upgrade when idle accepted 201" "$CODE" "201"

# --- alerts scan + ack/resolve (FR-7.1) ---
req POST /alerts/scan ""; check "alerts scan 200" "$CODE" "200"
ACTIVE=$(jsonint "$BODY" active)
[ "$ACTIVE" -ge 1 ] && ok "alerts scan found conditions" || bad "alerts scan found conditions" "$ACTIVE"
req GET /alerts ""; check "alerts list 200" "$CODE" "200"
printf '%s' "$BODY" | grep -q "reboot_required" && ok "reboot_required alert present" || bad "reboot_required alert present" "missing"
AID=$(jsonstr "$BODY" id)
req POST /alerts/ack-all ""; check "alerts ack-all 200" "$CODE" "200"
req POST "/alerts/$AID/ack" ""; check "alert ack 200" "$CODE" "200"
check "alert acked" "$(jsonstr "$BODY" status)" "ack"
req POST "/alerts/$AID/resolve" ""; check "alert resolve 200" "$CODE" "200"
check "alert resolved" "$(jsonstr "$BODY" status)" "resolved"

# --- audit trail + export (FR-7.2) ---
req GET /audit ""; check "audit list 200" "$CODE" "200"
[ "$BODY" != "[]" ] && ok "audit non-empty" || bad "audit non-empty" "empty"
req GET "/audit?limit=3" ""; check "audit limit 200" "$CODE" "200"
ACODE=$(curl -s -o "$TMP/audit.csv" -D "$TMP/audit.hdr" -w '%{http_code}' "$BASE/audit/export?format=csv")
check "audit csv export 200" "$ACODE" "200"
grep -qi 'attachment' "$TMP/audit.hdr" && ok "audit csv attachment" || bad "audit csv attachment" "missing"
head -1 "$TMP/audit.csv" | grep -q '^id,at,actor' && ok "audit csv header" || bad "audit csv header" "missing"
JCODE=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/audit/export?format=json")
check "audit json export 200" "$JCODE" "200"
req GET "/audit/export?format=xml" ""; check "audit bad format rejected 400" "$CODE" "400"

# --- GPG keys / trust root (NFR-S2) ---
req GET /keys ""; check "keys list 200" "$CODE" "200"
printf '%s' "$BODY" | grep -q "$FPR" && ok "keys list contains signing key" || bad "keys list contains signing key" "missing"
req POST "/keys/$FPR/trust-root" ""; check "set trust root 200" "$CODE" "200"
check "trust root flagged" "$(jsonbool "$BODY" is_trust_root)" "true"
KCODE=$(curl -s -o "$TMP/trust-root.asc" -w '%{http_code}' "$BASE/keys/trust-root.asc")
check "trust root export 200" "$KCODE" "200"
grep -q 'BEGIN PGP PUBLIC KEY BLOCK' "$TMP/trust-root.asc" && ok "trust root armor" || bad "trust root armor" "missing"
gpg --batch --armor --export "$FPR" > "$TMP/pub.asc" 2>/dev/null
PUBBODY=$(awk 'BEGIN{printf "{\"armored\":\""} {printf "%s\\n",$0} END{printf "\"}"}' "$TMP/pub.asc")
req POST /keys/import "$PUBBODY"; check "public key import 200" "$CODE" "200"
req POST /keys/import '{"armored":"-----BEGIN PGP PRIVATE KEY BLOCK-----\nfake\n-----END PGP PRIVATE KEY BLOCK-----"}'
check "private key import rejected 400" "$CODE" "400"

# --- staged rollout create + advance (FR-3.5) ---
req POST "/nodes/$NODE/heartbeat" '{"playing":false}'
req POST /rollouts "{\"name\":\"smoke-rollout\",\"bom_id\":\"smoke\",\"bom_version\":$BVER,\"rings\":[{\"name\":\"canary\",\"percent\":50},{\"name\":\"rest\",\"percent\":50}]}"
check "create rollout 201" "$CODE" "201"; RO=$(jsonstr "$BODY" id)
check "rollout starts at ring 0" "$(jsonint "$BODY" current_ring)" "0"
RJOB=$(printf '%s' "$BODY" | grep -o '"jobs":\["[^"]*' | head -1 | sed 's/.*\["//')
req POST "/rollouts/$RO/advance" ""; check "rollout advance busy 409" "$CODE" "409"
req POST "/jobs/$RJOB/advance" ""; check "rollout ring job -> downloading" "$(jsonstr "$BODY" state)" "downloading"
req POST "/jobs/$RJOB/phases" '{"phase":"validating","state":"ok"}'
check "rollout ring job -> done" "$(jsonstr "$BODY" state)" "done"
req POST "/rollouts/$RO/advance" ""; check "rollout advance to ring 1" "$(jsonint "$BODY" current_ring)" "1"
req POST "/rollouts/$RO/advance" ""; check "rollout final advance done" "$(jsonstr "$BODY" status)" "done"
req DELETE "/rollouts/$RO" ""; check "rollout delete 200" "$CODE" "200"

# --- BOM export / import / rollback (FR-3.2) ---
ECODE=$(curl -s -o "$TMP/bom-export.json" -D "$TMP/bom.hdr" -w '%{http_code}' "$BASE/boms/smoke/versions/$BVER/export")
check "bom export 200" "$ECODE" "200"
grep -qi 'attachment' "$TMP/bom.hdr" && ok "bom export attachment" || bad "bom export attachment" "missing"
EXPORTBODY=$(cat "$TMP/bom-export.json")
req POST /boms/import "$EXPORTBODY"; check "bom import 201" "$CODE" "201"
check "bom import is draft" "$(jsonstr "$BODY" status)" "draft"
IMPORTED_VER=$(jsonint "$BODY" version)
req POST "/boms/smoke/rollback" "{\"version\":$BVER}"; check "bom rollback 201" "$CODE" "201"
check "bom rollback published" "$(jsonstr "$BODY" status)" "published"
RB_VER=$(jsonint "$BODY" version)
[ "$RB_VER" -gt "$IMPORTED_VER" ] && ok "rollback version increments" || bad "rollback version increments" "$RB_VER <= $IMPORTED_VER"
req GET "/boms/smoke/versions" ""
check "bom version history 200" "$CODE" "200"
echo "$BODY" | grep -q "\"version\":$RB_VER" && ok "history contains rollback version" || bad "history contains rollback version" "missing v$RB_VER"
echo "$BODY" | grep -q "\"version\":$IMPORTED_VER" && ok "history keeps older draft" || bad "history keeps older draft" "missing v$IMPORTED_VER"
req GET "/boms?status=published" ""
check "published list keeps bound version" "$(echo "$BODY" | grep -o '"bom_id":"smoke","version":[0-9]*' | head -1 | grep -o '[0-9]*$')" "$RB_VER"
echo "$BODY" | grep -q '"status":"draft"' && bad "published list excludes drafts" "draft leaked" || ok "published list excludes drafts"

# --- node delete / restore + delete-playing guard (FR-3.10) ---
req POST /nodes/token '{"ttl_seconds":1800}'; TOKEN2=$(jsonstr "$BODY" token)
req POST /nodes/register "{\"token\":\"$TOKEN2\",\"alias\":\"smoke-node-2\",\"arch\":\"amd64\",\"machine_id\":\"smoke-mid-2\"}"
check "register second node 201" "$CODE" "201"; N2=$(jsonstr "$BODY" node_id)
req GET "/nodes/$N2" ""; NODEJSON="$BODY"
req POST "/nodes/$N2/heartbeat" '{"playing":true}'
req DELETE "/nodes/$N2" ""; check "delete playing node rejected 409" "$CODE" "409"
req POST "/nodes/$N2/heartbeat" '{"playing":false}'
req DELETE "/nodes/$N2" ""; check "delete idle node 200" "$CODE" "200"
req GET "/nodes/$N2" ""; check "deleted node gone 404" "$CODE" "404"
req POST /nodes/restore "{\"node\":$NODEJSON}"; check "restore node 200" "$CODE" "200"
req GET "/nodes/$N2" ""; check "restored node present 200" "$CODE" "200"

# --- console proxy guards (missing node -> 502) ---
req GET "/nodes/n-missing/console/status" ""; check "console proxy missing node 502" "$CODE" "502"
req GET "/nodes/n-missing/local/api/foo" ""; check "local api proxy missing node 502" "$CODE" "502"

# --- distribution downloads (whitelist only) ---
DCODE=$(curl -s -o "$TMP/dl-install.sh" -w '%{http_code}' "http://127.0.0.1:$PORT/dl/agent-install.sh")
check "dl agent-install.sh 200" "$DCODE" "200"
DCODE=$(curl -s -o "$TMP/dl-agent.deb" -w '%{http_code}' "http://127.0.0.1:$PORT/dl/hifihub-agent_0.1.0_amd64.deb")
check "dl agent deb 200" "$DCODE" "200"
DCODE=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/dl/SHA256SUMS")
check "dl SHA256SUMS 200" "$DCODE" "200"
DCODE=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/dl/not-whitelisted.bin")
check "dl bad name rejected 404" "$DCODE" "404"
DCODE=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/dl/hifihub-agent_0.1.0_mips.deb")
check "dl bad arch rejected 404" "$DCODE" "404"

# --- server self metrics ---
MCODE=$(curl -s -o "$TMP/metrics.json" -w '%{http_code}' "http://127.0.0.1:$PORT/metrics/server")
check "metrics 200" "$MCODE" "200"
MBODY=$(cat "$TMP/metrics.json")
check "metrics signature_mode=enforce" "$(jsonstr "$MBODY" signature_mode)" "enforce"
check "metrics play_gate=true" "$(jsonbool "$MBODY" play_gate)" "true"
printf '%s' "$MBODY" | grep -q '"version"' && ok "metrics version present" || bad "metrics version present" "missing"

echo "== result: $PASS passed, $FAIL failed =="
[ "$FAIL" -eq 0 ] || { echo "--- server.log ---"; cat "$TMP/server.log"; exit 1; }
