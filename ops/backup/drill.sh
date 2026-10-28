#!/usr/bin/env bash
# Backup and restore drill for the local Docker stack.
#
# It proves a backup can actually be restored, which is the only kind of backup
# that counts. It never touches live data: every restore goes into a scratch
# database (Postgres "<db>_drill", Mongo "<db>_drill") and a scratch folder,
# which are removed at the end (set KEEP=1 to inspect them).
#
#   ops/backup/drill.sh
#
# Checks: Postgres row counts for the tables below, the applied migration list,
# Mongo document counts per collection, and a checksum of every media file.
# Exit code 0 means everything restored and matched; anything else is a failure.
#
# Needs: docker, and the containers from docker-compose.yml running. Values come
# from ../../.env (POSTGRES_USER, POSTGRES_DB, MONGO_USER, MONGO_PASSWORD).
# Override container names with PG_CONTAINER / MONGO_CONTAINER, the media folder
# with MEDIA_DIR (default backend/data/media), the backup folder with OUT (default ops/backup/out/<time>, which git ignores).
set -euo pipefail
export MSYS_NO_PATHCONV=1   # Git Bash on Windows must not rewrite /tmp paths given to docker

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"
if [ -f "$root/.env" ]; then
  while IFS='=' read -r k v; do
    case "$k" in POSTGRES_USER|POSTGRES_DB|MONGO_USER|MONGO_PASSWORD) export "$k=$v" ;; esac
  done < <(grep -E '^(POSTGRES_USER|POSTGRES_DB|MONGO_USER|MONGO_PASSWORD)=' "$root/.env" | tr -d '\r')
fi
PGUSER_="${POSTGRES_USER:-connectme}"
PGDB="${POSTGRES_DB:-connectme}"
PG="${PG_CONTAINER:-dating-app-postgres-1}"
MG="${MONGO_CONTAINER:-dating-app-mongo-1}"
MEDIA="${MEDIA_DIR:-$root/backend/data/media}"
STAMP="$(date +%Y%m%d-%H%M%S)"
OUT="${OUT:-$here/out/$STAMP}"
mkdir -p "$OUT"
DRILL_PG="${PGDB}_drill"
DRILL_MONGO="connectme_drill"
fail=0

hostpath() { if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else printf "%s" "$1"; fi; }
say() { printf '%s\n' "$*"; }
bad() { say "FAIL: $*"; fail=1; }

# Tables whose row counts must match after a restore (the ones that hold
# people's data; the list is the same ones an erasure has to reach).
TABLES="users profiles swipes matches reports blocks coin_ledger live_rooms live_gifts investments trusted_contacts date_plans compat_answers"

psql_live() { docker exec -i -e PGPASSWORD="${POSTGRES_PASSWORD:-}" "$PG" psql -U "$PGUSER_" -d "$1" -v ON_ERROR_STOP=1 -At "${@:2}"; }

cleanup() {
  if [ "${KEEP:-0}" != "1" ]; then
    docker exec "$PG" psql -U "$PGUSER_" -d postgres -c "DROP DATABASE IF EXISTS \"$DRILL_PG\"" >/dev/null 2>&1 || true
    docker exec "$MG" mongosh --quiet -u "${MONGO_USER:-connectme}" -p "${MONGO_PASSWORD:-}" --authenticationDatabase admin \
      --eval "db.getSiblingDB('$DRILL_MONGO').dropDatabase()" >/dev/null 2>&1 || true
    docker exec "$PG" rm -f /tmp/drill.dump >/dev/null 2>&1 || true
    docker exec "$MG" rm -rf /tmp/drill-mongo >/dev/null 2>&1 || true
    rm -rf "$OUT/media-restore"
  fi
}
trap cleanup EXIT

say "== Postgres: dump =="
docker exec "$PG" pg_dump -U "$PGUSER_" -d "$PGDB" -Fc -f /tmp/drill.dump
docker cp "$PG:/tmp/drill.dump" "$(hostpath "$OUT/postgres-$STAMP.dump")"
say "   $(wc -c <"$OUT/postgres-$STAMP.dump") bytes -> $OUT/postgres-$STAMP.dump"

say "== Postgres: restore into scratch database $DRILL_PG =="
docker exec "$PG" psql -U "$PGUSER_" -d postgres -c "DROP DATABASE IF EXISTS \"$DRILL_PG\"" >/dev/null
docker exec "$PG" psql -U "$PGUSER_" -d postgres -c "CREATE DATABASE \"$DRILL_PG\"" >/dev/null
# Extensions need superuser-ish rights in the scratch database too.
docker exec "$PG" psql -U "$PGUSER_" -d "$DRILL_PG" -c "CREATE EXTENSION IF NOT EXISTS postgis; CREATE EXTENSION IF NOT EXISTS pgcrypto;" >/dev/null
docker exec "$PG" pg_restore -U "$PGUSER_" -d "$DRILL_PG" --no-owner --exit-on-error /tmp/drill.dump 2>"$OUT/pg_restore.log" || {
  # pg_restore reports "already exists" for the extensions created above; that is expected.
  if grep -vE 'already exists|^pg_restore: (warning|while PROCESSING|from TOC|error: could not execute query: ERROR:  extension)' "$OUT/pg_restore.log" | grep -q 'error'; then
    bad "pg_restore reported errors (see $OUT/pg_restore.log)"
  fi
}

say "== Postgres: compare =="
for t in $TABLES; do
  a="$(psql_live "$PGDB" -c "SELECT COUNT(*) FROM $t" 2>/dev/null || echo missing)"
  b="$(psql_live "$DRILL_PG" -c "SELECT COUNT(*) FROM $t" 2>/dev/null || echo missing)"
  if [ "$a" = "$b" ] && [ "$a" != "missing" ]; then say "   ok   $t: $a rows"; else bad "$t: live=$a restored=$b"; fi
done
a="$(psql_live "$PGDB" -c "SELECT COALESCE(string_agg(version, ',' ORDER BY version), '') FROM schema_migrations" 2>/dev/null || echo none)"
b="$(psql_live "$DRILL_PG" -c "SELECT COALESCE(string_agg(version, ',' ORDER BY version), '') FROM schema_migrations" 2>/dev/null || echo none)"
if [ "$a" = "$b" ] && [ "$a" != "none" ]; then say "   ok   applied migrations match"; else bad "migration lists differ (live=$a restored=$b)"; fi
# A restore that loses the geography index would silently slow every nearby search.
if [ "$(psql_live "$DRILL_PG" -c "SELECT COUNT(*) FROM pg_indexes WHERE indexdef ILIKE '%gist%'")" -ge 1 ]; then say "   ok   spatial index restored"; else bad "no GiST index after restore"; fi

say "== Mongo: dump and restore into scratch database $DRILL_MONGO =="
M_AUTH=(-u "${MONGO_USER:-connectme}" -p "${MONGO_PASSWORD:-}" --authenticationDatabase admin)
docker exec "$MG" rm -rf /tmp/drill-mongo
docker exec "$MG" mongodump "${M_AUTH[@]}" --db connectme --out /tmp/drill-mongo --quiet
docker exec "$MG" mongorestore "${M_AUTH[@]}" --nsFrom 'connectme.*' --nsTo "$DRILL_MONGO.*" /tmp/drill-mongo --quiet
colls="$(docker exec "$MG" mongosh --quiet "${M_AUTH[@]}" --eval "db.getSiblingDB('connectme').getCollectionNames().join(' ')")"
for c in $colls; do
  a="$(docker exec "$MG" mongosh --quiet "${M_AUTH[@]}" --eval "db.getSiblingDB('connectme').getCollection('$c').countDocuments()")"
  b="$(docker exec "$MG" mongosh --quiet "${M_AUTH[@]}" --eval "db.getSiblingDB('$DRILL_MONGO').getCollection('$c').countDocuments()")"
  if [ "$a" = "$b" ]; then say "   ok   $c: $a documents"; else bad "mongo $c: live=$a restored=$b"; fi
done
[ -n "$colls" ] || say "   (no collections yet: nothing to compare)"

say "== Media: archive, restore, compare checksums =="
if [ -d "$MEDIA" ]; then
  tar -C "$MEDIA" -czf "$OUT/media-$STAMP.tar.gz" .
  mkdir -p "$OUT/media-restore"
  tar -C "$OUT/media-restore" -xzf "$OUT/media-$STAMP.tar.gz"
  a="$(cd "$MEDIA" && find . -type f -print0 | sort -z | xargs -0 -r sha256sum | sha256sum | cut -d' ' -f1)"
  b="$(cd "$OUT/media-restore" && find . -type f -print0 | sort -z | xargs -0 -r sha256sum | sha256sum | cut -d' ' -f1)"
  n="$(find "$MEDIA" -type f | wc -l | tr -d ' ')"
  if [ "$a" = "$b" ]; then say "   ok   $n files identical"; else bad "media checksums differ"; fi
else
  say "   (no media folder at $MEDIA: skipped)"
fi

say
if [ "$fail" = "0" ]; then say "DRILL PASSED. Backups are in $OUT"; else say "DRILL FAILED. Backups are in $OUT"; fi
exit "$fail"
