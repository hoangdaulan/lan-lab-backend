#!/usr/bin/env bash
# Smoke test HTTP cho API v1 trên stack dev đang chạy (docs/IMPLEMENTATION_PLAN.md).
#
#   SMOKE_TEACHER_CODE=GV01 SMOKE_TEACHER_PASSWORD=... scripts/smoke.sh [auth|students|exams|problems|all]
#
# Biến tùy chọn: BASE (mặc định http://localhost:8080), DATA_HOST_DIR (thư mục
# ./data trên host, mặc định ./data). Mỗi lần chạy tạo dữ liệu mới (mã có hậu
# tố thời gian) và không xóa dữ liệu cũ. Cần curl và python3.
set -uo pipefail

BASE=${BASE:-http://localhost:8080}
API=$BASE/api/v1
DATA_HOST_DIR=${DATA_HOST_DIR:-./data}
TEACHER_CODE=${SMOKE_TEACHER_CODE:?set SMOKE_TEACHER_CODE}
TEACHER_PASSWORD=${SMOKE_TEACHER_PASSWORD:?set SMOKE_TEACHER_PASSWORD}
RUN=$(date +%H%M%S)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

PASS=0
FAIL=0
STATUS=""
BODY=""

# req METHOD PATH [JSON_BODY] [TOKEN]  -> sets STATUS, BODY
req() {
	local method=$1 path=$2 data=${3:-} token=${4:-}
	local args=(-s -o "$TMP/body" -w '%{http_code}' -X "$method" "$API$path")
	[[ -n $token ]] && args+=(-H "Authorization: Bearer $token")
	[[ -n $data ]] && args+=(-H 'Content-Type: application/json' -d "$data")
	STATUS=$(curl "${args[@]}")
	BODY=$(cat "$TMP/body")
}

# upload PATH FILE TOKEN  -> multipart field "file"
upload() {
	STATUS=$(curl -s -o "$TMP/body" -w '%{http_code}' -X POST "$API$1" -H "Authorization: Bearer $3" -F "file=@$2")
	BODY=$(cat "$TMP/body")
}

# jget EXPR  -> evaluates a Python expression over the JSON body bound to d
jget() {
	python3 -c 'import json,sys; d=json.loads(sys.argv[1]); print(eval(sys.argv[2]))' "$BODY" "$1"
}

ok() { PASS=$((PASS + 1)); printf '  \033[32mPASS\033[0m %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  \033[31mFAIL\033[0m %s (status %s) %s\n' "$1" "$STATUS" "${BODY:0:300}"; }

# expect DESC STATUS [PYTHON_CONDITION over d]
expect() {
	local desc=$1 want=$2 cond=${3:-}
	if [[ $STATUS != "$want" ]]; then
		bad "$desc: want $want"
		return
	fi
	if [[ -n $cond ]] && ! python3 -c 'import json,sys; d=json.loads(sys.argv[1]); sys.exit(0 if eval(sys.argv[2]) else 1)' "$BODY" "$cond" 2>/dev/null; then
		bad "$desc: condition $cond"
		return
	fi
	ok "$desc"
}

check() { # check DESC SHELL_TEST...
	local desc=$1
	shift
	if "$@"; then ok "$desc"; else bad "$desc"; fi
}

iso() { date -u -d "@$1" +%Y-%m-%dT%H:%M:%SZ; }

login() { # login CODE PASSWORD -> prints access token
	req POST /auth/login "{\"code\":\"$1\",\"password\":\"$2\"}"
	jget 'd["access_token"]'
}

teacher_token() { login "$TEACHER_CODE" "$TEACHER_PASSWORD"; }

new_student() { # new_student CODE TEACHER_TOKEN -> prints id
	req POST /students "{\"code\":\"$1\",\"fullname\":\"Smoke $1\"}" "$2"
	jget 'd["id"]'
}

# make_exam TOKEN -> sets EXAM_ID, RUNNING_ID, UPCOMING_ID
make_exam() {
	local now
	now=$(date -u +%s)
	req POST /exams "{\"title\":\"Smoke $RUN\",\"description\":\"\"}" "$1"
	EXAM_ID=$(jget 'd["id"]')
	req POST "/exams/$EXAM_ID/sessions" "{\"name\":\"running\",\"starts_at\":\"$(iso $((now - 60)))\",\"ends_at\":\"$(iso $((now + 3600)))\"}" "$1"
	RUNNING_ID=$(jget 'd["id"]')
	req POST "/exams/$EXAM_ID/sessions" "{\"name\":\"upcoming\",\"starts_at\":\"$(iso $((now + 3600)))\",\"ends_at\":\"$(iso $((now + 7200)))\"}" "$1"
	UPCOMING_ID=$(jget 'd["id"]')
}

section_auth() {
	echo "== auth"
	req POST /auth/login "{\"role\":\"teacher\",\"code\":\"$TEACHER_CODE\",\"password\":\"$TEACHER_PASSWORD\"}"
	expect "login with legacy role field rejected" 400 '"role" in d["error"]'
	req POST /auth/login "{\"code\":\"$TEACHER_CODE\",\"password\":\"wrong-$RUN\"}"
	expect "wrong password" 401
	local wrong_msg=$BODY
	req POST /auth/login "{\"code\":\"NOPE$RUN\",\"password\":\"x\"}"
	expect "unknown code, same message as wrong password" 401 "d == $wrong_msg"
	req POST /auth/login "{\"code\":\"$TEACHER_CODE\"}"
	expect "missing password" 400

	req POST /auth/login "{\"code\":\"${TEACHER_CODE,,}\",\"password\":\"$TEACHER_PASSWORD\"}"
	expect "login lowercase code, role from DB" 200 'd["user"]["role"] == "teacher" and d["user"]["code"] == "'"${TEACHER_CODE^^}"'"'
	local at rt rt2
	at=$(jget 'd["access_token"]')
	rt=$(jget 'd["refresh_token"]')
	check "JWT HS256, role claim, 15 min TTL" python3 - "$at" <<'PY'
import base64, json, sys
h, p, _ = sys.argv[1].split(".")
dec = lambda s: json.loads(base64.urlsafe_b64decode(s + "=" * (-len(s) % 4)))
h, p = dec(h), dec(p)
sys.exit(0 if h["alg"] == "HS256" and p["role"] == "teacher" and p["exp"] - p["iat"] == 900 else 1)
PY
	req GET /me
	expect "/me without token" 401
	req GET /me "" "${at%?}x"
	expect "/me with bad signature" 401
	req GET /me "" "$at"
	expect "/me" 200 'd["role"] == "teacher"'

	req POST /auth/refresh "{\"refresh_token\":\"$rt\"}"
	expect "refresh rotates" 200 'd["refresh_token"] != "'"$rt"'"'
	rt2=$(jget 'd["refresh_token"]')
	req POST /auth/refresh "{\"refresh_token\":\"$rt\"}"
	expect "old refresh token is single use" 401
	req POST /auth/logout "{\"refresh_token\":\"$rt2\"}"
	expect "logout" 204
	req POST /auth/refresh "{\"refresh_token\":\"$rt2\"}"
	expect "refresh after logout" 401

	local code="SMK${RUN}AU" st srt
	new_student "$code" "$at" >/dev/null
	req POST /auth/login "{\"code\":\"$code\",\"password\":\"$code\"}"
	expect "student login with MSV" 200 'd["user"]["role"] == "student"'
	st=$(jget 'd["access_token"]')
	srt=$(jget 'd["refresh_token"]')
	req PUT /me/password '{"current_password":"bad","new_password":"x"}' "$st"
	expect "change password, wrong current" 401
	req PUT /me/password "{\"current_password\":\"$code\",\"new_password\":\"new-$RUN\"}" "$st"
	expect "change password" 204
	req POST /auth/refresh "{\"refresh_token\":\"$srt\"}"
	expect "refresh tokens revoked by password change" 401
	req POST /auth/login "{\"code\":\"$code\",\"password\":\"new-$RUN\"}"
	expect "login with new password" 200
	req GET /students "" "$st"
	expect "student on teacher route" 403
	req GET /me/sessions "" "$at"
	expect "teacher on student route" 403
}

section_students() {
	echo "== students"
	local tt
	tt=$(teacher_token)
	local a="SMK${RUN}A" b="SMK${RUN}B" c="SMK${RUN}C"
	req POST /students "{\"code\":\" ${a,,} \",\"fullname\":\"Nguyen   Van  A\"}" "$tt"
	expect "create student normalizes" 201 'd["code"] == "'"$a"'" and d["fullname"] == "Nguyen Van A"'
	req POST /students "{\"code\":\"$a\",\"fullname\":\"X\"}" "$tt"
	expect "duplicate student code" 409
	req POST /students "{\"code\":\"$TEACHER_CODE\",\"fullname\":\"X\"}" "$tt"
	expect "student with teacher code" 409
	req POST /students '{"code":"A B","fullname":"X"}' "$tt"
	expect "code with space" 400

	printf 'code,full_name\n%s,Pham D\n%s,\n%s,Dup\n' "${b}X" "${c}X" "${b,,}x" >"$TMP/bad.csv"
	upload /students/import "$TMP/bad.csv" "$tt"
	expect "bad CSV rejected with row errors" 400 '[r["line"] for r in d["row_errors"]] == [3, 4]'
	req GET "/students?q=${b}X" "" "$tt"
	expect "bad CSV saved nothing" 200 'd["total"] == 0'

	printf '\xef\xbb\xbfcode,full_name,class\n%s,Tran Thi B,D21\n%s,Again,D21\n%s,"Le, Van C",D21\n%s,Teacher,D21\n' "$b" "$a" "$c" "$TEACHER_CODE" >"$TMP/ok.csv"
	upload /students/import "$TMP/ok.csv" "$tt"
	expect "import skips existing + teacher codes" 200 'd["created_count"] == 2 and [(s["line"], s["reason"]) for s in d["skipped"]] == [(3, "already exists"), (5, "code belongs to a teacher account")]'

	req GET "/students?q=SMK${RUN}&limit=2" "" "$tt"
	expect "list search + paging" 200 'd["total"] >= 3 and len(d["items"]) == 2 and d["limit"] == 2'
	req GET "/students?q=$TEACHER_CODE" "" "$tt"
	expect "teachers never listed as students" 200 'all(s["code"] != "'"${TEACHER_CODE^^}"'" for s in d["items"])'

	req POST /auth/login "{\"code\":\"$b\",\"password\":\"$b\"}"
	expect "imported student logs in with MSV" 200
	local st srt sid
	st=$(jget 'd["access_token"]')
	sid=$(jget 'd["user"]["id"]')
	req GET /students "" "$st"
	expect "student cannot manage students" 403
	req PUT /me/password "{\"current_password\":\"$b\",\"new_password\":\"changed-$RUN\"}" "$st"
	req POST /auth/login "{\"code\":\"$b\",\"password\":\"changed-$RUN\"}"
	srt=$(jget 'd["refresh_token"]')
	req POST "/students/$sid/reset-password" "" "$tt"
	expect "reset password" 204
	req POST /auth/refresh "{\"refresh_token\":\"$srt\"}"
	expect "reset revokes refresh tokens" 401
	req POST /auth/login "{\"code\":\"$b\",\"password\":\"$b\"}"
	expect "login with MSV after reset" 200
	req GET /me "" "$tt"
	local tid
	tid=$(jget 'd["id"]')
	req POST "/students/$tid/reset-password" "" "$tt"
	expect "reset on a teacher id" 404
}

section_exams() {
	echo "== exams"
	local tt st code="SMK${RUN}E"
	tt=$(teacher_token)
	new_student "$code" "$tt" >/dev/null
	st=$(login "$code" "$code")
	make_exam "$tt"
	req GET "/exams/$EXAM_ID" "" "$tt"
	expect "get exam" 200
	req POST "/exams/$EXAM_ID/sessions" "{\"name\":\"bad\",\"starts_at\":\"2026-10-21T10:00:00Z\",\"ends_at\":\"2026-10-21T10:00:00Z\"}" "$tt"
	expect "ends_at must be after starts_at" 400
	req POST "/exams/$EXAM_ID/problems" '{"code":"A","title":"Sum","statement":"a+b","time_limit_ms":1000,"memory_limit_mb":256}' "$tt"

	req GET /me/sessions "" "$st"
	expect "student sees sessions" 200 "any(i['id'] == $RUNNING_ID and i['participation'] is None for i in d['items'])"
	req GET "/sessions/$UPCOMING_ID" "" "$st"
	expect "upcoming hides problems" 200 'd["problems_hidden"] and d["problems"] == []'
	req GET "/sessions/$RUNNING_ID" "" "$st"
	expect "running shows problems" 200 'not d["problems_hidden"] and len(d["problems"]) == 1 and d["participation"] is None'
	req POST "/sessions/$UPCOMING_ID/join" "" "$st"
	expect "join before start" 409
	req POST "/sessions/$UPCOMING_ID/finish" "" "$st"
	expect "finish without join" 409
	req POST "/sessions/$RUNNING_ID/join" "" "$st"
	expect "join running" 200 'd["status"] == "in_progress"'
	local pid
	pid=$(jget 'd["id"]')
	req POST "/sessions/$RUNNING_ID/join" "" "$st"
	expect "re-join returns same row" 200 "d['id'] == $pid"
	req POST "/sessions/$RUNNING_ID/finish" "" "$st"
	expect "finish" 200 'd["status"] == "finished"'
	local fin
	fin=$(jget 'd["finished_at"]')
	sleep 1
	req POST "/sessions/$RUNNING_ID/finish" "" "$st"
	expect "finish is idempotent" 200 "d['finished_at'] == '$fin'"
	req GET "/sessions/$RUNNING_ID/participants" "" "$tt"
	expect "participants" 200 "[(p['student_code'], p['status']) for p in d['items']] == [('$code', 'finished')]"
	req DELETE "/exams/$EXAM_ID" "" "$tt"
	expect "delete exam with children" 409
	req DELETE "/sessions/$UPCOMING_ID" "" "$tt"
	expect "delete session without submissions" 204
	req GET /exams/999999999 "" "$tt"
	expect "unknown exam" 404
	req GET /exams/abc "" "$tt"
	expect "malformed id" 400
}

section_problems() {
	echo "== problems"
	local tt st code="SMK${RUN}P"
	tt=$(teacher_token)
	new_student "$code" "$tt" >/dev/null
	st=$(login "$code" "$code")
	make_exam "$tt"
	req POST "/exams/$EXAM_ID/problems" '{"code":" a ","title":"Sum","statement":"a+b","time_limit_ms":1000,"memory_limit_mb":256}' "$tt"
	expect "create problem" 201 'd["code"] == "A" and d["version"] == 1'
	local pid dir
	pid=$(jget 'd["id"]')
	dir="$DATA_HOST_DIR/$(jget 'd["test_dir"]')"
	check "test directory created and writable on host" test -d "$dir" -a -w "$dir"
	req POST "/exams/$EXAM_ID/problems" '{"code":"A","title":"dup","statement":"","time_limit_ms":1000,"memory_limit_mb":256}' "$tt"
	expect "duplicate problem code" 409
	req POST "/exams/$EXAM_ID/problems" '{"code":"B","title":"x","statement":"","time_limit_ms":0,"memory_limit_mb":256}' "$tt"
	expect "time limit out of range" 400

	req POST "/problems/$pid/rejudge" "" "$tt"
	expect "rejudge without tests" 409
	printf '1 2\n' >"$dir/1.in"
	req POST "/problems/$pid/rejudge" "" "$tt"
	expect "rejudge with unpaired file" 409 '"1.in has no 1.out" in d["error"]'
	printf '3\n' >"$dir/1.out"
	printf '' >"$dir/2.in"
	printf '0\n' >"$dir/2.out"
	printf '5 5\n' >"$dir/10.in"
	printf '10\n' >"$dir/10.out"
	req POST "/problems/$pid/rejudge" "" "$tt"
	expect "rejudge syncs tests in numeric order" 200 'd["problem"]["version"] == 2 and [t["name"] for t in d["tests"]] == ["1", "2", "10"]'
	req GET "/problems/$pid/tests" "" "$tt"
	expect "list tests" 200 'len(d["items"]) == 3'

	req PUT "/problems/$pid" '{"code":"A","title":"Sum 2","statement":"a+b","time_limit_ms":1000,"memory_limit_mb":256}' "$tt"
	expect "text edit keeps version" 200 'd["problem"]["version"] == 2 and not d["rejudge_queued"]'
	req PUT "/problems/$pid" '{"code":"A","title":"Sum 2","statement":"a+b","time_limit_ms":2000,"memory_limit_mb":256}' "$tt"
	expect "limit change bumps version" 200 'd["problem"]["version"] == 3 and d["rejudge_queued"]'

	req GET "/sessions/$RUNNING_ID/problems/$pid" "" "$st"
	expect "statement before join" 403
	req POST "/sessions/$RUNNING_ID/join" "" "$st"
	req GET "/sessions/$RUNNING_ID/problems/$pid" "" "$st"
	expect "statement after join" 200 'd["statement"] == "a+b" and d["time_limit_ms"] == 2000'
	req GET "/sessions/$UPCOMING_ID/problems/$pid" "" "$st"
	expect "statement before starts_at" 403

	local other_running=$RUNNING_ID
	make_exam "$tt"
	req POST "/exams/$EXAM_ID/problems" '{"code":"Z","title":"tmp","statement":"","time_limit_ms":1000,"memory_limit_mb":64}' "$tt"
	local other
	other=$(jget 'd["id"]')
	req GET "/sessions/$other_running/problems/$other" "" "$st"
	expect "problem of another exam" 404
	req DELETE "/problems/$other" "" "$tt"
	expect "delete problem without submissions" 204
	req GET "/problems/$other" "" "$tt"
	expect "deleted problem gone" 404
	req POST "/problems/$pid/rejudge" "" "$st"
	expect "student cannot rejudge" 403
}

case ${1:-all} in
auth) section_auth ;;
students) section_students ;;
exams) section_exams ;;
problems) section_problems ;;
all)
	section_auth
	section_students
	section_exams
	section_problems
	;;
*)
	echo "usage: $0 [auth|students|exams|problems|all]" >&2
	exit 2
	;;
esac

echo "== $PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
