#!/usr/bin/env bash
# Back up the database and ./data together (BACKEND_CONTEXT.md §7: files and
# DB must be backed up and restored as a pair).
#
#   scripts/backup.sh            -> backups/<UTC timestamp>/{db.dump,data.tar.gz,manifest.json}
#
# Order matters: the DB is dumped first, then ./data is archived. A source
# file is fsynced before its submission row commits and files are never
# rewritten, so every file referenced by the dump is already on disk when the
# archive is made (the archive may hold a few extra, unreferenced files).
set -euo pipefail
cd "$(dirname "$0")/.."
set -a
. ./.env
set +a

ts=$(date -u +%Y%m%dT%H%M%SZ)
dir=backups/$ts
mkdir -p "$dir"
chmod 700 backups "$dir"

echo "dumping database $POSTGRES_DB ..."
docker compose exec -T postgres pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc >"$dir/db.dump"
counts=$(docker compose exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tA -F= -c \
	"SELECT 'users', count(*) FROM users UNION ALL SELECT 'exams', count(*) FROM exams
	 UNION ALL SELECT 'problems', count(*) FROM problems UNION ALL SELECT 'test_cases', count(*) FROM test_cases
	 UNION ALL SELECT 'submissions', count(*) FROM submissions UNION ALL SELECT 'judge_runs', count(*) FROM judge_runs
	 UNION ALL SELECT 'judge_jobs', count(*) FROM judge_jobs")

echo "archiving ./data ..."
tar -C . -czf "$dir/data.tar.gz" data

python3 - "$dir" "$counts" <<'PY'
import hashlib, json, os, sys
d, counts = sys.argv[1], sys.argv[2]
def sha(p):
    h = hashlib.sha256()
    with open(p, "rb") as f:
        for b in iter(lambda: f.read(1 << 20), b""):
            h.update(b)
    return h.hexdigest()
m = {"created_at": os.path.basename(d),
     "rows": {k: int(v) for k, v in (l.split("=") for l in counts.split() if "=" in l)},
     "files": {n: {"bytes": os.path.getsize(os.path.join(d, n)), "sha256": sha(os.path.join(d, n))}
               for n in ("db.dump", "data.tar.gz")}}
json.dump(m, open(os.path.join(d, "manifest.json"), "w"), indent=2)
print(json.dumps(m["rows"]))
PY
echo "backup written to $dir"
