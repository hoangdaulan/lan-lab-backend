#!/usr/bin/env bash
# Restore a backup made by scripts/backup.sh.
#
#   scripts/restore.sh backups/<ts> --verify
#       Non-destructive: restores into a scratch database and a temp directory,
#       checks checksums, row counts against the manifest, and that every
#       submission's source file and every test file exists. Nothing live is
#       touched.
#
#   scripts/restore.sh backups/<ts> --yes
#       Replaces the live database and ./data (the current ./data is kept as
#       data.before-restore-<ts>). Stops api and worker during the restore.
set -euo pipefail
cd "$(dirname "$0")/.."
dir=${1:?usage: $0 backups/<ts> --verify|--yes}
mode=${2:-}
[[ -f $dir/db.dump && -f $dir/data.tar.gz && -f $dir/manifest.json ]] || {
	echo "not a backup directory: $dir" >&2
	exit 1
}
set -a
. ./.env
set +a
psql() { docker compose exec -T postgres psql -U "$POSTGRES_USER" -v ON_ERROR_STOP=1 "$@"; }

python3 - "$dir" <<'PY'
import hashlib, json, os, sys
d = sys.argv[1]
m = json.load(open(os.path.join(d, "manifest.json")))
for n, meta in m["files"].items():
    h = hashlib.sha256(open(os.path.join(d, n), "rb").read()).hexdigest()
    if h != meta["sha256"]:
        sys.exit(f"checksum mismatch: {n}")
print("checksums ok")
PY

case $mode in
--verify)
	db=judge_restore_verify
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"; psql -d "$POSTGRES_DB" -qc "DROP DATABASE IF EXISTS $db WITH (FORCE)" >/dev/null 2>&1 || true' EXIT
	psql -d "$POSTGRES_DB" -qc "DROP DATABASE IF EXISTS $db WITH (FORCE)" -c "CREATE DATABASE $db"
	docker compose exec -T postgres pg_restore -U "$POSTGRES_USER" -d "$db" --no-owner <"$dir/db.dump"
	tar -C "$tmp" -xzf "$dir/data.tar.gz"
	counts=$(psql -d "$db" -tA -F= -c \
		"SELECT 'users', count(*) FROM users UNION ALL SELECT 'exams', count(*) FROM exams
		 UNION ALL SELECT 'problems', count(*) FROM problems UNION ALL SELECT 'test_cases', count(*) FROM test_cases
		 UNION ALL SELECT 'submissions', count(*) FROM submissions UNION ALL SELECT 'judge_runs', count(*) FROM judge_runs
		 UNION ALL SELECT 'judge_jobs', count(*) FROM judge_jobs")
	paths=$(psql -d "$db" -tA -c "SELECT source_path FROM submissions UNION ALL SELECT input FROM test_cases UNION ALL SELECT output FROM test_cases")
	python3 - "$dir" "$tmp" "$counts" "$paths" <<'PY'
import json, os, sys
d, tmp, counts, paths = sys.argv[1:5]
want = json.load(open(os.path.join(d, "manifest.json")))["rows"]
got = {k: int(v) for k, v in (l.split("=") for l in counts.split() if "=" in l)}
if got != want:
    sys.exit(f"row counts differ: backup {want}, restored {got}")
missing = [p for p in paths.split() if not os.path.isfile(os.path.join(tmp, "data", p))]
if missing:
    sys.exit(f"{len(missing)} referenced files missing, e.g. {missing[:3]}")
print(f"restore verified: rows {got}; {len(paths.split())} referenced files present")
PY
	;;
--yes)
	ts=$(date -u +%Y%m%dT%H%M%SZ)
	echo "stopping api and worker ..."
	docker compose stop api worker
	echo "restoring database $POSTGRES_DB ..."
	docker compose exec -T postgres pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists --no-owner <"$dir/db.dump"
	echo "restoring ./data (current kept as data.before-restore-$ts) ..."
	mv data "data.before-restore-$ts"
	tar -C . -xzf "$dir/data.tar.gz"
	docker compose start api worker
	echo "restored from $dir"
	;;
*)
	echo "usage: $0 backups/<ts> --verify|--yes" >&2
	exit 2
	;;
esac
