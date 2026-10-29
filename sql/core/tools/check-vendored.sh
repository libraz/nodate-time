#!/usr/bin/env bash
# check-vendored.sh — verify a vendored directory still matches its upstream.
#
#   bash sql/core/tools/check-vendored.sh <dir> [--local-only]
#
# <dir> holds an UPSTREAM.json written by vendor.sh. Three checks:
#
#   local   the files are unedited, against the hashes in UPSTREAM.json
#   pin     the pinned ref, fetched from the recorded repository, still holds
#           exactly what is vendored
#   behind  the upstream default branch (UPSTREAM_DEFAULT_BRANCH, default
#           main) has not moved on without a re-vendor
#
# local needs nothing but this checkout. pin and behind need the upstream to
# be reachable; when it is not the script fails rather than passing, because
# "could not check" must not look like "no drift". --local-only runs just the
# first.
#
# Exit codes: 0 intact, 1 drift, 2 the check could not be performed.

set -euo pipefail
export LC_ALL=C

usage() { sed -n '2,20p' "${BASH_SOURCE[0]}"; }

DIR=""
LOCAL_ONLY=0
for arg in "$@"; do
  case "${arg}" in
    --local-only) LOCAL_ONLY=1 ;;
    -h | --help) usage; exit 0 ;;
    -*) echo "check-vendored.sh: unknown argument: ${arg}" >&2; exit 2 ;;
    *)
      if [[ -n "${DIR}" ]]; then
        echo "check-vendored.sh: more than one directory given" >&2
        exit 2
      fi
      DIR="${arg}"
      ;;
  esac
done
if [[ -z "${DIR}" ]]; then
  usage >&2
  exit 2
fi
if [[ ! -d "${DIR}" ]]; then
  echo "ERROR: ${DIR} is not a directory." >&2
  exit 2
fi
DIR="$(cd "${DIR}" && pwd)"
MANIFEST="${DIR}/UPSTREAM.json"
if [[ ! -f "${MANIFEST}" ]]; then
  echo "ERROR: ${MANIFEST} not found; the vendored copy has no provenance." >&2
  exit 2
fi

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  else
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

# vendor.sh writes one key per line, which is all this parser relies on.
json_top() {
  sed -nE "s/^  \"$1\": *\"([^\"]*)\".*/\1/p" "${MANIFEST}"
}
REPO="$(json_top repository)"
SRC="$(json_top sourcePath)"
REF="$(json_top ref)"
if [[ -z "${REPO}" || -z "${SRC}" || -z "${REF}" ]]; then
  echo "ERROR: ${MANIFEST} lacks repository, sourcePath or ref." >&2
  exit 2
fi

TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

# ── local: the copy has not been edited here ────────────────────────

awk '
  /"path":/   { p = $0; sub(/^[^:]*: *"/, "", p); sub(/".*$/, "", p) }
  /"sha256":/ { s = $0; sub(/^[^:]*: *"/, "", s); sub(/".*$/, "", s); printf "%s\t%s\n", p, s }
' "${MANIFEST}" | sort >"${TMP}/recorded"
(cd "${DIR}" && find . -type f ! -name UPSTREAM.json ! -name .DS_Store | sed 's#^\./##' | sort) >"${TMP}/present"
cut -f1 "${TMP}/recorded" >"${TMP}/recorded.paths"

problems=""
while IFS=$'\t' read -r rel want; do
  [[ -n "${rel}" ]] || continue
  if [[ ! -f "${DIR}/${rel}" ]]; then
    problems+="  missing: ${rel}"$'\n'
  elif [[ "$(sha256_of "${DIR}/${rel}")" != "${want}" ]]; then
    problems+="  edited:  ${rel}"$'\n'
  fi
done <"${TMP}/recorded"
while IFS= read -r rel; do
  problems+="  added:   ${rel}"$'\n'
done < <(comm -13 "${TMP}/recorded.paths" "${TMP}/present")

if [[ -n "${problems}" ]]; then
  echo "The vendored copy was changed in this repository:"
  printf '%s' "${problems}"
  echo ""
  echo "${DIR} is a copy, not a source. Change it upstream, then re-vendor;"
  echo "editing it here makes the two products disagree about a schema they"
  echo "both write to."
  exit 1
fi
echo "vendored copy matches its manifest ($(wc -l <"${TMP}/recorded" | tr -d ' ') files)"

if [[ ${LOCAL_ONLY} -eq 1 ]]; then
  exit 0
fi

# ── pin: the pinned ref still holds what was vendored ───────────────

# The manifest records the URL a person clones with, which may be SSH. The
# upstream is public, so fall back to HTTPS for agents without a key.
https_url() {
  printf '%s' "$1" | sed -E 's#^git@([^:]+):#https://\1/#'
}

fetch_into() {
  local dir="$1" what="$2" alt
  git init -q "${dir}" || return 1
  git -C "${dir}" remote add origin "${REPO}" || return 1
  if git -C "${dir}" fetch -q --depth 1 origin "${what}" 2>"${TMP}/fetch.err"; then
    return 0
  fi
  alt="$(https_url "${REPO}")"
  if [[ "${alt}" != "${REPO}" ]]; then
    git -C "${dir}" remote set-url origin "${alt}" || return 1
    git -C "${dir}" fetch -q --depth 1 origin "${what}" 2>"${TMP}/fetch.err" && return 0
  fi
  return 1
}

show_diff() {
  head -n 120 "$1"
  if [[ "$(wc -l <"$1")" -gt 120 ]]; then
    echo "... (truncated)"
  fi
}

echo "fetching ${REPO} at ${REF}"
if ! fetch_into "${TMP}/upstream" "${REF}"; then
  echo "ERROR: could not fetch ${REF} from ${REPO}." >&2
  sed 's/^/  /' "${TMP}/fetch.err" >&2 2>/dev/null || true
  echo "" >&2
  echo "The pinned ref must be reachable for this check to mean anything." >&2
  echo "If it has not been pushed upstream yet, push it; use --local-only" >&2
  echo "to assert only that the copy is unedited." >&2
  exit 2
fi
git -C "${TMP}/upstream" checkout -q FETCH_HEAD
if [[ ! -d "${TMP}/upstream/${SRC}" ]]; then
  echo "ERROR: ${REF} has no ${SRC} directory." >&2
  exit 2
fi

if ! diff -ru -x UPSTREAM.json -x .DS_Store "${TMP}/upstream/${SRC}" "${DIR}" >"${TMP}/pin.diff"; then
  echo "---- ${DIR} has drifted from ${REF} ----"
  show_diff "${TMP}/pin.diff"
  exit 1
fi
echo "${DIR} matches ${REPO} at ${REF}."

# ── behind: the pin itself is what went stale ───────────────────────
#
# The checks above validate a claim that stays true forever, including long
# after upstream moved. Only upstream's current state can say otherwise.

DEFAULT_REF="${UPSTREAM_DEFAULT_BRANCH:-main}"

echo "fetching ${REPO} at ${DEFAULT_REF}"
if ! fetch_into "${TMP}/head" "${DEFAULT_REF}"; then
  echo "ERROR: could not fetch ${DEFAULT_REF} from ${REPO}." >&2
  sed 's/^/  /' "${TMP}/fetch.err" >&2 2>/dev/null || true
  echo "" >&2
  echo "Whether the pin is current cannot be answered without it." >&2
  exit 2
fi
git -C "${TMP}/head" checkout -q FETCH_HEAD
HEAD_SHA="$(git -C "${TMP}/head" rev-parse HEAD)"

if diff -ru -x UPSTREAM.json -x .DS_Store "${TMP}/head/${SRC}" "${DIR}" >"${TMP}/head.diff"; then
  echo "${DIR} is current with ${DEFAULT_REF} (${HEAD_SHA})."
  exit 0
fi

echo "---- upstream moved and this copy was not re-vendored ----"
echo "pinned:  ${REF}"
echo "current: ${HEAD_SHA} (${DEFAULT_REF})"
echo ""
show_diff "${TMP}/head.diff"
echo ""
echo "Re-vendor from ${DEFAULT_REF} with vendor.sh, or decide deliberately to"
echo "stay behind and move the pin when you do."
exit 1
