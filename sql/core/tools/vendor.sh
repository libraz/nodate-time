#!/usr/bin/env bash
# vendor.sh — copy a directory of an upstream repository into this one and
# record where it came from.
#
#   bash sql/core/tools/vendor.sh --from <git-dir-or-url> --dest <dir>
#        [--ref <rev>] [--src <path>] [--repository <url>]
#
#   --from        a local git checkout or a clone URL
#   --dest        directory to replace with the vendored copy
#   --ref         commit-ish to take (default HEAD); pinned as a full sha
#   --src         path inside the upstream repository (default sql/core)
#   --repository  URL recorded in UPSTREAM.json (default: the checkout's
#                 origin URL, or --from itself)
#
# Only committed content is taken. <dest> is replaced wholesale and gets an
# UPSTREAM.json holding {repository, sourcePath, ref, files:[{path, sha256}]},
# which check-vendored.sh verifies.
#
# unreachable-by-design: run by hand when re-vendoring
#
# Exit codes: 0 ok, 2 bad usage or the source could not be read.

set -euo pipefail
export LC_ALL=C

usage() { sed -n '2,19p' "${BASH_SOURCE[0]}"; }

FROM=""
DEST=""
REF="HEAD"
SRC="sql/core"
REPOSITORY=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --from) FROM="${2:-}"; shift 2 ;;
    --dest) DEST="${2:-}"; shift 2 ;;
    --ref) REF="${2:-}"; shift 2 ;;
    --src) SRC="${2:-}"; shift 2 ;;
    --repository) REPOSITORY="${2:-}"; shift 2 ;;
    -h | --help) usage; exit 0 ;;
    *) echo "vendor.sh: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

SRC="${SRC%/}"
if [[ -z "${FROM}" || -z "${DEST}" || -z "${REF}" || -z "${SRC}" ]]; then
  usage >&2
  exit 2
fi
case "${DEST}" in
  / | . | ..)
    echo "vendor.sh: refusing to replace ${DEST}" >&2
    exit 2
    ;;
esac

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  else
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

if [[ -d "${FROM}" ]]; then
  REPO_DIR="${FROM}"
  SHA="$(git -C "${REPO_DIR}" rev-parse --verify "${REF}^{commit}")"
  if [[ -z "${REPOSITORY}" ]]; then
    REPOSITORY="$(git -C "${REPO_DIR}" remote get-url origin 2>/dev/null || true)"
    [[ -n "${REPOSITORY}" ]] || REPOSITORY="$(cd "${REPO_DIR}" && pwd)"
  fi
else
  REPO_DIR="${TMP}/clone"
  git init -q "${REPO_DIR}"
  git -C "${REPO_DIR}" remote add origin "${FROM}"
  git -C "${REPO_DIR}" fetch -q --depth 1 origin "${REF}"
  SHA="$(git -C "${REPO_DIR}" rev-parse --verify FETCH_HEAD)"
  [[ -n "${REPOSITORY}" ]] || REPOSITORY="${FROM}"
fi

mkdir "${TMP}/out"
git -C "${REPO_DIR}" archive --format=tar "${SHA}" -- "${SRC}" | tar -x -C "${TMP}/out"
if [[ ! -d "${TMP}/out/${SRC}" ]]; then
  echo "vendor.sh: ${SRC} is not a directory at ${SHA}" >&2
  exit 2
fi

rm -rf -- "${DEST}"
mkdir -p "${DEST}"
cp -R "${TMP}/out/${SRC}/." "${DEST}/"

FILES="$(cd "${DEST}" && find . -type f ! -name UPSTREAM.json ! -name .DS_Store | sed 's#^\./##' | sort)"

{
  printf '{\n'
  printf '  "repository": "%s",\n' "${REPOSITORY}"
  printf '  "sourcePath": "%s",\n' "${SRC}"
  printf '  "ref": "%s",\n' "${SHA}"
  printf '  "files": [\n'
  first=1
  while IFS= read -r rel; do
    [[ -n "${rel}" ]] || continue
    case "${rel}" in
      *'"'* | *'\'*)
        echo "vendor.sh: unsupported character in path: ${rel}" >&2
        exit 2
        ;;
    esac
    if [[ ${first} -eq 0 ]]; then printf ',\n'; fi
    first=0
    printf '    {\n      "path": "%s",\n      "sha256": "%s"\n    }' "${rel}" "$(sha256_of "${DEST}/${rel}")"
  done <<<"${FILES}"
  printf '\n  ]\n}\n'
} >"${TMP}/UPSTREAM.json"
mv "${TMP}/UPSTREAM.json" "${DEST}/UPSTREAM.json"

echo "vendored ${SRC} at ${SHA} into ${DEST}"
